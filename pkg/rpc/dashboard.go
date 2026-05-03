package rpc

import (
	"context"

	"apisrv/pkg/db"

	"github.com/vmkteam/embedlog"
	"github.com/vmkteam/zenrpc/v2"
)

type DashboardService struct {
	zenrpc.Service
	embedlog.Logger

	repo db.ApprenticeRepo
}

func NewDashboardService(dbo db.DB, logger embedlog.Logger) *DashboardService {
	return &DashboardService{
		repo:   db.NewApprenticeRepo(dbo),
		Logger: logger,
	}
}

// Summary returns aggregated counters for the page header.
//
//zenrpc:return Summary
//zenrpc:500 Internal Error
func (s DashboardService) Summary(ctx context.Context) (*Summary, error) {
	stages, err := s.repo.StagesByFilters(ctx, nil, db.PagerNoLimit, s.repo.FullStage())
	if err != nil {
		return nil, InternalError(err)
	}
	candidates, err := s.repo.CandidatesByFilters(ctx, nil, db.PagerNoLimit, s.repo.FullCandidate())
	if err != nil {
		return nil, InternalError(err)
	}
	cstages, err := s.repo.CandidateStagesByFilters(ctx, nil, db.PagerNoLimit, db.WithColumns(db.TableColumns))
	if err != nil {
		return nil, InternalError(err)
	}

	stageMaxSum := 0
	for _, st := range stages {
		stageMaxSum += st.MaxScore
	}

	totalPoints := 0
	for _, cs := range cstages {
		if cs.Score != nil {
			totalPoints += *cs.Score
		}
	}

	stageOrder := make(map[int]int, len(stages))
	for _, st := range stages {
		stageOrder[st.ID] = st.Order
	}
	totalOrder := 0
	for _, c := range candidates {
		totalOrder += stageOrder[c.CurrentStageID]
	}

	out := &Summary{
		CandidatesCount: len(candidates),
		StagesCount:     len(stages),
		TotalPoints:     totalPoints,
		MaxPoints:       len(candidates) * stageMaxSum,
	}
	if len(candidates) > 0 {
		out.AverageOrder = float64(totalOrder) / float64(len(candidates))
	}
	return out, nil
}
