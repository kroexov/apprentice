package rpc

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"apisrv/pkg/db"

	"github.com/go-pg/pg/v10"
	"github.com/vmkteam/embedlog"
	"github.com/vmkteam/zenrpc/v2"
)

var (
	ErrMaterialNotFound       = zenrpc.NewStringError(http.StatusNotFound, "material not found")
	ErrMaterialTitleRequired  = zenrpc.NewStringError(http.StatusBadRequest, "title is required")
	ErrMaterialTitleTaken     = zenrpc.NewStringError(http.StatusBadRequest, "title is already taken")
	ErrMaterialTypeInvalid    = zenrpc.NewStringError(http.StatusBadRequest, "type must be one of book/article/video/test/other")
	ErrMaterialURLRequired    = zenrpc.NewStringError(http.StatusBadRequest, "url is required")
	ErrMaterialMaxScoreRange  = zenrpc.NewStringError(http.StatusBadRequest, "maxScore must be between 1 and 100")
	ErrMaterialAlreadyScored  = zenrpc.NewStringError(http.StatusBadRequest, "material is already scored, candidate cannot retract")
	ErrCandidateMaterialEmpty = zenrpc.NewStringError(http.StatusNotFound, "candidate has no progress on this material")
)

const materialURLMaxLen = 2048

var materialAllowedTypes = map[string]struct{}{
	"book":    {},
	"article": {},
	"video":   {},
	"test":    {},
	"other":   {},
}

type MaterialService struct {
	zenrpc.Service
	embedlog.Logger

	dbo  db.DB
	repo db.ApprenticeRepo
}

func NewMaterialService(dbo db.DB, logger embedlog.Logger) *MaterialService {
	return &MaterialService{
		dbo:    dbo,
		repo:   db.NewApprenticeRepo(dbo),
		Logger: logger,
	}
}

// Get returns the active materials catalog ordered by order, then title.
//
//zenrpc:view *ViewOps
//zenrpc:return []Material
//zenrpc:500 Internal Error
func (s MaterialService) Get(ctx context.Context, view *ViewOps) ([]Material, error) {
	list, err := s.repo.MaterialsByFilters(ctx, nil, view.Pager(),
		db.WithColumns(db.TableColumns),
		db.WithSort(
			db.NewSortField(db.Columns.Material.Order, false),
			db.NewSortField(db.Columns.Material.Title, false),
		),
	)
	if err != nil {
		return nil, InternalError(err)
	}
	out := make([]Material, 0, len(list))
	for i := range list {
		out = append(out, *NewMaterial(&list[i]))
	}
	return out, nil
}

// GetByID returns a single material by id.
//
//zenrpc:id int
//zenrpc:return Material
//zenrpc:404 Not Found
//zenrpc:500 Internal Error
func (s MaterialService) GetByID(ctx context.Context, id int) (*Material, error) {
	m, err := s.repo.MaterialByID(ctx, id, db.WithColumns(db.TableColumns))
	if err != nil {
		return nil, InternalError(err)
	}
	if m == nil {
		return nil, ErrMaterialNotFound
	}
	return NewMaterial(m), nil
}

// Add creates a material in the catalog (admin only).
//
//zenrpc:input MaterialInput
//zenrpc:return Material
//zenrpc:400 Validation Error
//zenrpc:500 Internal Error
func (s MaterialService) Add(ctx context.Context, input MaterialInput) (*Material, error) {
	d, err := materialFromInput(input, nil)
	if err != nil {
		return nil, err
	}
	if input.Order == nil {
		nextOrder, ordErr := s.nextMaterialOrder(ctx)
		if ordErr != nil {
			return nil, InternalError(ordErr)
		}
		d.Order = nextOrder
	}
	created, err := s.repo.AddMaterial(ctx, d)
	if err != nil {
		if db.IsUniqueViolation(err) {
			return nil, ErrMaterialTitleTaken
		}
		return nil, InternalError(err)
	}
	s.Print(ctx, "material added", "materialId", created.ID, "type", created.Type, "order", created.Order)
	return NewMaterial(created), nil
}

