package rpc

import (
	"time"

	"apisrv/pkg/db"
)

// ============================================================================
// Pagination / sort
// ============================================================================

const maxPageSize = 500

type ViewOps struct {
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

func (v *ViewOps) Pager() db.Pager {
	if v == nil {
		return db.PagerDefault
	}
	if v.PageSize > maxPageSize {
		v.PageSize = maxPageSize
	} else if v.PageSize < 1 {
		v.PageSize = 1
	}
	return db.Pager{Page: v.Page, PageSize: v.PageSize}
}

// CandidateSort — режим сортировки списка кандидатов.
type CandidateSort string

const (
	CandidateSortPoints CandidateSort = "points"
	CandidateSortStage  CandidateSort = "stage"
	CandidateSortName   CandidateSort = "name"
)

// formatTime / formatTimePtr render time as RFC3339; the *Ptr variant returns
// nil for nil input so omitted timestamps survive the JSON round-trip.
func formatTime(t time.Time) string {
	return t.Format(time.RFC3339)
}

func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

// ============================================================================
// Stage
// ============================================================================

type Stage struct {
	ID          int    `json:"id"`
	Alias       string `json:"alias"`
	Order       int    `json:"order"`
	Title       string `json:"title"`
	ShortTitle  string `json:"shortTitle"`
	Description string `json:"description"`
	MaxScore    int    `json:"maxScore"`
}

func NewStage(d *db.Stage) *Stage {
	if d == nil {
		return nil
	}
	return &Stage{
		ID:          d.ID,
		Alias:       d.Alias,
		Order:       d.Order,
		Title:       d.Title,
		ShortTitle:  d.ShortTitle,
		Description: d.Description,
		MaxScore:    d.MaxScore,
	}
}

func (s *Stage) ToDB() *db.Stage {
	if s == nil {
		return nil
	}
	return &db.Stage{
		ID:          s.ID,
		Alias:       s.Alias,
		Order:       s.Order,
		Title:       s.Title,
		ShortTitle:  s.ShortTitle,
		Description: s.Description,
		MaxScore:    s.MaxScore,
		StatusID:    db.StatusEnabled,
	}
}

// ============================================================================
// Candidate
// ============================================================================

// Candidate is the public DTO for a candidate.
//
// API contract: Bio / Strengths / Weaknesses are plain-text fields. The
// backend never escapes or sanitises them — UI clients are responsible for
// rendering them as text (not raw HTML) to avoid XSS.
type Candidate struct {
	ID             int      `json:"id"`
	Name           string   `json:"name"`
	Handle         string   `json:"handle"`
	City           string   `json:"city"`
	Age            *int     `json:"age"`
	Bio            string   `json:"bio"`
	AvatarColor    string   `json:"avatarColor"`
	Initials       string   `json:"initials"`
	AvatarURL      *string  `json:"avatarUrl"`
	Strengths      []string `json:"strengths"`
	Weaknesses     []string `json:"weaknesses"`
	CurrentStageID int      `json:"currentStageId"`
	CreatedAt      string   `json:"createdAt"`
	UpdatedAt      string   `json:"updatedAt"`
	CompletedAt    *string  `json:"completedAt"`
}

func NewCandidate(d *db.Candidate) *Candidate {
	if d == nil {
		return nil
	}
	return &Candidate{
		ID:             d.ID,
		Name:           d.Name,
		Handle:         d.Handle,
		City:           d.City,
		Age:            d.Age,
		Bio:            d.Bio,
		AvatarColor:    d.AvatarColor,
		Initials:       d.Initials,
		AvatarURL:      d.AvatarUrl,
		Strengths:      d.Strengths,
		Weaknesses:     d.Weaknesses,
		CurrentStageID: d.CurrentStageID,
		CreatedAt:      formatTime(d.CreatedAt),
		UpdatedAt:      formatTime(d.UpdatedAt),
		CompletedAt:    formatTimePtr(d.CompletedAt),
	}
}

// ToDB normalises and converts a public Candidate to a db.Candidate. nil
// Strengths/Weaknesses become empty slices so the column always carries
// `text[]` instead of NULL — the only normaliser, all callers go through it.
func (c *Candidate) ToDB() *db.Candidate {
	if c == nil {
		return nil
	}
	return &db.Candidate{
		ID:             c.ID,
		Name:           c.Name,
		Handle:         c.Handle,
		City:           c.City,
		Age:            c.Age,
		Bio:            c.Bio,
		AvatarColor:    c.AvatarColor,
		Initials:       c.Initials,
		AvatarUrl:      c.AvatarURL,
		Strengths:      nilToEmpty(c.Strengths),
		Weaknesses:     nilToEmpty(c.Weaknesses),
		CurrentStageID: c.CurrentStageID,
		StatusID:       db.StatusEnabled,
	}
}

func nilToEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// CandidateStageHistory — одна строка в истории этапов кандидата.
type CandidateStageHistory struct {
	StageID  int     `json:"stageId"`
	Stage    *Stage  `json:"stage"`
	Status   string  `json:"status"`
	Score    *int    `json:"score"`
	MaxScore int     `json:"maxScore"`
	ScoredAt *string `json:"scoredAt"`
	ScoreID  *int    `json:"scoreId"`
}

const (
	StageStatusDone    = "done"
	StageStatusCurrent = "current"
	StageStatusTodo    = "todo"
)

// CandidateSummary — кандидат с агрегатами для списка/канбана.
type CandidateSummary struct {
	ID              int      `json:"id"`
	Name            string   `json:"name"`
	Handle          string   `json:"handle"`
	City            string   `json:"city"`
	Age             *int     `json:"age"`
	AvatarColor     string   `json:"avatarColor"`
	Initials        string   `json:"initials"`
	AvatarURL       *string  `json:"avatarUrl"`
	Strengths       []string `json:"strengths"`
	Weaknesses      []string `json:"weaknesses"`
	CurrentStage    *Stage   `json:"currentStage"`
	TotalPoints     int      `json:"totalPoints"`
	MaxPoints       int      `json:"maxPoints"`
	CompletedStages int      `json:"completedStages"`
	StageCount      int      `json:"stageCount"`
	CompletedAt     *string  `json:"completedAt"`
}

// CandidateDetail — карточка кандидата с историей этапов.
type CandidateDetail struct {
	CandidateSummary
	Bio     string                  `json:"bio"`
	History []CandidateStageHistory `json:"history"`
}

// KanbanColumn — кандидаты, сгруппированные по этапу.
type KanbanColumn struct {
	Stage      *Stage             `json:"stage"`
	Candidates []CandidateSummary `json:"candidates"`
}

// Score — выставленная оценка.
type Score struct {
	ID          int    `json:"id"`
	CandidateID int    `json:"candidateId"`
	StageID     int    `json:"stageId"`
	Score       int    `json:"score"`
	ScoredAt    string `json:"scoredAt"`
}

func NewScore(d *db.StageScore) *Score {
	if d == nil {
		return nil
	}
	return &Score{
		ID:          d.ID,
		CandidateID: d.CandidateID,
		StageID:     d.StageID,
		Score:       d.Score,
		ScoredAt:    formatTime(d.ScoredAt),
	}
}

// AdvanceResult — результат перевода кандидата на следующий этап.
type AdvanceResult struct {
	Candidate *Candidate `json:"candidate"`
	Score     *Score     `json:"score"`
}

// ============================================================================
// Summary
// ============================================================================

type Summary struct {
	CandidatesCount int     `json:"candidatesCount"`
	StagesCount     int     `json:"stagesCount"`
	TotalPoints     int     `json:"totalPoints"`
	MaxPoints       int     `json:"maxPoints"`
	AverageOrder    float64 `json:"averageOrder"`
}
