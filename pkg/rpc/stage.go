package rpc

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"unicode/utf8"

	"apisrv/pkg/db"

	"github.com/go-pg/pg/v10"
	"github.com/vmkteam/embedlog"
	"github.com/vmkteam/zenrpc/v2"
)

var (
	ErrStageNotFound  = zenrpc.NewStringError(http.StatusNotFound, "stage not found")
	ErrStageHasScores = zenrpc.NewStringError(http.StatusBadRequest, "stage has scores, cannot delete")
	ErrReorderInvalid = zenrpc.NewStringError(http.StatusBadRequest, "reorder list does not match stages")
	ErrAliasTaken     = zenrpc.NewStringError(http.StatusBadRequest, "alias is already taken")
	ErrOrderTaken     = zenrpc.NewStringError(http.StatusBadRequest, "order is already taken")
)

var stageAliasRegex = regexp.MustCompile(`^[a-z0-9.\-_]{2,64}$`)

// reorderShift: large positive offset used during the first phase of a Reorder
// to dodge the partial UNIQUE on stages.order without ever using non-positive
// values. Anything well above the max number of stages a project will ever
// realistically have works.
const reorderShift = 1_000_000

type StageService struct {
	zenrpc.Service
	embedlog.Logger

	dbo  db.DB
	repo db.ApprenticeRepo
}

func NewStageService(dbo db.DB, logger embedlog.Logger) *StageService {
	return &StageService{
		dbo:    dbo,
		repo:   db.NewApprenticeRepo(dbo),
		Logger: logger,
	}
}

// Get returns all enabled stages ordered by order field.
//
//zenrpc:return []Stage
//zenrpc:500 Internal Error
func (s StageService) Get(ctx context.Context) ([]Stage, error) {
	list, err := s.repo.StagesByFilters(ctx, nil, db.PagerNoLimit,
		s.repo.FullStage(),
		db.WithSort(db.NewSortField(db.Columns.Stage.Order, false)),
	)
	if err != nil {
		return nil, InternalError(err)
	}
	out := make([]Stage, 0, len(list))
	for i := range list {
		if v := NewStage(&list[i]); v != nil {
			out = append(out, *v)
		}
	}
	return out, nil
}

// GetByID returns a stage by ID.
//
//zenrpc:id int
//zenrpc:return Stage
//zenrpc:404 Not Found
//zenrpc:500 Internal Error
func (s StageService) GetByID(ctx context.Context, id int) (*Stage, error) {
	d, err := s.repo.StageByID(ctx, id, s.repo.FullStage())
	if err != nil {
		return nil, InternalError(err)
	}
	if d == nil {
		return nil, ErrStageNotFound
	}
	return NewStage(d), nil
}

// Add creates a new stage.
//
//zenrpc:stage Stage
//zenrpc:return Stage
//zenrpc:400 Validation Error
//zenrpc:500 Internal Error
func (s StageService) Add(ctx context.Context, stage Stage) (*Stage, error) {
	if ve := s.isValid(ctx, stage, false); ve.HasErrors() {
		return nil, ve.Error()
	}
	d := stage.ToDB()
	d.ID = 0
	d.StatusID = db.StatusEnabled
	created, err := s.repo.AddStage(ctx, d)
	if err != nil {
		if e := mapStageUniqueErr(err); e != nil {
			return nil, e
		}
		return nil, InternalError(err)
	}
	s.Print(ctx, "stage added", "stageId", created.ID, "alias", created.Alias, "order", created.Order)
	return NewStage(created), nil
}

// Update changes a stage. To reorder use Reorder.
//
//zenrpc:stage Stage
//zenrpc:return bool
//zenrpc:404 Not Found
//zenrpc:400 Validation Error
//zenrpc:500 Internal Error
func (s StageService) Update(ctx context.Context, stage Stage) (bool, error) {
	if ve := s.isValid(ctx, stage, true); ve.HasErrors() {
		return false, ve.Error()
	}

	var ok bool
	err := s.dbo.RunInTransaction(ctx, func(tx *pg.Tx) error {
		txRepo := s.repo.WithTransaction(tx)

		cur, err := txRepo.StageByID(ctx, stage.ID)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrStageNotFound
		}
		cur.Alias = stage.Alias
		cur.Title = stage.Title
		cur.ShortTitle = stage.ShortTitle
		cur.Description = stage.Description
		cur.MaxScore = stage.MaxScore
		cur.DeadlineDays = stage.DeadlineDays

		ok, err = txRepo.UpdateStage(ctx, cur, db.WithColumns(
			db.Columns.Stage.Alias,
			db.Columns.Stage.Title,
			db.Columns.Stage.ShortTitle,
			db.Columns.Stage.Description,
			db.Columns.Stage.MaxScore,
			db.Columns.Stage.DeadlineDays,
		))
		if err != nil {
			if e := mapStageUniqueErr(err); e != nil {
				return e
			}
			return err
		}
		return nil
	})
	if err != nil {
		var zerr *zenrpc.Error
		if errors.As(err, &zerr) {
			return false, zerr
		}
		return false, InternalError(err)
	}
	return ok, nil
}