// nextMaterialOrder returns MAX("order") + 1 across active materials, or 1 if
// the catalog is empty. Used when admin omits MaterialInput.Order on Add so
// the new row lands at the end of the list without colliding with the
// partial-unique materials_order_key.
func (s MaterialService) nextMaterialOrder(ctx context.Context) (int, error) {
	last, err := s.repo.MaterialsByFilters(ctx, nil, db.PagerOne,
		db.WithColumns(db.TableColumns),
		db.WithSort(db.NewSortField(db.Columns.Material.Order, true)),
	)
	if err != nil {
		return 0, err
	}
	if len(last) == 0 {
		return 1, nil
	}
	return last[0].Order + 1, nil
}

// Update edits a material (admin only). All fields from input replace the
// stored values.
//
//zenrpc:id int
//zenrpc:input MaterialInput
//zenrpc:return Material
//zenrpc:400 Validation Error
//zenrpc:404 Not Found
//zenrpc:500 Internal Error
func (s MaterialService) Update(ctx context.Context, id int, input MaterialInput) (*Material, error) {
	cur, err := s.repo.MaterialByID(ctx, id, db.WithColumns(db.TableColumns))
	if err != nil {
		return nil, InternalError(err)
	}
	if cur == nil {
		return nil, ErrMaterialNotFound
	}
	d, err := materialFromInput(input, cur)
	if err != nil {
		return nil, err
	}
	if _, upErr := s.repo.UpdateMaterial(ctx, d, db.WithColumns(
		db.Columns.Material.Title,
		db.Columns.Material.Type,
		db.Columns.Material.Url,
		db.Columns.Material.Description,
		db.Columns.Material.MaxScore,
		db.Columns.Material.Order,
		db.Columns.Material.UpdatedAt,
	)); upErr != nil {
		if db.IsUniqueViolation(upErr) {
			return nil, ErrMaterialTitleTaken
		}
		return nil, InternalError(upErr)
	}
	updated, err := s.repo.MaterialByID(ctx, id, db.WithColumns(db.TableColumns))
	if err != nil {
		return nil, InternalError(err)
	}
	s.Print(ctx, "material updated", "materialId", id)
	return NewMaterial(updated), nil
}

// Delete soft-deletes a material (admin only). Existing candidateMaterials
// rows remain intact for historical reporting.
//
//zenrpc:id int
//zenrpc:return bool
//zenrpc:404 Not Found
//zenrpc:500 Internal Error
func (s MaterialService) Delete(ctx context.Context, id int) (bool, error) {
	cur, err := s.repo.MaterialByID(ctx, id, db.WithColumns(db.TableColumns))
	if err != nil {
		return false, InternalError(err)
	}
	if cur == nil {
		return false, ErrMaterialNotFound
	}
	deleted, err := s.repo.DeleteMaterial(ctx, id)
	if err != nil {
		return false, InternalError(err)
	}
	s.Print(ctx, "material deleted", "materialId", id)
	return deleted, nil
}

// SetRead toggles the candidate-side readAt flag for a material. read=true is
// idempotent: the original readAt is preserved on repeat calls. read=false is
// rejected once the admin has scored the material.
//
//zenrpc:materialId int
//zenrpc:read bool
//zenrpc:return CandidateMaterial
//zenrpc:401 Unauthorized
//zenrpc:403 Forbidden
//zenrpc:404 Not Found
//zenrpc:400 Validation Error
//zenrpc:500 Internal Error
func (s MaterialService) SetRead(ctx context.Context, materialID int, read bool) (*CandidateMaterial, error) {
	cand := CandidateFromContext(ctx)
	if cand == nil {
		return nil, ErrForbidden
	}
	mat, err := s.repo.MaterialByID(ctx, materialID, db.WithColumns(db.TableColumns))
	if err != nil {
		return nil, InternalError(err)
	}
	if mat == nil {
		return nil, ErrMaterialNotFound
	}
	updated, err := s.repo.SetMaterialRead(ctx, cand.ID, materialID, read)
	if err != nil {
		if errors.Is(err, db.ErrMaterialAlreadyScored) {
			return nil, ErrMaterialAlreadyScored
		}
		return nil, InternalError(err)
	}
	if updated == nil {
		return nil, ErrCandidateMaterialEmpty
	}
	s.Print(ctx, "material read set",
		"candidateId", cand.ID, "materialId", materialID, "read", read)
	return NewCandidateMaterial(updated), nil
}

