package rpc

import (
	"testing"
	"time"

	"apisrv/pkg/db"
)

// TestBuildSummaryFor_PicksCurrentStage covers the regression that came from
// using max(stageId) as a proxy for "current stage": after stage.reorder, a
// candidate's CurrentStageID can have a stageId smaller than a later-scored
// stage's, so the heuristic returned the wrong row. The fix matches by
// CurrentStageID directly. This is a pure-function test — no DB needed, so it
// runs in `make test-short`.
func TestBuildSummaryFor_PicksCurrentStage(t *testing.T) {
	stages := []db.Stage{
		{ID: 1, Alias: "draft", Order: 1, MaxScore: 10},
		{ID: 4, Alias: "first-mr", Order: 5, MaxScore: 10},      // current (stageId 4, but order 5)
		{ID: 17, Alias: "architecture", Order: 4, MaxScore: 10}, // already scored, but stageId > current
	}
	created := time.Date(2026, 5, 12, 2, 12, 0, 0, time.UTC)
	scored := time.Date(2026, 5, 11, 23, 0, 0, 0, time.UTC)
	score := 8
	link := "https://example.com/wakatime"

	candStages := []db.CandidateStage{
		{ID: 1, CandidateID: 1, StageID: 1, Score: &score, ScoredAt: &scored},
		// architecture row was scored — has biggest stageId among candidate's rows,
		// which used to "win" under the old max-stageId rule.
		{ID: 31, CandidateID: 1, StageID: 17, Score: &score, ScoredAt: &scored, CreatedAt: created},
		// candidate's actual current stage — the one with link/isReady that the
		// API must surface in currentCandidateStage.
		{ID: 33, CandidateID: 1, StageID: 4, Link: &link, IsReady: true, CreatedAt: created},
	}
	cand := &db.Candidate{ID: 1, CurrentStageID: 4}

	summary := buildSummaryFor(cand, stages, candStages, totalsFromStages(stages))

	if summary.CurrentCandidateStage == nil {
		t.Fatalf("currentCandidateStage is nil; expected the (1,4) row")
	}
	if got := summary.CurrentCandidateStage.Link; got == nil || *got != link {
		t.Fatalf("link mismatch: got %v, want %q", got, link)
	}
	if !summary.CurrentCandidateStage.IsReady {
		t.Fatalf("isReady mismatch: got false, want true")
	}
	if summary.CurrentStage == nil || summary.CurrentStage.ID != 4 {
		t.Fatalf("currentStage mismatch: got %+v, want id=4", summary.CurrentStage)
	}
	// Two scored rows (stage 1 and stage 17), each with score=8 → 16 / completed=2.
	if summary.TotalPoints != 16 || summary.CompletedStages != 2 {
		t.Fatalf("aggregates mismatch: totalPoints=%d completedStages=%d, want 16/2",
			summary.TotalPoints, summary.CompletedStages)
	}
}

// TestBuildSummaryFor_NoCurrentRow guards the case where the candidate has no
// CandidateStage row for their CurrentStageID (data anomaly). The summary must
// still build, with currentCandidateStage=nil, instead of falling back to some
// other stage's row.
func TestBuildSummaryFor_NoCurrentRow(t *testing.T) {
	stages := []db.Stage{
		{ID: 1, Order: 1, MaxScore: 10},
		{ID: 2, Order: 2, MaxScore: 10},
	}
	score := 5
	candStages := []db.CandidateStage{
		{ID: 1, CandidateID: 1, StageID: 1, Score: &score},
	}
	cand := &db.Candidate{ID: 1, CurrentStageID: 2}

	summary := buildSummaryFor(cand, stages, candStages, totalsFromStages(stages))
	if summary.CurrentCandidateStage != nil {
		t.Fatalf("expected nil currentCandidateStage for missing row, got %+v",
			summary.CurrentCandidateStage)
	}
}
