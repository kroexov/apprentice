package rpc

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sort"
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
	ErrCandidateNotFound   = zenrpc.NewStringError(http.StatusNotFound, "candidate not found")
	ErrAlreadyScored       = zenrpc.NewStringError(http.StatusBadRequest, "stage already scored for this candidate")
	ErrAlreadyCompleted    = zenrpc.NewStringError(http.StatusBadRequest, "candidate already completed all stages")
	ErrScoreOutOfRange     = zenrpc.NewStringError(http.StatusBadRequest, "score out of range")
	ErrCannotRollback      = zenrpc.NewStringError(http.StatusBadRequest, "no previous stage to roll back to")
	ErrScoreNotFound       = zenrpc.NewStringError(http.StatusNotFound, "score not found")
	ErrInvalidCurrentStage = zenrpc.NewStringError(http.StatusBadRequest, "currentStageId references unknown stage")
	ErrHandleTaken         = zenrpc.NewStringError(http.StatusBadRequest, "handle is already taken")
)

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
	scores, err := s.repo.StageScoresByFilters(ctx, &db.StageScoreSearch{CandidateID: &id}, db.PagerNoLimit,
		db.WithColumns(db.TableColumns))
	if err != nil {
		return nil, InternalError(err)
	}

	summary := buildSummaryFor(cand, stages, scores, totalsFromStages(stages))
	currentStage := findStage(stages, cand.CurrentStageID)
	scoreByStage := indexScores(scores)

	history := make([]CandidateStageHistory, 0, len(stages))
	for i := range stages {
		st := &stages[i]
		row := CandidateStageHistory{
			StageID:  st.ID,
			Stage:    NewStage(st),
			MaxScore: st.MaxScore,
		}
		if sc, ok := scoreByStage[st.ID]; ok {
			row.Status = StageStatusDone
			score := sc.Score
			row.Score = &score
			scoredAt := sc.ScoredAt.Format(time.RFC3339)
			row.ScoredAt = &scoredAt
			scoreID := sc.ID
			row.ScoreID = &scoreID
		} else if cand.CompletedAt == nil && currentStage != nil && st.ID == currentStage.ID {
			row.Status = StageStatusCurrent
		} else {
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

// Add creates a new candidate.
//
//zenrpc:candidate Candidate
//zenrpc:return Candidate
//zenrpc:400 Validation Error
//zenrpc:500 Internal Error
func (s CandidateService) Add(ctx context.Context, candidate Candidate) (*Candidate, error) {
	if ve := s.isValid(ctx, candidate, false); ve.HasErrors() {
		return nil, ve.Error()
	}
	stage, err := s.repo.StageByID(ctx, candidate.CurrentStageID)
	if err != nil {
		return nil, InternalError(err)
	}
	if stage == nil {
		return nil, ErrInvalidCurrentStage
	}

	d := candidate.ToDB()
	d.ID = 0
	d.CompletedAt = nil
	created, err := s.repo.AddCandidate(ctx, d)
	if err != nil {
		if db.IsUniqueViolation(err) {
			return nil, ErrHandleTaken
		}
		return nil, InternalError(err)
	}

	full, err := s.repo.CandidateByID(ctx, created.ID, s.repo.FullCandidate())
	if err != nil {
		return nil, InternalError(err)
	}
	s.Print(ctx, "candidate added", "candidateId", created.ID, "handle", created.Handle, "stageId", created.CurrentStageID)
	return NewCandidate(full), nil
}

// Update changes candidate's basic fields. Use Advance/Rollback for stage progression.
//
//zenrpc:candidate Candidate
//zenrpc:return bool
//zenrpc:404 Not Found
//zenrpc:400 Validation Error
//zenrpc:500 Internal Error
func (s CandidateService) Update(ctx context.Context, candidate Candidate) (bool, error) {
	cur, err := s.repo.CandidateByID(ctx, candidate.ID)
	if err != nil {
		return false, InternalError(err)
	}
	if cur == nil {
		return false, ErrCandidateNotFound
	}
	if ve := s.isValid(ctx, candidate, true); ve.HasErrors() {
		return false, ve.Error()
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

	ok, err := s.repo.UpdateCandidate(ctx, cur, db.WithColumns(
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
			return false, ErrHandleTaken
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

// Advance scores the candidate for their current stage and moves them to the next one.
// If current stage is the last, completedAt is set instead.
//
//zenrpc:candidateId int
//zenrpc:score int
//zenrpc:return AdvanceResult
//zenrpc:400 Validation Error
//zenrpc:404 Not Found
//zenrpc:500 Internal Error
func (s CandidateService) Advance(ctx context.Context, candidateID, score int) (*AdvanceResult, error) {
	var (
		outCand  *db.Candidate
		outScore *db.StageScore
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

		ss := &db.StageScore{
			CandidateID: cand.ID,
			StageID:     stage.ID,
			Score:       score,
			ScoredAt:    time.Now(),
		}
		if _, addErr := txRepo.AddStageScore(ctx, ss); addErr != nil {
			if db.IsUniqueViolation(addErr) {
				return ErrAlreadyScored
			}
			return addErr
		}
		outScore = ss

		next, err := txRepo.NextStageAfter(ctx, stage.Order)
		if err != nil {
			return err
		}

		now := time.Now()
		cand.UpdatedAt = now
		if next == nil {
			cand.CompletedAt = &now
			if _, err := txRepo.UpdateCandidate(ctx, cand,
				db.WithColumns(db.Columns.Candidate.CompletedAt, db.Columns.Candidate.UpdatedAt)); err != nil {
				return err
			}
		} else {
			cand.CurrentStageID = next.ID
			if _, err := txRepo.UpdateCandidate(ctx, cand,
				db.WithColumns(db.Columns.Candidate.CurrentStageID, db.Columns.Candidate.UpdatedAt)); err != nil {
				return err
			}
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
		"stageId", outScore.StageID,
		"score", outScore.Score,
		"completed", outCand.CompletedAt != nil,
	)
	return &AdvanceResult{
		Candidate: NewCandidate(full),
		Score:     NewScore(outScore),
	}, nil
}

// Rate corrects an existing score without changing the candidate's current stage.
//
//zenrpc:scoreId int
//zenrpc:score int
//zenrpc:return Score
//zenrpc:400 Validation Error
//zenrpc:404 Not Found
//zenrpc:500 Internal Error
func (s CandidateService) Rate(ctx context.Context, scoreID, score int) (*Score, error) {
	cur, err := s.repo.StageScoreByID(ctx, scoreID, s.repo.FullStageScore())
	if err != nil {
		return nil, InternalError(err)
	}
	if cur == nil {
		return nil, ErrScoreNotFound
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

	cur.Score = score
	if _, err := s.repo.UpdateStageScore(ctx, cur, db.WithColumns(db.Columns.StageScore.Score)); err != nil {
		return nil, InternalError(err)
	}
	s.Print(ctx, "score corrected", "scoreId", cur.ID, "candidateId", cur.CandidateID, "stageId", cur.StageID, "score", score)
	return NewScore(cur), nil
}

// Rollback removes the candidate's most recent score and moves them one stage back.
// If the candidate was completed, completedAt is cleared.
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

		latest, err := txRepo.LatestStageScoreByCandidate(ctx, cand.ID)
		if err != nil {
			return err
		}
		if latest == nil {
			return ErrCannotRollback
		}
		if _, err := txRepo.DeleteStageScore(ctx, latest.ID); err != nil {
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

func (s CandidateService) loadAggregateInputs(ctx context.Context) ([]db.Stage, []db.Candidate, []db.StageScore, error) {
	stages, err := s.repo.StagesByFilters(ctx, nil, db.PagerNoLimit, s.repo.FullStage())
	if err != nil {
		return nil, nil, nil, InternalError(err)
	}
	candidates, err := s.repo.CandidatesByFilters(ctx, nil, db.PagerNoLimit, s.repo.FullCandidate())
	if err != nil {
		return nil, nil, nil, InternalError(err)
	}
	scores, err := s.repo.StageScoresByFilters(ctx, nil, db.PagerNoLimit, db.WithColumns(db.TableColumns))
	if err != nil {
		return nil, nil, nil, InternalError(err)
	}
	return stages, candidates, scores, nil
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

	other, err := s.repo.OneCandidate(ctx, &db.CandidateSearch{Handle: &c.Handle})
	if err != nil {
		v.SetInternalError(err)
		return v
	}
	if other != nil && (!isUpdate || other.ID != c.ID) {
		v.Append("handle", FieldErrorUnique)
	}
	return v
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

func indexScores(scores []db.StageScore) map[int]db.StageScore {
	out := make(map[int]db.StageScore, len(scores))
	for _, sc := range scores {
		out[sc.StageID] = sc
	}
	return out
}

// totalsFromStages returns sum(MaxScore) across stages — the "max points" baseline.
func totalsFromStages(stages []db.Stage) int {
	maxPoints := 0
	for _, st := range stages {
		maxPoints += st.MaxScore
	}
	return maxPoints
}

// buildSummaryFor materialises a CandidateSummary using a pre-loaded score subset
// belonging to this candidate (the caller is responsible for filtering).
func buildSummaryFor(cand *db.Candidate, stages []db.Stage, candScores []db.StageScore, maxPoints int) CandidateSummary {
	totalPoints := 0
	for _, sc := range candScores {
		totalPoints += sc.Score
	}
	currentStage := findStage(stages, cand.CurrentStageID)

	return CandidateSummary{
		ID:              cand.ID,
		Name:            cand.Name,
		Handle:          cand.Handle,
		City:            cand.City,
		Age:             cand.Age,
		AvatarColor:     cand.AvatarColor,
		Initials:        cand.Initials,
		AvatarURL:       cand.AvatarUrl,
		Strengths:       cand.Strengths,
		Weaknesses:      cand.Weaknesses,
		CurrentStage:    NewStage(currentStage),
		TotalPoints:     totalPoints,
		MaxPoints:       maxPoints,
		CompletedStages: len(candScores),
		StageCount:      len(stages),
		CompletedAt:     formatTimePtr(cand.CompletedAt),
	}
}

func buildSummaries(candidates []db.Candidate, stages []db.Stage, scores []db.StageScore) []CandidateSummary {
	scoresByCand := make(map[int][]db.StageScore, len(candidates))
	for _, sc := range scores {
		scoresByCand[sc.CandidateID] = append(scoresByCand[sc.CandidateID], sc)
	}
	maxPoints := totalsFromStages(stages)

	out := make([]CandidateSummary, 0, len(candidates))
	for i := range candidates {
		out = append(out, buildSummaryFor(&candidates[i], stages, scoresByCand[candidates[i].ID], maxPoints))
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