// GetProgress returns the materials × candidates × progress matrix data.
// Public read — same tier as dashboard.Summary and candidate.Get. The three
// lists are read inside a single transaction so the matrix is consistent
// (no orphan progress rows pointing at a soft-deleted material seen mid-read).
//
//zenrpc:return MaterialsProgress
//zenrpc:500 Internal Error
func (s MaterialService) GetProgress(ctx context.Context) (*MaterialsProgress, error) {
	var matrix *db.MaterialsMatrix
	err := s.dbo.RunInTransaction(ctx, func(tx *pg.Tx) error {
		var txErr error
		matrix, txErr = s.repo.WithTransaction(tx).MaterialsProgressMatrix(ctx)
		return txErr
	})
	if err != nil {
		return nil, InternalError(err)
	}

	out := &MaterialsProgress{
		Materials:  make([]Material, 0, len(matrix.Materials)),
		Candidates: make([]CandidateBrief, 0, len(matrix.Candidates)),
		Progress:   make([]CandidateMaterial, 0, len(matrix.Progress)),
	}
	for i := range matrix.Materials {
		out.Materials = append(out.Materials, *NewMaterial(&matrix.Materials[i]))
	}
	for i := range matrix.Candidates {
		out.Candidates = append(out.Candidates, *NewCandidateBrief(&matrix.Candidates[i]))
	}
	for i := range matrix.Progress {
		out.Progress = append(out.Progress, *NewCandidateMaterial(&matrix.Progress[i]))
	}
	return out, nil
}

// GetMyProgress returns the current candidate's progress on every active
// material. nil Progress = candidate has not marked the material yet.
//
//zenrpc:return []MyMaterialProgress
//zenrpc:401 Unauthorized
//zenrpc:500 Internal Error
func (s MaterialService) GetMyProgress(ctx context.Context) ([]MyMaterialProgress, error) {
	cand := CandidateFromContext(ctx)
	if cand == nil {
		return nil, ErrForbidden
	}
	materials, err := s.repo.MaterialsByFilters(ctx, nil, db.PagerNoLimit,
		db.WithColumns(db.TableColumns),
		db.WithSort(
			db.NewSortField(db.Columns.Material.Order, false),
			db.NewSortField(db.Columns.Material.Title, false),
		),
	)
	if err != nil {
		return nil, InternalError(err)
	}
	progress, err := s.repo.CandidateMaterialsByFilters(ctx,
		&db.CandidateMaterialSearch{CandidateID: &cand.ID}, db.PagerNoLimit,
		db.WithColumns(db.TableColumns),
	)
	if err != nil {
		return nil, InternalError(err)
	}
	byMaterial := make(map[int]*db.CandidateMaterial, len(progress))
	for i := range progress {
		byMaterial[progress[i].MaterialID] = &progress[i]
	}
	out := make([]MyMaterialProgress, 0, len(materials))
	for i := range materials {
		row := MyMaterialProgress{Material: *NewMaterial(&materials[i])}
		if cm, ok := byMaterial[materials[i].ID]; ok {
			row.Progress = NewCandidateMaterial(cm)
		}
		out = append(out, row)
	}
	return out, nil
}

