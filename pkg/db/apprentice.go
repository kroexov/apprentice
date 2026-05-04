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
			Tables.Material.Name:  {StatusFilter},
		},
		sort: map[string][]SortField{
			Tables.Candidate.Name:         {{Column: Columns.Candidate.CreatedAt, Direction: SortDesc}},
			Tables.CandidateStage.Name:    {{Column: Columns.CandidateStage.CreatedAt, Direction: SortDesc}},
			Tables.Stage.Name:             {{Column: Columns.Stage.Title, Direction: SortAsc}},
			Tables.CandidateMaterial.Name: {{Column: Columns.CandidateMaterial.CreatedAt, Direction: SortDesc}},
			Tables.Material.Name:          {{Column: Columns.Material.CreatedAt, Direction: SortDesc}},
		},
		join: map[string][]string{
			Tables.Candidate.Name:         {TableColumns, Columns.Candidate.CurrentStage},
			Tables.CandidateStage.Name:    {TableColumns, Columns.CandidateStage.Candidate, Columns.CandidateStage.Stage},
			Tables.Stage.Name:             {TableColumns},
			Tables.CandidateMaterial.Name: {TableColumns, Columns.CandidateMaterial.Candidate, Columns.CandidateMaterial.Material},
			Tables.Material.Name:          {TableColumns},
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

/*** CandidateMaterial ***/

// FullCandidateMaterial returns full joins with all columns
func (ar ApprenticeRepo) FullCandidateMaterial() OpFunc {
	return WithColumns(ar.join[Tables.CandidateMaterial.Name]...)
}

// DefaultCandidateMaterialSort returns default sort.
func (ar ApprenticeRepo) DefaultCandidateMaterialSort() OpFunc {
	return WithSort(ar.sort[Tables.CandidateMaterial.Name]...)
}

// CandidateMaterialByID is a function that returns CandidateMaterial by ID(s) or nil.
func (ar ApprenticeRepo) CandidateMaterialByID(ctx context.Context, id int, ops ...OpFunc) (*CandidateMaterial, error) {
	return ar.OneCandidateMaterial(ctx, &CandidateMaterialSearch{ID: &id}, ops...)
}

// OneCandidateMaterial is a function that returns one CandidateMaterial by filters. It could return pg.ErrMultiRows.
func (ar ApprenticeRepo) OneCandidateMaterial(ctx context.Context, search *CandidateMaterialSearch, ops ...OpFunc) (*CandidateMaterial, error) {
	obj := &CandidateMaterial{}
	err := buildQuery(ctx, ar.db, obj, search, ar.filters[Tables.CandidateMaterial.Name], PagerTwo, ops...).Select()

	if errors.Is(err, pg.ErrMultiRows) {
		return nil, err
	} else if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}

	return obj, err
}

// CandidateMaterialsByFilters returns CandidateMaterial list.
func (ar ApprenticeRepo) CandidateMaterialsByFilters(ctx context.Context, search *CandidateMaterialSearch, pager Pager, ops ...OpFunc) (candidateMaterials []CandidateMaterial, err error) {
	err = buildQuery(ctx, ar.db, &candidateMaterials, search, ar.filters[Tables.CandidateMaterial.Name], pager, ops...).Select()
	return
}

// CountCandidateMaterials returns count
func (ar ApprenticeRepo) CountCandidateMaterials(ctx context.Context, search *CandidateMaterialSearch, ops ...OpFunc) (int, error) {
	return buildQuery(ctx, ar.db, &CandidateMaterial{}, search, ar.filters[Tables.CandidateMaterial.Name], PagerOne, ops...).Count()
}

// AddCandidateMaterial adds CandidateMaterial to DB.
func (ar ApprenticeRepo) AddCandidateMaterial(ctx context.Context, candidateMaterial *CandidateMaterial, ops ...OpFunc) (*CandidateMaterial, error) {
	q := ar.db.ModelContext(ctx, candidateMaterial)
	if len(ops) == 0 {
		q = q.ExcludeColumn(Columns.CandidateMaterial.CreatedAt)
	}
	applyOps(q, ops...)
	_, err := q.Insert()

	return candidateMaterial, err
}

