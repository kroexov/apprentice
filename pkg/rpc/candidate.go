package rpc

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"apisrv/pkg/db"

	"github.com/go-pg/pg/v10"
	"github.com/vmkteam/embedlog"
	"github.com/vmkteam/zenrpc/v2"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

var (
	ErrCandidateNotFound      = zenrpc.NewStringError(http.StatusNotFound, "candidate not found")
	ErrCandidateStageNotFound = zenrpc.NewStringError(http.StatusNotFound, "candidate stage not found")
	ErrAlreadyScored          = zenrpc.NewStringError(http.StatusBadRequest, "stage already scored for this candidate")
	ErrAlreadyCompleted       = zenrpc.NewStringError(http.StatusBadRequest, "candidate already completed all stages")
	ErrScoreOutOfRange        = zenrpc.NewStringError(http.StatusBadRequest, "score out of range")
	ErrCannotRollback         = zenrpc.NewStringError(http.StatusBadRequest, "no previous stage to roll back to")
	ErrInvalidCurrentStage    = zenrpc.NewStringError(http.StatusBadRequest, "currentStageId references unknown stage")
	ErrHandleTaken            = zenrpc.NewStringError(http.StatusBadRequest, "handle is already taken")
	ErrForbidden              = zenrpc.NewStringError(http.StatusForbidden, "forbidden")
	ErrLinkInvalid            = zenrpc.NewStringError(http.StatusBadRequest, "link is not a valid http(s) URL")
)

const candidateStageLinkMaxLen = 2048

var candidateHandleRegex = regexp.MustCompile(`^[a-z0-9.\-_]{2,40}$`)

// ruCollator: locale-aware comparator for Russian names (А-Я ordering, case-insensitive).
var ruCollator = collate.New(language.Russian, collate.IgnoreCase)

type CandidateService struct {
	zenrpc.Service
	embedlog.Logger

	dbo  db.DB
	repo db.ApprenticeRepo
}

func NewCandidateService(dbo db.DB, logger embedlog.Logger) *CandidateService {
	return &CandidateService{
		dbo:    dbo,
		repo:   db.NewApprenticeRepo(dbo),
		Logger: logger,
	}
}

// Get returns candidate list with aggregates, sorted by sortBy.
//
//zenrpc:sortBy CandidateSort
//zenrpc:return []CandidateSummary
//zenrpc:500 Internal Error
func (s CandidateService) Get(ctx context.Context, sortBy CandidateSort) ([]CandidateSummary, error) {
	stages, candidates, scores, err := s.loadAggregateInputs(ctx)
	if err != nil {
		return nil, err
	}

	out := buildSummaries(candidates, stages, scores)
	sortSummaries(out, sortBy, stages)
	return out, nil
}

// GetByID returns full candidate detail with stage history.
//
//zenrpc:id int
//zenrpc:return CandidateDetail
//zenrpc:404 Not Found
//zenrpc:500 Internal Error
func (s CandidateService) GetByID(ctx context.Context, id int) (*CandidateDetail, error) {
	cand, err := s.repo.CandidateByID(ctx, id, s.repo.FullCandidate())
	if err != nil {
		return nil, InternalError(err)
	}
	if cand == nil {
		return nil, ErrCandidateNotFound
	}

	stages, err := s.repo.StagesByFilters(ctx, nil, db.PagerNoLimit, s.repo.FullStage())
	if err != nil {
		return nil, InternalError(err)
	}
	cstages, err := s.repo.CandidateStagesByFilters(ctx, &db.CandidateStageSearch{CandidateID: &id}, db.PagerNoLimit,
		db.WithColumns(db.TableColumns))
	if err != nil {
		return nil, InternalError(err)
	}

	summary := buildSummaryFor(cand, stages, cstages, totalsFromStages(stages))
	currentStage := findStage(stages, cand.CurrentStageID)
	csByStage := indexCandidateStages(cstages)

	history := make([]CandidateStageHistory, 0, len(stages))
	for i := range stages {
		st := &stages[i]
		row := CandidateStageHistory{
			StageID:  st.ID,
			Stage:    NewStage(st),
			MaxScore: st.MaxScore,
		}
		cs, hasRow := csByStage[st.ID]
		switch {
		case hasRow && cs.Score != nil:
			row.Status = StageStatusDone
			row.Score = cs.Score
			row.ScoredAt = formatTimePtr(cs.ScoredAt)
			row.CandidateStageID = &cs.ID
			row.Link = cs.Link
			row.Deadline = formatTimePtr(cs.Deadline)
			row.CreatedAt = ptrString(cs.CreatedAt.Format(time.RFC3339))
		case hasRow && cand.CompletedAt == nil && currentStage != nil && st.ID == currentStage.ID:
			row.Status = StageStatusCurrent
			row.CandidateStageID = &cs.ID
			row.Link = cs.Link
			row.Deadline = formatTimePtr(cs.Deadline)
			row.CreatedAt = ptrString(cs.CreatedAt.Format(time.RFC3339))
		default:
			row.Status = StageStatusTodo
		}
		history = append(history, row)
	}

	return &CandidateDetail{
		CandidateSummary: summary,
		Bio:              cand.Bio,
		History:          history,
	}, nil
}