// Score sets score/scoredAt/scoredBy/notes on a candidateMaterial (admin only,
// UPSERT-style). Score must be within [1, materials.maxScore].
//
//zenrpc:candidateId int
//zenrpc:materialId int
//zenrpc:score int
//zenrpc:notes *string
//zenrpc:return CandidateMaterial
//zenrpc:400 Validation Error
//zenrpc:404 Not Found
//zenrpc:500 Internal Error
func (s MaterialService) Score(ctx context.Context, candidateID, materialID, score int, notes *string) (*CandidateMaterial, error) {
	admin := AdminFromContext(ctx)
	if admin == nil {
		return nil, ErrUnauthorized
	}
	cand, err := s.repo.CandidateByID(ctx, candidateID, db.WithColumns(db.TableColumns))
	if err != nil {
		return nil, InternalError(err)
	}
	if cand == nil {
		return nil, ErrCandidateNotFound
	}
	mat, err := s.repo.MaterialByID(ctx, materialID, db.WithColumns(db.TableColumns))
	if err != nil {
		return nil, InternalError(err)
	}
	if mat == nil {
		return nil, ErrMaterialNotFound
	}
	if score < 1 || score > mat.MaxScore {
		return nil, ErrScoreOutOfRange
	}
	cleanedNotes := normalizeNotes(notes)
	updated, err := s.repo.ScoreCandidateMaterial(ctx, candidateID, materialID, score, admin.ID, cleanedNotes)
	if err != nil {
		return nil, InternalError(err)
	}
	s.Print(ctx, "candidate material scored",
		"candidateId", candidateID, "materialId", materialID, "score", score, "scoredBy", admin.ID)
	return NewCandidateMaterial(updated), nil
}

// Unscore clears score/scoredAt/scoredBy/notes on a candidateMaterial. Returns
// false if no scored row existed (idempotent).
//
//zenrpc:candidateId int
//zenrpc:materialId int
//zenrpc:return bool
//zenrpc:404 Not Found
//zenrpc:500 Internal Error
func (s MaterialService) Unscore(ctx context.Context, candidateID, materialID int) (bool, error) {
	cleared, err := s.repo.UnscoreCandidateMaterial(ctx, candidateID, materialID)
	if err != nil {
		return false, InternalError(err)
	}
	s.Print(ctx, "candidate material unscored",
		"candidateId", candidateID, "materialId", materialID, "cleared", cleared)
	return cleared, nil
}

// materialFromInput validates input and returns a db.Material ready for
// AddMaterial / UpdateMaterial. cur is the existing row when updating; nil
// when adding.
func materialFromInput(in MaterialInput, cur *db.Material) (*db.Material, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, ErrMaterialTitleRequired
	}
	if _, ok := materialAllowedTypes[in.Type]; !ok {
		return nil, ErrMaterialTypeInvalid
	}
	urlVal := strings.TrimSpace(in.URL)
	if urlVal == "" {
		return nil, ErrMaterialURLRequired
	}
	normalized, err := normalizeLink(&urlVal)
	if err != nil || normalized == nil {
		return nil, ErrMaterialURLRequired
	}
	urlVal = *normalized
	if len(urlVal) > materialURLMaxLen {
		return nil, ErrMaterialURLRequired
	}
	maxScore := 10
	if in.MaxScore != nil {
		maxScore = *in.MaxScore
	} else if cur != nil {
		maxScore = cur.MaxScore
	}
	if maxScore < 1 || maxScore > 100 {
		return nil, ErrMaterialMaxScoreRange
	}
	order := 0
	if in.Order != nil {
		order = *in.Order
	} else if cur != nil {
		order = cur.Order
	}
	d := &db.Material{
		Title:       title,
		Type:        in.Type,
		Url:         urlVal,
		Description: in.Description,
		MaxScore:    maxScore,
		Order:       order,
		StatusID:    db.StatusEnabled,
	}
	if cur != nil {
		d.ID = cur.ID
	}
	return d, nil
}

func normalizeNotes(notes *string) *string {
	if notes == nil {
		return nil
	}
	v := strings.TrimSpace(*notes)
	if v == "" {
		return nil
	}
	return &v
}