// UpdateCandidateMaterial updates CandidateMaterial in DB.
func (ar ApprenticeRepo) UpdateCandidateMaterial(ctx context.Context, candidateMaterial *CandidateMaterial, ops ...OpFunc) (bool, error) {
	q := ar.db.ModelContext(ctx, candidateMaterial).WherePK()
	if len(ops) == 0 {
		q = q.ExcludeColumn(Columns.CandidateMaterial.ID, Columns.CandidateMaterial.CreatedAt)
	}
	applyOps(q, ops...)
	res, err := q.Update()
	if err != nil {
		return false, err
	}

	return res.RowsAffected() > 0, err
}

// DeleteCandidateMaterial deletes CandidateMaterial from DB.
func (ar ApprenticeRepo) DeleteCandidateMaterial(ctx context.Context, id int) (deleted bool, err error) {
	candidateMaterial := &CandidateMaterial{ID: id}

	res, err := ar.db.ModelContext(ctx, candidateMaterial).WherePK().Delete()
	if err != nil {
		return false, err
	}

	return res.RowsAffected() > 0, err
}

/*** Material ***/

// FullMaterial returns full joins with all columns
func (ar ApprenticeRepo) FullMaterial() OpFunc {
	return WithColumns(ar.join[Tables.Material.Name]...)
}

// DefaultMaterialSort returns default sort.
func (ar ApprenticeRepo) DefaultMaterialSort() OpFunc {
	return WithSort(ar.sort[Tables.Material.Name]...)
}

// MaterialByID is a function that returns Material by ID(s) or nil.
func (ar ApprenticeRepo) MaterialByID(ctx context.Context, id int, ops ...OpFunc) (*Material, error) {
	return ar.OneMaterial(ctx, &MaterialSearch{ID: &id}, ops...)
}

// OneMaterial is a function that returns one Material by filters. It could return pg.ErrMultiRows.
func (ar ApprenticeRepo) OneMaterial(ctx context.Context, search *MaterialSearch, ops ...OpFunc) (*Material, error) {
	obj := &Material{}
	err := buildQuery(ctx, ar.db, obj, search, ar.filters[Tables.Material.Name], PagerTwo, ops...).Select()

	if errors.Is(err, pg.ErrMultiRows) {
		return nil, err
	} else if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}

	return obj, err
}

// MaterialsByFilters returns Material list.
func (ar ApprenticeRepo) MaterialsByFilters(ctx context.Context, search *MaterialSearch, pager Pager, ops ...OpFunc) (materials []Material, err error) {
	err = buildQuery(ctx, ar.db, &materials, search, ar.filters[Tables.Material.Name], pager, ops...).Select()
	return
}

// CountMaterials returns count
func (ar ApprenticeRepo) CountMaterials(ctx context.Context, search *MaterialSearch, ops ...OpFunc) (int, error) {
	return buildQuery(ctx, ar.db, &Material{}, search, ar.filters[Tables.Material.Name], PagerOne, ops...).Count()
}

// AddMaterial adds Material to DB.
func (ar ApprenticeRepo) AddMaterial(ctx context.Context, material *Material, ops ...OpFunc) (*Material, error) {
	q := ar.db.ModelContext(ctx, material)
	if len(ops) == 0 {
		q = q.ExcludeColumn(Columns.Material.CreatedAt)
	}
	applyOps(q, ops...)
	_, err := q.Insert()

	return material, err
}

// UpdateMaterial updates Material in DB.
func (ar ApprenticeRepo) UpdateMaterial(ctx context.Context, material *Material, ops ...OpFunc) (bool, error) {
	q := ar.db.ModelContext(ctx, material).WherePK()
	if len(ops) == 0 {
		q = q.ExcludeColumn(Columns.Material.ID, Columns.Material.CreatedAt)
	}
	applyOps(q, ops...)
	res, err := q.Update()
	if err != nil {
		return false, err
	}

	return res.RowsAffected() > 0, err
}

// DeleteMaterial set statusId to deleted in DB.
func (ar ApprenticeRepo) DeleteMaterial(ctx context.Context, id int) (deleted bool, err error) {
	material := &Material{ID: id, StatusID: StatusDeleted}

	return ar.UpdateMaterial(ctx, material, WithColumns(Columns.Material.StatusID))
}