// Add creates a candidate and returns a one-time password (only here).
//
//zenrpc:candidate Candidate
//zenrpc:return CandidateWithPassword
//zenrpc:400 Validation Error
//zenrpc:500 Internal Error
func (s CandidateService) Add(ctx context.Context, candidate Candidate) (*CandidateWithPassword, error) {
	if ve := s.isValid(ctx, candidate, false); ve.HasErrors() {
		return nil, ve.Error()
	}

	plainPassword := generateInitialPassword()
	hashed, err := passwordHash(plainPassword)
	if err != nil {
		return nil, InternalError(err)
	}

	var full *db.Candidate
	err = s.dbo.RunInTransaction(ctx, func(tx *pg.Tx) error {
		txRepo := s.repo.WithTransaction(tx)

		stage, txErr := txRepo.StageByID(ctx, candidate.CurrentStageID)
		if txErr != nil {
			return txErr
		}
		if stage == nil {
			return ErrInvalidCurrentStage
		}

		d := candidate.ToDB()
		d.ID = 0
		d.CompletedAt = nil
		d.Password = hashed
		created, txErr := txRepo.AddCandidate(ctx, d)
		if txErr != nil {
			if db.IsUniqueViolation(txErr) {
				return ErrHandleTaken
			}
			return txErr
		}

		if _, txErr = txRepo.CreateCandidateStage(ctx, created.ID, stage); txErr != nil {
			return txErr
		}

		full, txErr = txRepo.CandidateByID(ctx, created.ID, txRepo.FullCandidate())
		return txErr
	})
	if err != nil {
		var zerr *zenrpc.Error
		if errors.As(err, &zerr) {
			return nil, zerr
		}
		return nil, InternalError(err)
	}

	s.Print(ctx, "candidate added", "candidateId", full.ID, "stageId", full.CurrentStageID)
	return &CandidateWithPassword{
		Candidate: *NewCandidate(full),
		Password:  plainPassword,
	}, nil
}

// advanceFinalize runs the post-score half of Advance: either set completedAt
// (last stage) or create the next CandidateStage and bump CurrentStageID.
func advanceFinalize(ctx context.Context, txRepo db.ApprenticeRepo, cand *db.Candidate, stage *db.Stage, now time.Time) error {
	next, err := txRepo.NextStageAfter(ctx, stage.Order)
	if err != nil {
		return err
	}
	cand.UpdatedAt = now
	if next == nil {
		cand.CompletedAt = &now
		_, err = txRepo.UpdateCandidate(ctx, cand,
			db.WithColumns(db.Columns.Candidate.CompletedAt, db.Columns.Candidate.UpdatedAt))
		return err
	}
	if _, e := txRepo.CreateCandidateStage(ctx, cand.ID, next); e != nil {
		if db.IsUniqueViolation(e) {
			return ErrAlreadyScored
		}
		return e
	}
	cand.CurrentStageID = next.ID
	_, err = txRepo.UpdateCandidate(ctx, cand,
		db.WithColumns(db.Columns.Candidate.CurrentStageID, db.Columns.Candidate.UpdatedAt))
	return err
}

