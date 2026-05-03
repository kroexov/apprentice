package db

import (
	"context"
	"errors"

	"github.com/go-pg/pg/v10"
	"github.com/go-pg/pg/v10/orm"
)

type ApprenticeRepo struct {
	db      orm.DB
	filters map[string][]Filter
	sort    map[string][]SortField
	join    map[string][]string
}

// NewApprenticeRepo returns new repository
func NewApprenticeRepo(db orm.DB) ApprenticeRepo {
	return ApprenticeRepo{
		db: db,
		filters: map[string][]Filter{
			Tables.Candidate.Name: {StatusFilter},
			Tables.Stage.Name:     {StatusFilter},
		},
		sort: map[string][]SortField{
			Tables.Candidate.Name:      {{Column: Columns.Candidate.CreatedAt, Direction: SortDesc}},
			Tables.CandidateStage.Name: {{Column: Columns.CandidateStage.CreatedAt, Direction: SortDesc}},
			Tables.Stage.Name:          {{Column: Columns.Stage.Title, Direction: SortAsc}},
		},
		join: map[string][]string{
			Tables.Candidate.Name:      {TableColumns, Columns.Candidate.CurrentStage},
			Tables.CandidateStage.Name: {TableColumns, Columns.CandidateStage.Candidate, Columns.CandidateStage.Stage},
			Tables.Stage.Name:          {TableColumns},
		},
	}
}

// WithTransaction is a function that wraps ApprenticeRepo with pg.Tx transaction.
func (ar ApprenticeRepo) WithTransaction(tx *pg.Tx) ApprenticeRepo {
	ar.db = tx
	return ar
}

// WithEnabledOnly is a function that adds "statusId"=1 as base filter.
func (ar ApprenticeRepo) WithEnabledOnly() ApprenticeRepo {
	f := make(map[string][]Filter, len(ar.filters))
	for i := range ar.filters {
		f[i] = make([]Filter, len(ar.filters[i]))
		copy(f[i], ar.filters[i])
		f[i] = append(f[i], StatusEnabledFilter)
	}
	ar.filters = f

	return ar
}

/*** Candidate ***/

// FullCandidate returns full joins with all columns
func (ar ApprenticeRepo) FullCandidate() OpFunc {
	return WithColumns(ar.join[Tables.Candidate.Name]...)
}

// DefaultCandidateSort returns default sort.
func (ar ApprenticeRepo) DefaultCandidateSort() OpFunc {
	return WithSort(ar.sort[Tables.Candidate.Name]...)
}

// CandidateByID is a function that returns Candidate by ID(s) or nil.
func (ar ApprenticeRepo) CandidateByID(ctx context.Context, id int, ops ...OpFunc) (*Candidate, error) {
	return ar.OneCandidate(ctx, &CandidateSearch{ID: &id}, ops...)
}

// OneCandidate is a function that returns one Candidate by filters. It could return pg.ErrMultiRows.
func (ar ApprenticeRepo) OneCandidate(ctx context.Context, search *CandidateSearch, ops ...OpFunc) (*Candidate, error) {
	obj := &Candidate{}
	err := buildQuery(ctx, ar.db, obj, search, ar.filters[Tables.Candidate.Name], PagerTwo, ops...).Select()

	if errors.Is(err, pg.ErrMultiRows) {
		return nil, err
	} else if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}

	return obj, err
}

// CandidatesByFilters returns Candidate list.
func (ar ApprenticeRepo) CandidatesByFilters(ctx context.Context, search *CandidateSearch, pager Pager, ops ...OpFunc) (candidates []Candidate, err error) {
	err = buildQuery(ctx, ar.db, &candidates, search, ar.filters[Tables.Candidate.Name], pager, ops...).Select()
	return
}

// CountCandidates returns count
func (ar ApprenticeRepo) CountCandidates(ctx context.Context, search *CandidateSearch, ops ...OpFunc) (int, error) {
	return buildQuery(ctx, ar.db, &Candidate{}, search, ar.filters[Tables.Candidate.Name], PagerOne, ops...).Count()
}

