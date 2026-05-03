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

// IsUniqueViolation reports whether err is a Postgres unique-constraint error.
// Used to convert race-induced 23505 collisions into domain-specific RPC errors.
func IsUniqueViolation(err error) bool {
	var pgErr pg.Error
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Field('C') == "23505"
}