// generateInitialPassword returns a random 12-char URL-safe password used by
// candidate.Add. Surfaced once in the response — never stored client-side.
func generateInitialPassword() string {
	b := make([]byte, 9) // 9 bytes → 12 base64url chars, well above passwordMinLen.
	if _, err := cryptorand.Read(b); err != nil {
		panic("rpc/candidate: crypto/rand failure: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// Update changes basic fields. login/currentStageId/timestamps are immutable here.
//
//zenrpc:candidate Candidate
//zenrpc:return bool
//zenrpc:404 Not Found
//zenrpc:400 Validation Error
//zenrpc:500 Internal Error
func (s CandidateService) Update(ctx context.Context, candidate Candidate) (bool, error) {
	var ok bool
	err := s.dbo.RunInTransaction(ctx, func(tx *pg.Tx) error {
		txRepo := s.repo.WithTransaction(tx)

		cur, err := txRepo.CandidateByID(ctx, candidate.ID)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrCandidateNotFound
		}
		// Validation does only reads; running it on the outer repo is fine
		// — uniqueness errors will surface either here or via UniqueViolation.
		if ve := s.isValid(ctx, candidate, true); ve.HasErrors() {
			return ve.Error()
		}

		patch := candidate.ToDB()
		cur.Name = patch.Name
		cur.Handle = patch.Handle
		cur.City = patch.City
		cur.Age = patch.Age
		cur.Bio = patch.Bio
		cur.AvatarColor = patch.AvatarColor
		cur.Initials = patch.Initials
		cur.AvatarUrl = patch.AvatarUrl
		cur.Strengths = patch.Strengths
		cur.Weaknesses = patch.Weaknesses
		cur.UpdatedAt = time.Now()

		ok, err = txRepo.UpdateCandidate(ctx, cur, db.WithColumns(
			db.Columns.Candidate.Name,
			db.Columns.Candidate.Handle,
			db.Columns.Candidate.City,
			db.Columns.Candidate.Age,
			db.Columns.Candidate.Bio,
			db.Columns.Candidate.AvatarColor,
			db.Columns.Candidate.Initials,
			db.Columns.Candidate.AvatarUrl,
			db.Columns.Candidate.Strengths,
			db.Columns.Candidate.Weaknesses,
			db.Columns.Candidate.UpdatedAt,
		))
		if err != nil {
			if db.IsUniqueViolation(err) {
				return ErrHandleTaken
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

// Delete soft-deletes a candidate.
//
//zenrpc:id int
//zenrpc:return bool
//zenrpc:404 Not Found
//zenrpc:500 Internal Error
func (s CandidateService) Delete(ctx context.Context, id int) (bool, error) {
	cur, err := s.repo.CandidateByID(ctx, id)
	if err != nil {
		return false, InternalError(err)
	}
	if cur == nil {
		return false, ErrCandidateNotFound
	}
	ok, err := s.repo.DeleteCandidate(ctx, id)
	if err != nil {
		return false, InternalError(err)
	}
	s.Print(ctx, "candidate deleted", "candidateId", id)
	return ok, nil
}

// Advance scores current stage; moves to next or sets completedAt if last.
//
//zenrpc:candidateId int
//zenrpc:score int
//zenrpc:return AdvanceResult
//zenrpc:400 Validation Error
//zenrpc:404 Not Found
//zenrpc:500 Internal Error
func (s CandidateService) Advance(ctx context.Context, candidateID, score int) (*AdvanceResult, error) {
	var (
		outCand *db.Candidate
		outCS   *db.CandidateStage
	)

	err := s.dbo.RunInTransaction(ctx, func(tx *pg.Tx) error {
		txRepo := s.repo.WithTransaction(tx)

		cand, err := txRepo.CandidateByID(ctx, candidateID)
		if err != nil {
			return err
		}
		if cand == nil {
			return ErrCandidateNotFound
		}
		if cand.CompletedAt != nil {
			return ErrAlreadyCompleted
		}

		stage, err := txRepo.StageByID(ctx, cand.CurrentStageID)
		if err != nil {
			return err
		}
		if stage == nil {
			return ErrStageNotFound
		}
		if score < 1 || score > stage.MaxScore {
			return ErrScoreOutOfRange
		}

		cur, err := txRepo.OneCandidateStage(ctx, &db.CandidateStageSearch{
			CandidateID: &cand.ID,
			StageID:     &stage.ID,
		})
		if err != nil {
			return err
		}
		if cur == nil {
			// Invariant violation: every non-completed candidate must have an
			// empty row for currentStageId. If it's missing (legacy data, manual
			// SQL, etc.) reject loudly rather than silently inserting.
			return ErrCandidateStageNotFound
		}
		if cur.Score != nil {
			return ErrAlreadyScored
		}

		now := time.Now()
		cur.Score = &score
		cur.ScoredAt = &now
		if _, e := txRepo.UpdateCandidateStage(ctx, cur,
			db.WithColumns(db.Columns.CandidateStage.Score, db.Columns.CandidateStage.ScoredAt)); e != nil {
			return e
		}
		outCS = cur

		if e := advanceFinalize(ctx, txRepo, cand, stage, now); e != nil {
			return e
		}
		outCand = cand
		return nil
	})
	if err != nil {
		var zerr *zenrpc.Error
		if errors.As(err, &zerr) {
			return nil, zerr
		}
		return nil, InternalError(err)
	}

	full, err := s.repo.CandidateByID(ctx, outCand.ID, s.repo.FullCandidate())
	if err != nil {
		return nil, InternalError(err)
	}
	s.Print(ctx, "candidate advanced",
		"candidateId", outCand.ID,
		"stageId", outCS.StageID,
		"score", *outCS.Score,
		"completed", outCand.CompletedAt != nil,
	)
	return &AdvanceResult{
		Candidate:      NewCandidate(full),
		CandidateStage: NewCandidateStage(outCS),
	}, nil
}

// Rate sets or corrects score on a CandidateStage; does not move stage.
//
//zenrpc:candidateStageId int
//zenrpc:score int
//zenrpc:return CandidateStage
//zenrpc:400 Validation Error
//zenrpc:404 Not Found
//zenrpc:500 Internal Error
func (s CandidateService) Rate(ctx context.Context, candidateStageID, score int) (*CandidateStage, error) {
	cur, err := s.repo.CandidateStageByID(ctx, candidateStageID, s.repo.FullCandidateStage())
	if err != nil {
		return nil, InternalError(err)
	}
	if cur == nil {
		return nil, ErrCandidateStageNotFound
	}

	stage, err := s.repo.StageByID(ctx, cur.StageID)
	if err != nil {
		return nil, InternalError(err)
	}
	if stage == nil {
		return nil, ErrStageNotFound
	}
	if score < 1 || score > stage.MaxScore {
		return nil, ErrScoreOutOfRange
	}

	cur.Score = &score
	if cur.ScoredAt == nil {
		now := time.Now()
		cur.ScoredAt = &now
	}
	if _, err := s.repo.UpdateCandidateStage(ctx, cur,
		db.WithColumns(db.Columns.CandidateStage.Score, db.Columns.CandidateStage.ScoredAt)); err != nil {
		return nil, InternalError(err)
	}
	s.Print(ctx, "candidate stage rated",
		"candidateStageId", cur.ID, "candidateId", cur.CandidateID, "stageId", cur.StageID, "score", score)
	return NewCandidateStage(cur), nil
}

// Rollback reverts the most recent Advance; clears completedAt if set.
//
//zenrpc:candidateId int
//zenrpc:return Candidate
//zenrpc:400 Validation Error
//zenrpc:404 Not Found
//zenrpc:500 Internal Error
func (s CandidateService) Rollback(ctx context.Context, candidateID int) (*Candidate, error) {
	var out *db.Candidate

	err := s.dbo.RunInTransaction(ctx, func(tx *pg.Tx) error {
		txRepo := s.repo.WithTransaction(tx)

		cand, err := txRepo.CandidateByID(ctx, candidateID)
		if err != nil {
			return err
		}
		if cand == nil {
			return ErrCandidateNotFound
		}

		latest, err := txRepo.LatestScoredCandidateStageByCandidate(ctx, cand.ID)
		if err != nil {
			return err
		}
		if latest == nil {
			return ErrCannotRollback
		}

		if cand.CompletedAt == nil {
			// Drop the empty current-stage row to satisfy the
			// "exactly one empty row" invariant after rollback.
			cur, err := txRepo.OneCandidateStage(ctx, &db.CandidateStageSearch{
				CandidateID: &cand.ID,
				StageID:     &cand.CurrentStageID,
			})
			if err != nil {
				return err
			}
			if cur != nil && cur.ID != latest.ID {
				if _, err := txRepo.DeleteCandidateStage(ctx, cur.ID); err != nil {
					return err
				}
			}
		}

		latest.Score = nil
		latest.ScoredAt = nil
		if _, err := txRepo.UpdateCandidateStage(ctx, latest,
			db.WithColumns(db.Columns.CandidateStage.Score, db.Columns.CandidateStage.ScoredAt)); err != nil {
			return err
		}

		cand.CurrentStageID = latest.StageID
		cand.CompletedAt = nil
		cand.UpdatedAt = time.Now()
		if _, err := txRepo.UpdateCandidate(ctx, cand,
			db.WithColumns(db.Columns.Candidate.CurrentStageID, db.Columns.Candidate.CompletedAt, db.Columns.Candidate.UpdatedAt)); err != nil {
			return err
		}

		out = cand
		return nil
	})
	if err != nil {
		var zerr *zenrpc.Error
		if errors.As(err, &zerr) {
			return nil, zerr
		}
		return nil, InternalError(err)
	}

	full, err := s.repo.CandidateByID(ctx, out.ID, s.repo.FullCandidate())
	if err != nil {
		return nil, InternalError(err)
	}
	s.Print(ctx, "candidate rolled back", "candidateId", out.ID, "stageId", out.CurrentStageID)
	return NewCandidate(full), nil
}

// SetLink attaches or detaches link on a CandidateStage (admin or self-candidate).
//
//zenrpc:candidateStageId int
//zenrpc:link *string
//zenrpc:return CandidateStage
//zenrpc:401 Unauthorized
//zenrpc:403 Forbidden
//zenrpc:404 Not Found
//zenrpc:400 Validation Error
//zenrpc:500 Internal Error
func (s CandidateService) SetLink(ctx context.Context, candidateStageID int, link *string) (*CandidateStage, error) {
	admin := AdminFromContext(ctx)
	cand := CandidateFromContext(ctx)
	if admin == nil && cand == nil {
		return nil, ErrUnauthorized
	}

	cur, err := s.repo.CandidateStageByID(ctx, candidateStageID, s.repo.FullCandidateStage())
	if err != nil {
		return nil, InternalError(err)
	}
	if cur == nil {
		return nil, ErrCandidateStageNotFound
	}
	if admin == nil && cur.CandidateID != cand.ID {
		return nil, ErrForbidden
	}

	normalized, err := normalizeLink(link)
	if err != nil {
		return nil, err
	}
	cur.Link = normalized
	if _, err := s.repo.UpdateCandidateStage(ctx, cur, db.WithColumns(db.Columns.CandidateStage.Link)); err != nil {
		return nil, InternalError(err)
	}
	s.Print(ctx, "candidate stage link set",
		"candidateStageId", cur.ID, "candidateId", cur.CandidateID, "stageId", cur.StageID,
		"detached", normalized == nil)
	return NewCandidateStage(cur), nil
}

// normalizeLink returns nil for nil/empty/whitespace input (detach), otherwise
// validates the trimmed value is a http(s) URL no longer than candidateStageLinkMaxLen.
func normalizeLink(link *string) (*string, error) {
	if link == nil {
		return nil, nil
	}
	v := strings.TrimSpace(*link)
	if v == "" {
		return nil, nil
	}
	if len(v) > candidateStageLinkMaxLen {
		return nil, ErrLinkInvalid
	}
	u, err := url.Parse(v)
	if err != nil {
		return nil, ErrLinkInvalid
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, ErrLinkInvalid
	}
	if u.Host == "" {
		return nil, ErrLinkInvalid
	}
	return &v, nil
}

// Kanban returns candidates grouped by their current stage.
//
//zenrpc:return []KanbanColumn
//zenrpc:500 Internal Error
func (s CandidateService) Kanban(ctx context.Context) ([]KanbanColumn, error) {
	stages, candidates, scores, err := s.loadAggregateInputs(ctx)
	if err != nil {
		return nil, err
	}

	summaries := buildSummaries(candidates, stages, scores)
	byStage := make(map[int][]CandidateSummary, len(stages))
	for _, c := range summaries {
		stageID := -1
		if c.CurrentStage != nil {
			stageID = c.CurrentStage.ID
		}
		byStage[stageID] = append(byStage[stageID], c)
	}

	cols := make([]KanbanColumn, 0, len(stages))
	for i := range stages {
		st := &stages[i]
		cols = append(cols, KanbanColumn{
			Stage:      NewStage(st),
			Candidates: byStage[st.ID],
		})
	}
	return cols, nil
}

func (s CandidateService) loadAggregateInputs(ctx context.Context) ([]db.Stage, []db.Candidate, []db.CandidateStage, error) {
	stages, err := s.repo.StagesByFilters(ctx, nil, db.PagerNoLimit, s.repo.FullStage())
	if err != nil {
		return nil, nil, nil, InternalError(err)
	}
	candidates, err := s.repo.CandidatesByFilters(ctx, nil, db.PagerNoLimit, s.repo.FullCandidate())
	if err != nil {
		return nil, nil, nil, InternalError(err)
	}
	cstages, err := s.repo.CandidateStagesByFilters(ctx, nil, db.PagerNoLimit, db.WithColumns(db.TableColumns))
	if err != nil {
		return nil, nil, nil, InternalError(err)
	}
	return stages, candidates, cstages, nil
}

func (s CandidateService) isValid(ctx context.Context, c Candidate, isUpdate bool) Validator {
	var v Validator

	switch {
	case c.Name == "":
		v.Append("name", FieldErrorRequired)
	case utf8.RuneCountInString(c.Name) > 80:
		v.AppendMax("name", 80)
	}
	switch {
	case c.Handle == "":
		v.Append("handle", FieldErrorRequired)
	case utf8.RuneCountInString(c.Handle) > 40:
		v.AppendMax("handle", 40)
	case !candidateHandleRegex.MatchString(c.Handle):
		v.Append("handle", FieldErrorFormat)
	}
	// Login is validated only on Add. Update ignores `candidate.login`
	// entirely (immutable field — see Update doc-comment), so we don't want
	// stale/empty values from the client to fail validation here.
	if !isUpdate {
		switch {
		case c.Login == "":
			v.Append("login", FieldErrorRequired)
		case utf8.RuneCountInString(c.Login) > 64:
			v.AppendMax("login", 64)
		case !candidateHandleRegex.MatchString(c.Login):
			v.Append("login", FieldErrorFormat)
		}
	}
	if utf8.RuneCountInString(c.City) > 128 {
		v.AppendMax("city", 128)
	}
	if c.Age != nil && (*c.Age < 14 || *c.Age > 120) {
		v.Append("age", FieldErrorFormat)
	}
	if utf8.RuneCountInString(c.AvatarColor) > 16 {
		v.AppendMax("avatarColor", 16)
	}
	if n := utf8.RuneCountInString(c.Initials); n < 1 || n > 3 {
		v.Append("initials", FieldErrorLen)
	}
	if len(c.Strengths) > 10 {
		v.AppendMax("strengths", 10)
	}
	for _, t := range c.Strengths {
		if utf8.RuneCountInString(t) > 40 {
			v.AppendMax("strengths", 40)
			break
		}
	}
	if len(c.Weaknesses) > 10 {
		v.AppendMax("weaknesses", 10)
	}
	for _, t := range c.Weaknesses {
		if utf8.RuneCountInString(t) > 40 {
			v.AppendMax("weaknesses", 40)
			break
		}
	}
	if c.CurrentStageID == 0 {
		v.Append("currentStageId", FieldErrorRequired)
	}
	if v.HasErrors() {
		return v
	}

	s.checkCandidateUniqueness(ctx, &v, c, isUpdate)
	return v
}

// checkCandidateUniqueness fills uniqueness errors for handle and (on Add only)
// login. Pulled out of isValid solely to keep cyclomatic complexity below the
// linter cap. Update never re-checks login since the field is immutable.
func (s CandidateService) checkCandidateUniqueness(ctx context.Context, v *Validator, c Candidate, isUpdate bool) {
	other, err := s.repo.OneCandidate(ctx, &db.CandidateSearch{Handle: &c.Handle})
	if err != nil {
		v.SetInternalError(err)
		return
	}
	if other != nil && (!isUpdate || other.ID != c.ID) {
		v.Append("handle", FieldErrorUnique)
	}
	if isUpdate {
		return
	}
	other, err = s.repo.OneCandidate(ctx, &db.CandidateSearch{Login: &c.Login})
	if err != nil {
		v.SetInternalError(err)
		return
	}
	if other != nil {
		v.Append("login", FieldErrorUnique)
	}
}

// ============================================================================
// Helpers (in-memory aggregation)
// ============================================================================

func findStage(stages []db.Stage, id int) *db.Stage {
	for i := range stages {
		if stages[i].ID == id {
			return &stages[i]
		}
	}
	return nil
}

// indexCandidateStages keys a candidate's CandidateStages by stageId. There's
// at most one row per (candidate, stage), so this is a 1:1 map.
func indexCandidateStages(rows []db.CandidateStage) map[int]db.CandidateStage {
	out := make(map[int]db.CandidateStage, len(rows))
	for _, r := range rows {
		out[r.StageID] = r
	}
	return out
}

// ptrString returns a pointer to s — convenience wrapper to keep call sites tight.
func ptrString(s string) *string { return &s }

// totalsFromStages returns sum(MaxScore) across stages — the "max points" baseline.
func totalsFromStages(stages []db.Stage) int {
	maxPoints := 0
	for _, st := range stages {
		maxPoints += st.MaxScore
	}
	return maxPoints
}

// buildSummaryFor materialises a CandidateSummary using a pre-loaded
// CandidateStage subset belonging to this candidate (the caller is responsible
// for filtering). Only rows with non-NULL Score count toward TotalPoints/CompletedStages.
// CurrentCandidateStage carries link/deadline/createdAt of the row with the
// largest stageId — i.e. the candidate's current (or last reached) stage.
func buildSummaryFor(cand *db.Candidate, stages []db.Stage, candStages []db.CandidateStage, maxPoints int) CandidateSummary {
	totalPoints := 0
	completed := 0
	var currentCS *db.CandidateStage
	for i := range candStages {
		cs := &candStages[i]
		if cs.Score != nil {
			totalPoints += *cs.Score
			completed++
		}
		if currentCS == nil || cs.StageID > currentCS.StageID {
			currentCS = cs
		}
	}
	currentStage := findStage(stages, cand.CurrentStageID)

	return CandidateSummary{
		ID:                    cand.ID,
		Name:                  cand.Name,
		Handle:                cand.Handle,
		City:                  cand.City,
		Age:                   cand.Age,
		AvatarColor:           cand.AvatarColor,
		Initials:              cand.Initials,
		AvatarURL:             cand.AvatarUrl,
		Strengths:             cand.Strengths,
		Weaknesses:            cand.Weaknesses,
		CurrentStage:          NewStage(currentStage),
		CurrentCandidateStage: newCandidateStageSummary(currentCS),
		TotalPoints:           totalPoints,
		MaxPoints:             maxPoints,
		CompletedStages:       completed,
		StageCount:            len(stages),
		CompletedAt:           formatTimePtr(cand.CompletedAt),
	}
}

// newCandidateStageSummary projects a db.CandidateStage onto its summary form;
// returns nil for nil input so absent rows survive the JSON round-trip.
func newCandidateStageSummary(cs *db.CandidateStage) *CandidateStageSummary {
	if cs == nil {
		return nil
	}
	return &CandidateStageSummary{
		Link:      cs.Link,
		Deadline:  formatTimePtr(cs.Deadline),
		CreatedAt: formatTime(cs.CreatedAt),
	}
}

func buildSummaries(candidates []db.Candidate, stages []db.Stage, cstages []db.CandidateStage) []CandidateSummary {
	byCand := make(map[int][]db.CandidateStage, len(candidates))
	for _, cs := range cstages {
		byCand[cs.CandidateID] = append(byCand[cs.CandidateID], cs)
	}
	maxPoints := totalsFromStages(stages)

	out := make([]CandidateSummary, 0, len(candidates))
	for i := range candidates {
		out = append(out, buildSummaryFor(&candidates[i], stages, byCand[candidates[i].ID], maxPoints))
	}
	return out
}

func sortSummaries(list []CandidateSummary, by CandidateSort, stages []db.Stage) {
	stageOrder := make(map[int]int, len(stages))
	for _, st := range stages {
		stageOrder[st.ID] = st.Order
	}

	switch by {
	case CandidateSortStage:
		sort.SliceStable(list, func(i, j int) bool {
			oi, oj := 0, 0
			if list[i].CurrentStage != nil {
				oi = stageOrder[list[i].CurrentStage.ID]
			}
			if list[j].CurrentStage != nil {
				oj = stageOrder[list[j].CurrentStage.ID]
			}
			return oi > oj
		})
	case CandidateSortName:
		sort.SliceStable(list, func(i, j int) bool {
			return ruCollator.CompareString(list[i].Name, list[j].Name) < 0
		})
	case CandidateSortPoints:
		fallthrough
	default:
		sort.SliceStable(list, func(i, j int) bool {
			return list[i].TotalPoints > list[j].TotalPoints
		})
	}
}