// Delete removes a stage. Fails if any candidate has a score for it.
//
//zenrpc:id int
//zenrpc:return bool
//zenrpc:404 Not Found
//zenrpc:400 stage has scores
//zenrpc:500 Internal Error
func (s StageService) Delete(ctx context.Context, id int) (bool, error) {
	var ok bool
	err := s.dbo.RunInTransaction(ctx, func(tx *pg.Tx) error {
		txRepo := s.repo.WithTransaction(tx)

		cur, err := txRepo.StageByID(ctx, id)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrStageNotFound
		}

		cnt, err := txRepo.CountCandidateStages(ctx, &db.CandidateStageSearch{StageID: &id})
		if err != nil {
			return err
		}
		if cnt > 0 {
			return ErrStageHasScores
		}

		ok, err = txRepo.DeleteStage(ctx, id)
		return err
	})
	if err != nil {
		var zerr *zenrpc.Error
		if errors.As(err, &zerr) {
			return false, zerr
		}
		return false, InternalError(err)
	}
	s.Print(ctx, "stage deleted", "stageId", id)
	return ok, nil
}

// Reorder accepts an ordered list of stage ids and re-numbers order to match.
// All enabled stages must be present, no duplicates allowed.
//
//zenrpc:ids []int
//zenrpc:return []Stage
//zenrpc:400 Validation Error
//zenrpc:500 Internal Error
func (s StageService) Reorder(ctx context.Context, ids []int) ([]Stage, error) {
	var out []Stage
	err := s.dbo.RunInLock(ctx, "stages-reorder", func(tx *pg.Tx) error {
		txRepo := s.repo.WithTransaction(tx)

		all, err := txRepo.StagesByFilters(ctx, nil, db.PagerNoLimit, txRepo.FullStage())
		if err != nil {
			return err
		}
		if len(ids) != len(all) {
			return ErrReorderInvalid
		}
		byID := make(map[int]*db.Stage, len(all))
		for i := range all {
			byID[all[i].ID] = &all[i]
		}
		seen := make(map[int]bool, len(ids))
		for _, id := range ids {
			if seen[id] || byID[id] == nil {
				return ErrReorderInvalid
			}
			seen[id] = true
		}

		// Two phases inside one tx + advisory lock. First phase shifts every row
		// to a high, collision-free range; second phase writes the target order.
		// Always-positive values keep us safe for any future ">0" CHECK and for
		// readers that observe in-flight state during a long-running reorder.
		for i, id := range ids {
			st := byID[id]
			st.Order = reorderShift + i + 1
			if _, err := txRepo.UpdateStage(ctx, st, db.WithColumns(db.Columns.Stage.Order)); err != nil {
				return err
			}
		}
		for i, id := range ids {
			st := byID[id]
			st.Order = i + 1
			if _, err := txRepo.UpdateStage(ctx, st, db.WithColumns(db.Columns.Stage.Order)); err != nil {
				return err
			}
		}

		out = make([]Stage, 0, len(ids))
		for _, id := range ids {
			out = append(out, *NewStage(byID[id]))
		}
		return nil
	})
	if err != nil {
		var zerr *zenrpc.Error
		if errors.As(err, &zerr) {
			return nil, zerr
		}
		return nil, InternalError(err)
	}
	s.Print(ctx, "stages reordered", "count", len(ids))
	return out, nil
}

func (s StageService) isValid(ctx context.Context, stage Stage, isUpdate bool) Validator {
	var v Validator

	// Regex matches the DB CHECK in docs/apisrv.sql for "alias".
	switch {
	case stage.Alias == "":
		v.Append("alias", FieldErrorRequired)
	case utf8.RuneCountInString(stage.Alias) > 64:
		v.AppendMax("alias", 64)
	case !stageAliasRegex.MatchString(stage.Alias):
		v.Append("alias", FieldErrorFormat)
	}
	switch {
	case stage.Title == "":
		v.Append("title", FieldErrorRequired)
	case utf8.RuneCountInString(stage.Title) > 255:
		v.AppendMax("title", 255)
	}
	switch {
	case stage.ShortTitle == "":
		v.Append("shortTitle", FieldErrorRequired)
	case utf8.RuneCountInString(stage.ShortTitle) > 64:
		v.AppendMax("shortTitle", 64)
	}
	switch {
	case stage.MaxScore < 1:
		v.AppendMin("maxScore", 1)
	case stage.MaxScore > 100:
		v.AppendMax("maxScore", 100)
	}
	switch {
	case stage.DeadlineDays < 0:
		v.AppendMin("deadlineDays", 0)
	case stage.DeadlineDays > 365:
		v.AppendMax("deadlineDays", 365)
	}
	if stage.Order < 1 {
		v.AppendMin("order", 1)
	}
	if v.HasErrors() {
		return v
	}

	other, err := s.repo.OneStage(ctx, &db.StageSearch{Alias: &stage.Alias})
	if err != nil {
		v.SetInternalError(err)
		return v
	}
	if other != nil && (!isUpdate || other.ID != stage.ID) {
		v.Append("alias", FieldErrorUnique)
	}
	if !isUpdate {
		conflict, err := s.repo.OneStage(ctx, &db.StageSearch{Order: &stage.Order})
		if err != nil {
			v.SetInternalError(err)
			return v
		}
		if conflict != nil {
			v.Append("order", FieldErrorUnique)
		}
	}
	return v
}

// mapStageUniqueErr converts a Postgres 23505 from a stages-table write into
// either ErrAliasTaken or ErrOrderTaken when we can tell which constraint
// fired. Falls back to ErrAliasTaken otherwise (the more user-friendly default
// — Reorder is the only path that can collide on order).
func mapStageUniqueErr(err error) *zenrpc.Error {
	if !db.IsUniqueViolation(err) {
		return nil
	}
	var pgErr pg.Error
	if errors.As(err, &pgErr) {
		switch pgErr.Field('n') {
		case "stages_order_key":
			return ErrOrderTaken
		case "stages_alias_key":
			return ErrAliasTaken
		}
	}
	return ErrAliasTaken
}
