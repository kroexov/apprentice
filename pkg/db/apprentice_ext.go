package db

import (
	"context"
	"errors"

	"github.com/go-pg/pg/v10"
)

// LatestStageScoreByCandidate returns the most recently scored StageScore for
// a candidate, or nil if the candidate has no scores yet.
//
// scoredAt has microsecond precision, and two Advance calls within the same
// microsecond would otherwise tie — ID is appended as a stable secondary key
// so this always returns the row that was inserted last.
func (ar ApprenticeRepo) LatestStageScoreByCandidate(ctx context.Context, candidateID int, ops ...OpFunc) (*StageScore, error) {
	combined := append([]OpFunc{
		WithColumns(TableColumns),
		WithSort(
			NewSortField(Columns.StageScore.ScoredAt, true),
			NewSortField(Columns.StageScore.ID, true),
		),
	}, ops...)
	list, err := ar.StageScoresByFilters(ctx, &StageScoreSearch{CandidateID: &candidateID}, NewPager(1, 1), combined...)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return &list[0], nil
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
