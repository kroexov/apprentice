package db

import (
	"context"
	"errors"
	"time"

	"github.com/go-pg/pg/v10"
)

// LatestScoredCandidateStageByCandidate returns the most recently scored
// CandidateStage (scoredAt IS NOT NULL) for a candidate, or nil if the
// candidate has no scored stages yet.
//
// scoredAt has microsecond precision, and two Advance calls within the same
// microsecond would otherwise tie — candidateStageId is appended as a stable
// secondary key so this always returns the row whose score was set last.
func (ar ApprenticeRepo) LatestScoredCandidateStageByCandidate(ctx context.Context, candidateID int, ops ...OpFunc) (*CandidateStage, error) {
	search := &CandidateStageSearch{CandidateID: &candidateID}
	search.With(`?TableAlias."scoredAt" IS NOT NULL`)
	combined := append([]OpFunc{
		WithColumns(TableColumns),
		WithSort(
			NewSortField(Columns.CandidateStage.ScoredAt, true),
			NewSortField(Columns.CandidateStage.ID, true),
		),
	}, ops...)
	list, err := ar.CandidateStagesByFilters(ctx, search, NewPager(1, 1), combined...)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return &list[0], nil
}

// CreateCandidateStage inserts an empty CandidateStage for (candidateID, stage)
// with deadline = now() + stage.DeadlineDays days when DeadlineDays > 0,
// otherwise NULL. createdAt is left to the DB default.
//
// Use whenever a candidate enters a stage (initial Add or Advance to next).
// Returns the inserted row with PK populated.
func (ar ApprenticeRepo) CreateCandidateStage(ctx context.Context, candidateID int, stage *Stage) (*CandidateStage, error) {
	cs := &CandidateStage{
		CandidateID: candidateID,
		StageID:     stage.ID,
	}
	if stage.DeadlineDays > 0 {
		d := time.Now().Add(time.Duration(stage.DeadlineDays) * 24 * time.Hour)
		cs.Deadline = &d
	}
	return ar.AddCandidateStage(ctx, cs)
}

// NextStageAfter returns the stage with the smallest order strictly greater
// than currentOrder, or nil if currentOrder is the last stage.
func (ar ApprenticeRepo) NextStageAfter(ctx context.Context, currentOrder int) (*Stage, error) {
	search := &StageSearch{}
	search.With(`?TableAlias."order" > ?`, currentOrder)
	list, err := ar.StagesByFilters(ctx, search, NewPager(1, 1),
		WithSort(NewSortField(Columns.Stage.Order, false)),
		WithColumns(TableColumns),
	)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return &list[0], nil
}

// UnsetReadyAndBumpRetries atomically clears isReady (and setReadyAt) and
// increments retries for a CandidateStage — but only when the row was
// actually ready at UPDATE time. Returns true when the row flipped, false
// when it was already not-ready (idempotent no-op for repeated admin
// setReady(false)).
//
// The WHERE "isReady" = true gate is what prevents lost updates: two
// concurrent admin setReady(false) calls on the same row would otherwise
// both read prevReady=true, both write retries+1, and the second UPDATE
// would clobber the first's increment. With the gate, exactly one UPDATE
// matches the row; the other matches zero rows and is a no-op.
func (ar ApprenticeRepo) UnsetReadyAndBumpRetries(ctx context.Context, candidateStageID int) (bool, error) {
	res, err := ar.db.ExecContext(ctx, `
		UPDATE "candidateStages"
		SET "isReady"    = false,
			"setReadyAt" = NULL,
			"retries"    = "retries" + 1
		WHERE "candidateStageId" = ? AND "isReady" = true
	`, candidateStageID)
	if err != nil {
		return false, err
	}
	return res.RowsAffected() > 0, nil
}

// IsUniqueViolation reports whether err is a Postgres unique-constraint error.
// Used to convert race-induced 23505 collisions into domain-specific RPC errors.
func IsUniqueViolation(err error) bool {
	var pgErr pg.Error
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Field('C') == "23505"
}

// ErrMaterialAlreadyScored is returned by SetMaterialRead(read=false) when the
// admin has already scored the row — the candidate must not be able to retract
// a verified read.
var ErrMaterialAlreadyScored = errors.New("candidate material already scored")

// SetMaterialRead toggles the candidate-side readAt flag on a CandidateMaterial.
//
// read=true is an UPSERT: the first call inserts a row with readAt=now(); a
// repeated call keeps the original readAt (so the timestamp reflects when the
// candidate first marked it read, not the latest re-mark).
//
// read=false clears readAt only when the row is not yet scored. Once an admin
// has set score/scoredAt/scoredBy, the candidate cannot retract — caller gets
// ErrMaterialAlreadyScored. The function always re-fetches the current row at
// the end so the response carries the actual post-operation state, even if a
// concurrent admin Score landed between the SELECT and UPDATE in clearMaterialRead.
func (ar ApprenticeRepo) SetMaterialRead(ctx context.Context, candidateID, materialID int, read bool) (*CandidateMaterial, error) {
	if read {
		if err := ar.markMaterialRead(ctx, candidateID, materialID); err != nil {
			return nil, err
		}
	} else if _, err := ar.clearMaterialRead(ctx, candidateID, materialID); err != nil {
		return nil, err
	}

	return ar.OneCandidateMaterial(ctx, &CandidateMaterialSearch{
		CandidateID: &candidateID,
		MaterialID:  &materialID,
	})
}

// markMaterialRead UPSERTs a candidateMaterials row, preserving the earliest
// readAt across repeats.
func (ar ApprenticeRepo) markMaterialRead(ctx context.Context, candidateID, materialID int) error {
	_, err := ar.db.ExecContext(ctx, `
		INSERT INTO "candidateMaterials" ("candidateId", "materialId", "readAt")
		VALUES (?, ?, now())
		ON CONFLICT ("candidateId", "materialId") DO UPDATE
		SET "readAt" = COALESCE("candidateMaterials"."readAt", EXCLUDED."readAt")
	`, candidateID, materialID)
	return err
}