// AddCandidate adds Candidate to DB.
func (ar ApprenticeRepo) AddCandidate(ctx context.Context, candidate *Candidate, ops ...OpFunc) (*Candidate, error) {
	q := ar.db.ModelContext(ctx, candidate)
	if len(ops) == 0 {
		q = q.ExcludeColumn(Columns.Candidate.CreatedAt)
	}
	applyOps(q, ops...)
	_, err := q.Insert()

	return candidate, err
}

// UpdateCandidate updates Candidate in DB.
func (ar ApprenticeRepo) UpdateCandidate(ctx context.Context, candidate *Candidate, ops ...OpFunc) (bool, error) {
	q := ar.db.ModelContext(ctx, candidate).WherePK()
	if len(ops) == 0 {
		q = q.ExcludeColumn(Columns.Candidate.ID, Columns.Candidate.CreatedAt)
	}
	applyOps(q, ops...)
	res, err := q.Update()
	if err != nil {
		return false, err
	}

	return res.RowsAffected() > 0, err
}

// DeleteCandidate set statusId to deleted in DB.
func (ar ApprenticeRepo) DeleteCandidate(ctx context.Context, id int) (deleted bool, err error) {
	candidate := &Candidate{ID: id, StatusID: StatusDeleted}

	return ar.UpdateCandidate(ctx, candidate, WithColumns(Columns.Candidate.StatusID))
}

/*** CandidateStage ***/

// FullCandidateStage returns full joins with all columns
func (ar ApprenticeRepo) FullCandidateStage() OpFunc {
	return WithColumns(ar.join[Tables.CandidateStage.Name]...)
}

// DefaultCandidateStageSort returns default sort.
func (ar ApprenticeRepo) DefaultCandidateStageSort() OpFunc {
	return WithSort(ar.sort[Tables.CandidateStage.Name]...)
}

// CandidateStageByID is a function that returns CandidateStage by ID(s) or nil.
func (ar ApprenticeRepo) CandidateStageByID(ctx context.Context, id int, ops ...OpFunc) (*CandidateStage, error) {
	return ar.OneCandidateStage(ctx, &CandidateStageSearch{ID: &id}, ops...)
}

// OneCandidateStage is a function that returns one CandidateStage by filters. It could return pg.ErrMultiRows.
func (ar ApprenticeRepo) OneCandidateStage(ctx context.Context, search *CandidateStageSearch, ops ...OpFunc) (*CandidateStage, error) {
	obj := &CandidateStage{}
	err := buildQuery(ctx, ar.db, obj, search, ar.filters[Tables.CandidateStage.Name], PagerTwo, ops...).Select()

	if errors.Is(err, pg.ErrMultiRows) {
		return nil, err
	} else if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}

	return obj, err
}

// CandidateStagesByFilters returns CandidateStage list.
func (ar ApprenticeRepo) CandidateStagesByFilters(ctx context.Context, search *CandidateStageSearch, pager Pager, ops ...OpFunc) (candidateStages []CandidateStage, err error) {
	err = buildQuery(ctx, ar.db, &candidateStages, search, ar.filters[Tables.CandidateStage.Name], pager, ops...).Select()
	return
}

// CountCandidateStages returns count
func (ar ApprenticeRepo) CountCandidateStages(ctx context.Context, search *CandidateStageSearch, ops ...OpFunc) (int, error) {
	return buildQuery(ctx, ar.db, &CandidateStage{}, search, ar.filters[Tables.CandidateStage.Name], PagerOne, ops...).Count()
}

// AddCandidateStage adds CandidateStage to DB.
func (ar ApprenticeRepo) AddCandidateStage(ctx context.Context, candidateStage *CandidateStage, ops ...OpFunc) (*CandidateStage, error) {
	q := ar.db.ModelContext(ctx, candidateStage)
	if len(ops) == 0 {
		q = q.ExcludeColumn(Columns.CandidateStage.CreatedAt)
	}
	applyOps(q, ops...)
	_, err := q.Insert()

	return candidateStage, err
}

