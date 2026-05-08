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
	ID           int    `json:"id"`
	Alias        string `json:"alias"`
	Order        int    `json:"order"`
	Title        string `json:"title"`
	ShortTitle   string `json:"shortTitle"`
	Description  string `json:"description"`
	MaxScore     int    `json:"maxScore"`
	DeadlineDays int    `json:"deadlineDays"`
}

func NewStage(d *db.Stage) *Stage {
	if d == nil {
		return nil
	}
	return &Stage{
		ID:           d.ID,
		Alias:        d.Alias,
		Order:        d.Order,
		Title:        d.Title,
		ShortTitle:   d.ShortTitle,
		Description:  d.Description,
		MaxScore:     d.MaxScore,
		DeadlineDays: d.DeadlineDays,
	}
}

func (s *Stage) ToDB() *db.Stage {
	if s == nil {
		return nil
	}
	return &db.Stage{
		ID:           s.ID,
		Alias:        s.Alias,
		Order:        s.Order,
		Title:        s.Title,
		ShortTitle:   s.ShortTitle,
		Description:  s.Description,
		MaxScore:     s.MaxScore,
		DeadlineDays: s.DeadlineDays,
		StatusID:     db.StatusEnabled,
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
	Login          string   `json:"login"`
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
		Login:          d.Login,
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
// Password / AuthKey / LastActivityAt are credential-only fields and are
// never copied here: they're managed exclusively by AuthService.
func (c *Candidate) ToDB() *db.Candidate {
	if c == nil {
		return nil
	}
	return &db.Candidate{
		ID:             c.ID,
		Name:           c.Name,
		Handle:         c.Handle,
		Login:          c.Login,
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
	StageID          int     `json:"stageId"`
	Stage            *Stage  `json:"stage"`
	Status           string  `json:"status"`
	Score            *int    `json:"score"`
	MaxScore         int     `json:"maxScore"`
	ScoredAt         *string `json:"scoredAt"`
	CandidateStageID *int    `json:"candidateStageId"`
	Link             *string `json:"link"`
	Deadline         *string `json:"deadline"`
	CreatedAt        *string `json:"createdAt"`
	IsReady          bool    `json:"isReady"`
	SetReadyAt       *string `json:"setReadyAt"`
	Retries          int     `json:"retries"`
}

const (
	StageStatusDone    = "done"
	StageStatusCurrent = "current"
	StageStatusTodo    = "todo"
)

// CandidateStageSummary — компактная сводка по текущей CandidateStage кандидата
// (link, createdAt, deadline). Заполняется в списочных ответах, чтобы UI не
// ходил отдельно за CandidateStage. nil, если у кандидата ещё нет ни одной
// CandidateStage-строки. ID/StageID намеренно не отдаём: id внутренний,
// stageId дублирует CandidateSummary.CurrentStage.id.
type CandidateStageSummary struct {
	Link       *string `json:"link"`
	Deadline   *string `json:"deadline"`
	CreatedAt  string  `json:"createdAt"`
	IsReady    bool    `json:"isReady"`
	SetReadyAt *string `json:"setReadyAt"`
	Retries    int     `json:"retries"`
}

// CandidateSummary — кандидат с агрегатами для списка/канбана.
type CandidateSummary struct {
	ID                    int                    `json:"id"`
	Name                  string                 `json:"name"`
	Handle                string                 `json:"handle"`
	City                  string                 `json:"city"`
	Age                   *int                   `json:"age"`
	AvatarColor           string                 `json:"avatarColor"`
	Initials              string                 `json:"initials"`
	AvatarURL             *string                `json:"avatarUrl"`
	Strengths             []string               `json:"strengths"`
	Weaknesses            []string               `json:"weaknesses"`
	CurrentStage          *Stage                 `json:"currentStage"`
	CurrentCandidateStage *CandidateStageSummary `json:"currentCandidateStage"`
	TotalPoints           int                    `json:"totalPoints"`
	MaxPoints             int                    `json:"maxPoints"`
	CompletedStages       int                    `json:"completedStages"`
	StageCount            int                    `json:"stageCount"`
	CompletedAt           *string                `json:"completedAt"`
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

// CandidateStage — прохождение кандидатом этапа.
//
// Score / ScoredAt — null до момента оценки (Advance / Rate).
// Link — null пока не прикреплён.
// Deadline — null если у этапа deadlineDays = 0.
type CandidateStage struct {
	ID          int     `json:"id"`
	CandidateID int     `json:"candidateId"`
	StageID     int     `json:"stageId"`
	Link        *string `json:"link"`
	Score       *int    `json:"score"`
	ScoredAt    *string `json:"scoredAt"`
	Deadline    *string `json:"deadline"`
	CreatedAt   string  `json:"createdAt"`
	IsReady     bool    `json:"isReady"`
	SetReadyAt  *string `json:"setReadyAt"`
	Retries     int     `json:"retries"`
}

func NewCandidateStage(d *db.CandidateStage) *CandidateStage {
	if d == nil {
		return nil
	}
	return &CandidateStage{
		ID:          d.ID,
		CandidateID: d.CandidateID,
		StageID:     d.StageID,
		Link:        d.Link,
		Score:       d.Score,
		ScoredAt:    formatTimePtr(d.ScoredAt),
		Deadline:    formatTimePtr(d.Deadline),
		CreatedAt:   formatTime(d.CreatedAt),
		IsReady:     d.IsReady,
		SetReadyAt:  formatTimePtr(d.SetReadyAt),
		Retries:     d.Retries,
	}
}

// AdvanceResult — результат перевода кандидата на следующий этап.
type AdvanceResult struct {
	Candidate      *Candidate      `json:"candidate"`
	CandidateStage *CandidateStage `json:"candidateStage"`
}

// CandidateWithPassword carries the freshly generated password back to the
// caller of CandidateService.Add — the only path where the password is
// surfaced. Subsequent reads (Get/GetByID/Update responses) never include it.
type CandidateWithPassword struct {
	Candidate
	Password string `json:"password"`
}

// SignUpParams is the public-signup payload: credentials plus the basic
// profile fields a candidate fills on the registration form. Fields gated by
// program flow (currentStageId, completedAt) are intentionally absent —
// the first stage is assigned automatically, completion is set by Advance.
//
// Optional fields use pointers so the caller can omit them: Handle defaults
// to Login, Initials defaults to defaultInitials(Login), AvatarUrl is nullable,
// Strengths/Weaknesses default to empty slices.
type SignUpParams struct {
	Login       string   `json:"login"`
	Password    string   `json:"password"`
	Name        string   `json:"name"`
	Handle      *string  `json:"handle"`
	City        string   `json:"city"`
	Age         *int     `json:"age"`
	Bio         string   `json:"bio"`
	AvatarColor string   `json:"avatarColor"`
	Initials    *string  `json:"initials"`
	AvatarURL   *string  `json:"avatarUrl"`
	Strengths   []string `json:"strengths"`
	Weaknesses  []string `json:"weaknesses"`
}

// CandidateProfile is the editable subset of Candidate exposed via
// candidate.updateProfile. Excluded by design: password (separate flow),
// authKey (managed by auth.*), currentStageId / completedAt (program flow),
// createdAt / updatedAt (server-managed), statusId (admin lifecycle).
//
// Login is editable here. Changing it does not invalidate existing authKey —
// the token is independent of login.
type CandidateProfile struct {
	Login       string   `json:"login"`
	Name        string   `json:"name"`
	Handle      string   `json:"handle"`
	City        string   `json:"city"`
	Age         *int     `json:"age"`
	Bio         string   `json:"bio"`
	AvatarColor string   `json:"avatarColor"`
	Initials    string   `json:"initials"`
	AvatarURL   *string  `json:"avatarUrl"`
	Strengths   []string `json:"strengths"`
	Weaknesses  []string `json:"weaknesses"`
}

// ============================================================================
// Auth
// ============================================================================

// Me — публичные сведения о текущем принципале для AuthService.Me.
// UserType — один из MeTypeAdmin / MeTypeCandidate.
type Me struct {
	UserID   int    `json:"userId"`
	Login    string `json:"login"`
	UserType string `json:"userType"`
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

// ============================================================================
// Material
// ============================================================================

// Material — теоретический материал из общего каталога.
type Material struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	URL         string `json:"url"`
	Description string `json:"description"`
	MaxScore    int    `json:"maxScore"`
	Order       int    `json:"order"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

func NewMaterial(d *db.Material) *Material {
	if d == nil {
		return nil
	}
	return &Material{
		ID:          d.ID,
		Title:       d.Title,
		Type:        d.Type,
		URL:         d.Url,
		Description: d.Description,
		MaxScore:    d.MaxScore,
		Order:       d.Order,
		CreatedAt:   formatTime(d.CreatedAt),
		UpdatedAt:   formatTime(d.UpdatedAt),
	}
}

// MaterialInput — поля, которые админ передаёт при Add/Update.
// MaxScore=nil → дефолт 10 (на Add) или сохранение прежнего значения (на Update).
// Order=nil → MAX(order)+1 среди активных материалов на Add, либо сохранение
// прежнего значения на Update.
type MaterialInput struct {
	Title       string `json:"title"`
	Type        string `json:"type"`
	URL         string `json:"url"`
	Description string `json:"description"`
	MaxScore    *int   `json:"maxScore"`
	Order       *int   `json:"order"`
}

// CandidateMaterial — прогресс кандидата по конкретному материалу.
type CandidateMaterial struct {
	ID          int     `json:"id"`
	CandidateID int     `json:"candidateId"`
	MaterialID  int     `json:"materialId"`
	ReadAt      *string `json:"readAt"`
	Score       *int    `json:"score"`
	ScoredAt    *string `json:"scoredAt"`
	ScoredBy    *int    `json:"scoredBy"`
	Notes       *string `json:"notes"`
	CreatedAt   string  `json:"createdAt"`
}

func NewCandidateMaterial(d *db.CandidateMaterial) *CandidateMaterial {
	if d == nil {
		return nil
	}
	return &CandidateMaterial{
		ID:          d.ID,
		CandidateID: d.CandidateID,
		MaterialID:  d.MaterialID,
		ReadAt:      formatTimePtr(d.ReadAt),
		Score:       d.Score,
		ScoredAt:    formatTimePtr(d.ScoredAt),
		ScoredBy:    d.ScoredBy,
		Notes:       d.Notes,
		CreatedAt:   formatTime(d.CreatedAt),
	}
}

// CandidateBrief — компактный кандидат для матрицы прогресса. Без bio/login,
// чтобы не раздувать ответ getProgress.
type CandidateBrief struct {
	ID             int     `json:"id"`
	Name           string  `json:"name"`
	Handle         string  `json:"handle"`
	AvatarColor    string  `json:"avatarColor"`
	Initials       string  `json:"initials"`
	AvatarURL      *string `json:"avatarUrl"`
	CurrentStageID int     `json:"currentStageId"`
}

func NewCandidateBrief(d *db.Candidate) *CandidateBrief {
	if d == nil {
		return nil
	}
	return &CandidateBrief{
		ID:             d.ID,
		Name:           d.Name,
		Handle:         d.Handle,
		AvatarColor:    d.AvatarColor,
		Initials:       d.Initials,
		AvatarURL:      d.AvatarUrl,
		CurrentStageID: d.CurrentStageID,
	}
}

// MaterialsProgress — публичная «доска прогресса». Клиент строит матрицу по
// (candidateId, materialId). Записи в Progress содержат только реально
// существующие в БД отметки; ячейки без записи трактуются как «не отмечен».
type MaterialsProgress struct {
	Materials  []Material          `json:"materials"`
	Candidates []CandidateBrief    `json:"candidates"`
	Progress   []CandidateMaterial `json:"progress"`
}

// MyMaterialProgress — материал плюс прогресс текущего кандидата по нему.
// Progress=nil означает, что кандидат ещё не отмечал.
type MyMaterialProgress struct {
	Material Material           `json:"material"`
	Progress *CandidateMaterial `json:"progress"`
}