// clearMaterialRead nulls readAt for an existing not-yet-scored row.
//
// Return contract:
//   - (true, nil) — readAt was actually flipped from non-NULL to NULL.
//   - (false, nil) — nothing changed: row does not exist, readAt was already
//     NULL, or a concurrent admin Score landed between our SELECT and UPDATE
//     (in which case the WHERE scoredAt IS NULL guard skipped the UPDATE).
//   - (false, ErrMaterialAlreadyScored) — row exists and was already scored
//     when we read it; candidate must not retract.
func (ar ApprenticeRepo) clearMaterialRead(ctx context.Context, candidateID, materialID int) (bool, error) {
	existing, err := ar.OneCandidateMaterial(ctx, &CandidateMaterialSearch{
		CandidateID: &candidateID,
		MaterialID:  &materialID,
	}, WithColumns(TableColumns))
	if err != nil {
		return false, err
	}
	if existing == nil {
		return false, nil
	}
	if existing.ScoredAt != nil {
		return false, ErrMaterialAlreadyScored
	}
	res, err := ar.db.ExecContext(ctx, `
		UPDATE "candidateMaterials"
		SET "readAt" = NULL
		WHERE "candidateId" = ? AND "materialId" = ? AND "scoredAt" IS NULL
	`, candidateID, materialID)
	if err != nil {
		return false, err
	}
	return res.RowsAffected() > 0, nil
}

// ScoreCandidateMaterial sets score/scoredAt/scoredBy on a CandidateMaterial
// — UPSERT-style so the admin can score even before the candidate has marked
// the material read. notes follows COALESCE semantics: a non-nil notes
// overwrites the stored value, but notes=nil preserves the previous comment
// (re-scoring without a fresh comment must not erase an old one).
//
// Caller is responsible for validating score against materials.maxScore.
func (ar ApprenticeRepo) ScoreCandidateMaterial(ctx context.Context, candidateID, materialID, score, scoredBy int, notes *string) (*CandidateMaterial, error) {
	_, err := ar.db.ExecContext(ctx, `
		INSERT INTO "candidateMaterials" ("candidateId", "materialId", "score", "scoredAt", "scoredBy", "notes")
		VALUES (?, ?, ?, now(), ?, ?)
		ON CONFLICT ("candidateId", "materialId") DO UPDATE
		SET "score"    = EXCLUDED."score",
		    "scoredAt" = EXCLUDED."scoredAt",
		    "scoredBy" = EXCLUDED."scoredBy",
		    "notes"    = COALESCE(EXCLUDED."notes", "candidateMaterials"."notes")
	`, candidateID, materialID, score, scoredBy, notes)
	if err != nil {
		return nil, err
	}
	return ar.OneCandidateMaterial(ctx, &CandidateMaterialSearch{
		CandidateID: &candidateID,
		MaterialID:  &materialID,
	})
}

// UnscoreCandidateMaterial clears score/scoredAt/scoredBy/notes for the given
// (candidate, material) pair. Idempotent: returns false when no scored row
// exists.
func (ar ApprenticeRepo) UnscoreCandidateMaterial(ctx context.Context, candidateID, materialID int) (bool, error) {
	res, err := ar.db.ExecContext(ctx, `
		UPDATE "candidateMaterials"
		SET "score"    = NULL,
		    "scoredAt" = NULL,
		    "scoredBy" = NULL,
		    "notes"    = NULL
		WHERE "candidateId" = ? AND "materialId" = ? AND "scoredAt" IS NOT NULL
	`, candidateID, materialID)
	if err != nil {
		return false, err
	}
	return res.RowsAffected() > 0, nil
}

// MaterialsMatrix is the snapshot of every active material, every active
// candidate, and every existing candidateMaterials row at the moment the
// matrix was read. Cells with no entry in Progress are "not marked".
//
// The three slices are designed to travel together — caller should ensure
// they came from a single transaction so the matrix is consistent (no
// orphan progress rows referring to a soft-deleted material seen mid-read).
type MaterialsMatrix struct {
	Materials  []Material
	Candidates []Candidate
	Progress   []CandidateMaterial
}

// MaterialsProgressMatrix reads the three lists from ar.db. To get a
// consistent snapshot, call inside RunInTransaction:
//
//	var matrix *db.MaterialsMatrix
//	err := dbo.RunInTransaction(ctx, func(tx *pg.Tx) error {
//	    var err error
//	    matrix, err = repo.WithTransaction(tx).MaterialsProgressMatrix(ctx)
//	    return err
//	})
func (ar ApprenticeRepo) MaterialsProgressMatrix(ctx context.Context) (*MaterialsMatrix, error) {
	materials, err := ar.MaterialsByFilters(ctx, nil, PagerNoLimit,
		WithColumns(TableColumns),
		WithSort(
			NewSortField(Columns.Material.Order, false),
			NewSortField(Columns.Material.Title, false),
		),
	)
	if err != nil {
		return nil, err
	}

	candidates, err := ar.CandidatesByFilters(ctx, nil, PagerNoLimit,
		WithColumns(TableColumns),
		WithSort(NewSortField(Columns.Candidate.Name, false)),
	)
	if err != nil {
		return nil, err
	}

	progress, err := ar.CandidateMaterialsByFilters(ctx, nil, PagerNoLimit,
		WithColumns(TableColumns),
	)
	if err != nil {
		return nil, err
	}

	return &MaterialsMatrix{
		Materials:  materials,
		Candidates: candidates,
		Progress:   progress,
	}, nil
}