// UpdateCandidateStage updates CandidateStage in DB.
func (ar ApprenticeRepo) UpdateCandidateStage(ctx context.Context, candidateStage *CandidateStage, ops ...OpFunc) (bool, error) {
	q := ar.db.ModelContext(ctx, candidateStage).WherePK()
	if len(ops) == 0 {
		q = q.ExcludeColumn(Columns.CandidateStage.ID, Columns.CandidateStage.CreatedAt)
	}
	applyOps(q, ops...)
	res, err := q.Update()
	if err != nil {
		return false, err
	}

	return res.RowsAffected() > 0, err
}

// DeleteCandidateStage deletes CandidateStage from DB.
func (ar ApprenticeRepo) DeleteCandidateStage(ctx context.Context, id int) (deleted bool, err error) {
	candidateStage := &CandidateStage{ID: id}

	res, err := ar.db.ModelContext(ctx, candidateStage).WherePK().Delete()
	if err != nil {
		return false, err
	}

	return res.RowsAffected() > 0, err
}

/*** Stage ***/

// FullStage returns full joins with all columns
func (ar ApprenticeRepo) FullStage() OpFunc {
	return WithColumns(ar.join[Tables.Stage.Name]...)
}

// DefaultStageSort returns default sort.
func (ar ApprenticeRepo) DefaultStageSort() OpFunc {
	return WithSort(ar.sort[Tables.Stage.Name]...)
}

// StageByID is a function that returns Stage by ID(s) or nil.
func (ar ApprenticeRepo) StageByID(ctx context.Context, id int, ops ...OpFunc) (*Stage, error) {
	return ar.OneStage(ctx, &StageSearch{ID: &id}, ops...)
}

// OneStage is a function that returns one Stage by filters. It could return pg.ErrMultiRows.
func (ar ApprenticeRepo) OneStage(ctx context.Context, search *StageSearch, ops ...OpFunc) (*Stage, error) {
	obj := &Stage{}
	err := buildQuery(ctx, ar.db, obj, search, ar.filters[Tables.Stage.Name], PagerTwo, ops...).Select()

	if errors.Is(err, pg.ErrMultiRows) {
		return nil, err
	} else if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}

	return obj, err
}

// StagesByFilters returns Stage list.
func (ar ApprenticeRepo) StagesByFilters(ctx context.Context, search *StageSearch, pager Pager, ops ...OpFunc) (stages []Stage, err error) {
	err = buildQuery(ctx, ar.db, &stages, search, ar.filters[Tables.Stage.Name], pager, ops...).Select()
	return
}

// CountStages returns count
func (ar ApprenticeRepo) CountStages(ctx context.Context, search *StageSearch, ops ...OpFunc) (int, error) {
	return buildQuery(ctx, ar.db, &Stage{}, search, ar.filters[Tables.Stage.Name], PagerOne, ops...).Count()
}

// AddStage adds Stage to DB.
func (ar ApprenticeRepo) AddStage(ctx context.Context, stage *Stage, ops ...OpFunc) (*Stage, error) {
	q := ar.db.ModelContext(ctx, stage)
	applyOps(q, ops...)
	_, err := q.Insert()

	return stage, err
}

// UpdateStage updates Stage in DB.
func (ar ApprenticeRepo) UpdateStage(ctx context.Context, stage *Stage, ops ...OpFunc) (bool, error) {
	q := ar.db.ModelContext(ctx, stage).WherePK()
	if len(ops) == 0 {
		q = q.ExcludeColumn(Columns.Stage.ID)
	}
	applyOps(q, ops...)
	res, err := q.Update()
	if err != nil {
		return false, err
	}

	return res.RowsAffected() > 0, err
}

// DeleteStage set statusId to deleted in DB.
func (ar ApprenticeRepo) DeleteStage(ctx context.Context, id int) (deleted bool, err error) {
	stage := &Stage{ID: id, StatusID: StatusDeleted}

	return ar.UpdateStage(ctx, stage, WithColumns(Columns.Stage.StatusID))
}
