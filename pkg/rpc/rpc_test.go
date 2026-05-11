package rpc

import (
	"context"
	"fmt"
	"testing"

	"apisrv/pkg/db"
	"apisrv/pkg/db/test"

	. "github.com/smartystreets/goconvey/convey"
)

// rpcFixtures wires every service against a fresh DB and resets it between
// `Convey` outer groups.
type rpcFixtures struct {
	dbo       db.DB
	repo      db.ApprenticeRepo
	candidate *CandidateService
	stage     *StageService
	dashboard *DashboardService
	auth      *AuthService
	material  *MaterialService
}

func newRPCFixtures(t *testing.T) rpcFixtures {
	dbo, logger := test.Setup(t)
	resetApprenticeDB(t, dbo)
	return rpcFixtures{
		dbo:       dbo,
		repo:      db.NewApprenticeRepo(dbo),
		candidate: NewCandidateService(dbo, logger),
		stage:     NewStageService(dbo, logger),
		dashboard: NewDashboardService(dbo, logger),
		auth:      NewAuthService(dbo, logger),
		material:  NewMaterialService(dbo, logger),
	}
}

// resetApprenticeDB wipes apprentice tables and reseeds two stages so each
// test gets a small, fully-controlled topology. Also clears non-admin users so
// AuthService Register tests start from a known state.
func resetApprenticeDB(t *testing.T, dbo db.DB) {
	t.Helper()
	ctx := t.Context()
	stmts := []string{
		`TRUNCATE TABLE "candidateStages" RESTART IDENTITY CASCADE`,
		`TRUNCATE TABLE "candidateMaterials" RESTART IDENTITY CASCADE`,
		`DELETE FROM "candidates"`,
		`ALTER SEQUENCE "candidates_candidateId_seq" RESTART WITH 1`,
		`DELETE FROM "stages"`,
		`ALTER SEQUENCE "stages_stageId_seq" RESTART WITH 1`,
		`DELETE FROM "materials"`,
		`ALTER SEQUENCE "materials_materialId_seq" RESTART WITH 1`,
		`DELETE FROM "users" WHERE login <> 'admin'`,
	}
	for _, s := range stmts {
		if _, err := dbo.ExecContext(ctx, s); err != nil {
			t.Fatalf("reset: %s: %v", s, err)
		}
	}
}

// makeStages creates `n` enabled stages with order=1..n and maxScore=10.
func makeStages(t *testing.T, ctx context.Context, srv *StageService, n int) []Stage {
	t.Helper()
	out := make([]Stage, 0, n)
	for i := 1; i <= n; i++ {
		s := Stage{
			Alias:      fmt.Sprintf("s%d", i),
			Order:      i,
			Title:      fmt.Sprintf("stage %d", i),
			ShortTitle: fmt.Sprintf("S%d", i),
			MaxScore:   10,
		}
		created, err := srv.Add(ctx, s)
		if err != nil {
			t.Fatalf("seed stage %d: %v", i, err)
		}
		out = append(out, *created)
	}
	return out
}

// testAdminPassword is the plaintext password seedAdmin assigns to every
// fixture admin. Tests that need to exercise login pass this value.
const testAdminPassword = "passw0rd!"

// seedAdmin inserts an enabled admin User directly via the repo and logs them
// in to obtain an authKey usable by the middleware. Returns the authKey for
// test cases that hit protected RPC methods.
func seedAdmin(t *testing.T, ctx context.Context, dbo db.DB, login string) string {
	t.Helper()
	hash, err := passwordHash(testAdminPassword)
	if err != nil {
		t.Fatalf("hash admin pwd: %v", err)
	}
	return seedAdminWithRawHash(t, ctx, dbo, login, hash)
}

// seedAdminWithRawHash inserts an admin with a precomputed bcrypt hash —
// useful when a test needs to login with a password that doesn't satisfy the
// current Register policy (e.g. the seed password "12345" from init.sql).
func seedAdminWithRawHash(t *testing.T, ctx context.Context, dbo db.DB, login, bcryptHash string) string {
	t.Helper()
	commonRepo := db.NewCommonRepo(dbo)
	u, err := commonRepo.AddUser(ctx, &db.User{
		Login:    login,
		Password: bcryptHash,
		StatusID: db.StatusEnabled,
	})
	if err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	authKey := generateAuthKey()
	if _, err := commonRepo.AuthenticateUser(ctx, u, authKey); err != nil {
		t.Fatalf("authenticate admin: %v", err)
	}
	return authKey
}

func makeCandidate(t *testing.T, ctx context.Context, srv *CandidateService, handle string, stageID int) *Candidate {
	t.Helper()
	c := Candidate{
		Name:           "Иван " + handle,
		Handle:         handle,
		Login:          handle,
		Initials:       "ИИ",
		AvatarColor:    "#fff",
		CurrentStageID: stageID,
	}
	created, err := srv.Add(ctx, c)
	if err != nil {
		t.Fatalf("seed candidate %s: %v", handle, err)
	}
	return &created.Candidate
}

// =============================================================================
// StageService
// =============================================================================

func TestDB_StageService_CRUD(t *testing.T) {
	Convey("StageService CRUD + reorder", t, func() {
		f := newRPCFixtures(t)
		ctx := t.Context()

		Convey("Add: happy path", func() {
			stages := makeStages(t, ctx, f.stage, 3)
			So(stages, ShouldHaveLength, 3)

			list, err := f.stage.Get(ctx)
			So(err, ShouldBeNil)
			So(list, ShouldHaveLength, 3)
			So(list[0].Order, ShouldEqual, 1)
			So(list[2].Order, ShouldEqual, 3)
		})

		Convey("Add: rejects duplicate alias", func() {
			_ = makeStages(t, ctx, f.stage, 1)
			_, err := f.stage.Add(ctx, Stage{
				Alias: "s1", Order: 99, Title: "x", ShortTitle: "x", MaxScore: 10,
			})
			So(err, ShouldNotBeNil)
		})

		Convey("Add: rejects duplicate order", func() {
			_ = makeStages(t, ctx, f.stage, 1)
			_, err := f.stage.Add(ctx, Stage{
				Alias: "other", Order: 1, Title: "x", ShortTitle: "x", MaxScore: 10,
			})
			So(err, ShouldNotBeNil)
		})

		Convey("Add: validation errors surface for bad fields", func() {
			_, err := f.stage.Add(ctx, Stage{Alias: "BAD!!", Title: "", ShortTitle: "", MaxScore: 0, Order: 0})
			So(err, ShouldNotBeNil)
		})

		Convey("Update: changes basic fields", func() {
			stages := makeStages(t, ctx, f.stage, 1)
			st := stages[0]
			st.Title = "renamed"
			ok, err := f.stage.Update(ctx, st)
			So(err, ShouldBeNil)
			So(ok, ShouldBeTrue)

			fresh, err := f.stage.GetByID(ctx, st.ID)
			So(err, ShouldBeNil)
			So(fresh.Title, ShouldEqual, "renamed")
		})

		Convey("Update: 404 for unknown id", func() {
			_, err := f.stage.Update(ctx, Stage{ID: 999, Alias: "xy", Order: 1, Title: "t", ShortTitle: "s", MaxScore: 10})
			So(err, ShouldEqual, ErrStageNotFound)
		})

		Convey("Delete: refuses if scores exist", func() {
			stages := makeStages(t, ctx, f.stage, 2)
			c := makeCandidate(t, ctx, f.candidate, "h.one", stages[0].ID)
			_, err := f.candidate.Advance(ctx, c.ID, 7, nil)
			So(err, ShouldBeNil)

			_, err = f.stage.Delete(ctx, stages[0].ID)
			So(err, ShouldEqual, ErrStageHasScores)
		})

		Convey("Delete: soft-deletes when safe and frees alias/order", func() {
			stages := makeStages(t, ctx, f.stage, 1)
			ok, err := f.stage.Delete(ctx, stages[0].ID)
			So(err, ShouldBeNil)
			So(ok, ShouldBeTrue)

			// alias re-usable after delete (partial unique).
			_, err = f.stage.Add(ctx, Stage{Alias: "s1", Order: 1, Title: "again", ShortTitle: "S1", MaxScore: 10})
			So(err, ShouldBeNil)
		})

		Convey("Reorder: full permutation succeeds inside one tx", func() {
			stages := makeStages(t, ctx, f.stage, 3)
			ids := []int{stages[2].ID, stages[0].ID, stages[1].ID}

			out, err := f.stage.Reorder(ctx, ids)
			So(err, ShouldBeNil)
			So(out, ShouldHaveLength, 3)
			So(out[0].ID, ShouldEqual, stages[2].ID)
			So(out[0].Order, ShouldEqual, 1)
			So(out[1].Order, ShouldEqual, 2)
			So(out[2].Order, ShouldEqual, 3)

			Convey("and no row carries a non-positive order afterwards", func() {
				list, err := f.stage.Get(ctx)
				So(err, ShouldBeNil)
				for _, s := range list {
					So(s.Order, ShouldBeGreaterThanOrEqualTo, 1)
				}
			})
		})

		Convey("Reorder: rejects mismatched length", func() {
			_ = makeStages(t, ctx, f.stage, 3)
			_, err := f.stage.Reorder(ctx, []int{1, 2})
			So(err, ShouldEqual, ErrReorderInvalid)
		})

		Convey("Reorder: rejects unknown id", func() {
			stages := makeStages(t, ctx, f.stage, 2)
			_, err := f.stage.Reorder(ctx, []int{stages[0].ID, 999})
			So(err, ShouldEqual, ErrReorderInvalid)
		})

		Convey("Reorder: rejects duplicate id", func() {
			stages := makeStages(t, ctx, f.stage, 2)
			_, err := f.stage.Reorder(ctx, []int{stages[0].ID, stages[0].ID})
			So(err, ShouldEqual, ErrReorderInvalid)
		})
	})
}

// =============================================================================
// CandidateService — list, CRUD, soft-delete handle reuse
// =============================================================================

func TestDB_CandidateService_CRUD(t *testing.T) {
	Convey("CandidateService CRUD", t, func() {
		f := newRPCFixtures(t)
		ctx := t.Context()
		stages := makeStages(t, ctx, f.stage, 3)

		Convey("Add: creates with FullCandidate response", func() {
			c := makeCandidate(t, ctx, f.candidate, "ivan.s", stages[0].ID)
			So(c, ShouldNotBeNil)
			So(c.ID, ShouldBeGreaterThan, 0)
			So(c.CurrentStageID, ShouldEqual, stages[0].ID)
			So(c.Strengths, ShouldNotBeNil)
			So(c.Weaknesses, ShouldNotBeNil)
		})

		Convey("Add: nil Strengths/Weaknesses become empty arrays", func() {
			c := makeCandidate(t, ctx, f.candidate, "ivan.s", stages[0].ID)
			So(c.Strengths, ShouldHaveLength, 0)
			So(c.Weaknesses, ShouldHaveLength, 0)
		})

		Convey("Add: rejects bad handle", func() {
			_, err := f.candidate.Add(ctx, Candidate{
				Name: "X", Handle: "BAD HANDLE!!", Initials: "XX", CurrentStageID: stages[0].ID,
			})
			So(err, ShouldNotBeNil)
		})

		Convey("Add: rejects unknown stage", func() {
			_, err := f.candidate.Add(ctx, Candidate{
				Name: "X", Handle: "x.x", Login: "x.x", Initials: "XX", CurrentStageID: 999,
			})
			So(err, ShouldEqual, ErrInvalidCurrentStage)
		})

		Convey("Add: handle uniqueness", func() {
			_ = makeCandidate(t, ctx, f.candidate, "ivan.s", stages[0].ID)
			_, err := f.candidate.Add(ctx, Candidate{
				Name: "X", Handle: "ivan.s", Login: "ivan.s", Initials: "XX", CurrentStageID: stages[0].ID,
			})
			So(err, ShouldNotBeNil)
		})

		Convey("Update: changes name and handle", func() {
			c := makeCandidate(t, ctx, f.candidate, "ivan.s", stages[0].ID)
			c.Name = "Ivan Renamed"
			c.Handle = "ivan.r"
			ok, err := f.candidate.Update(ctx, *c)
			So(err, ShouldBeNil)
			So(ok, ShouldBeTrue)

			detail, err := f.candidate.GetByID(ctx, c.ID)
			So(err, ShouldBeNil)
			So(detail.Name, ShouldEqual, "Ivan Renamed")
			So(detail.Handle, ShouldEqual, "ivan.r")
		})

		Convey("Update: 404 for unknown id", func() {
			_, err := f.candidate.Update(ctx, Candidate{
				ID: 999, Name: "X", Handle: "x.x", Initials: "XX", CurrentStageID: stages[0].ID,
			})
			So(err, ShouldEqual, ErrCandidateNotFound)
		})

		Convey("Delete: soft-deletes and frees handle for reuse", func() {
			c := makeCandidate(t, ctx, f.candidate, "ivan.s", stages[0].ID)
			ok, err := f.candidate.Delete(ctx, c.ID)
			So(err, ShouldBeNil)
			So(ok, ShouldBeTrue)

			// Now the same handle should be re-usable (partial unique honours soft-delete).
			c2, err := f.candidate.Add(ctx, Candidate{
				Name: "Ivan B", Handle: "ivan.s", Login: "ivan.s", Initials: "ИБ", CurrentStageID: stages[0].ID,
			})
			So(err, ShouldBeNil)
			So(c2.ID, ShouldNotEqual, c.ID)
		})

		Convey("GetByID: 404 on missing", func() {
			_, err := f.candidate.GetByID(ctx, 999)
			So(err, ShouldEqual, ErrCandidateNotFound)
		})
	})
}

// =============================================================================
// CandidateService — Advance / Rate / Rollback
// =============================================================================

func TestDB_CandidateService_Advance(t *testing.T) {
	Convey("Advance / Rate / Rollback", t, func() {
		f := newRPCFixtures(t)
		ctx := t.Context()
		stages := makeStages(t, ctx, f.stage, 3)
		c := makeCandidate(t, ctx, f.candidate, "ivan.s", stages[0].ID)

		Convey("Advance: scores current stage and moves to next", func() {
			res, err := f.candidate.Advance(ctx, c.ID, 7, nil)
			So(err, ShouldBeNil)
			So(res, ShouldNotBeNil)
			So(res.CandidateStage.Score, ShouldNotBeNil)
			So(*res.CandidateStage.Score, ShouldEqual, 7)
			So(res.CandidateStage.StageID, ShouldEqual, stages[0].ID)
			So(res.Candidate.CurrentStageID, ShouldEqual, stages[1].ID)
			So(res.Candidate.CompletedAt, ShouldBeNil)
		})

		Convey("Advance: last stage sets completedAt and keeps current pointer", func() {
			_, err := f.candidate.Advance(ctx, c.ID, 5, nil)
			So(err, ShouldBeNil)
			_, err = f.candidate.Advance(ctx, c.ID, 6, nil)
			So(err, ShouldBeNil)
			res, err := f.candidate.Advance(ctx, c.ID, 7, nil)
			So(err, ShouldBeNil)
			So(res.Candidate.CompletedAt, ShouldNotBeNil)
			So(res.Candidate.CurrentStageID, ShouldEqual, stages[2].ID)
		})

		Convey("Advance: blocks duplicate score for same stage", func() {
			_, err := f.candidate.Advance(ctx, c.ID, 5, nil)
			So(err, ShouldBeNil)

			// Force the candidate back to stage 1 (which already has a scored
			// CandidateStage row) to trigger the "already scored" guard without
			// going through Rollback.
			cur, _ := f.repo.CandidateByID(ctx, c.ID)
			cur.CurrentStageID = stages[0].ID
			_, _ = f.repo.UpdateCandidate(ctx, cur, db.WithColumns(db.Columns.Candidate.CurrentStageID))

			_, err = f.candidate.Advance(ctx, c.ID, 5, nil)
			So(err, ShouldEqual, ErrAlreadyScored)
		})

		Convey("Advance: blocks completed candidate", func() {
			_, _ = f.candidate.Advance(ctx, c.ID, 5, nil)
			_, _ = f.candidate.Advance(ctx, c.ID, 5, nil)
			_, _ = f.candidate.Advance(ctx, c.ID, 5, nil)
			_, err := f.candidate.Advance(ctx, c.ID, 5, nil)
			So(err, ShouldEqual, ErrAlreadyCompleted)
		})

		Convey("Advance: enforces score range against stage maxScore", func() {
			_, err := f.candidate.Advance(ctx, c.ID, 0, nil)
			So(err, ShouldEqual, ErrScoreOutOfRange)
			_, err = f.candidate.Advance(ctx, c.ID, 999, nil)
			So(err, ShouldEqual, ErrScoreOutOfRange)
		})

		Convey("Advance: 404 on unknown candidate", func() {
			_, err := f.candidate.Advance(ctx, 999, 5, nil)
			So(err, ShouldEqual, ErrCandidateNotFound)
		})

		Convey("Rate: corrects an existing score", func() {
			res, _ := f.candidate.Advance(ctx, c.ID, 5, nil)
			updated, err := f.candidate.Rate(ctx, res.CandidateStage.ID, 9, nil)
			So(err, ShouldBeNil)
			So(updated.Score, ShouldNotBeNil)
			So(*updated.Score, ShouldEqual, 9)
			So(updated.StageID, ShouldEqual, res.CandidateStage.StageID)
		})

		Convey("Rate: rejects out-of-range", func() {
			res, _ := f.candidate.Advance(ctx, c.ID, 5, nil)
			_, err := f.candidate.Rate(ctx, res.CandidateStage.ID, 999, nil)
			So(err, ShouldEqual, ErrScoreOutOfRange)
		})

		Convey("Rate: 404 for unknown candidateStageId", func() {
			_, err := f.candidate.Rate(ctx, 999, 5, nil)
			So(err, ShouldEqual, ErrCandidateStageNotFound)
		})

		Convey("Advance: persists notes and returns them in response", func() {
			notes := "  good work, ship it  "
			res, err := f.candidate.Advance(ctx, c.ID, 7, &notes)
			So(err, ShouldBeNil)
			So(res.CandidateStage.Notes, ShouldNotBeNil)
			So(*res.CandidateStage.Notes, ShouldEqual, "good work, ship it")
		})

		Convey("Advance: whitespace-only notes treated as no notes", func() {
			ws := "   \t  "
			res, err := f.candidate.Advance(ctx, c.ID, 7, &ws)
			So(err, ShouldBeNil)
			So(res.CandidateStage.Notes, ShouldBeNil)
		})

		Convey("Rate: COALESCE — nil notes preserves prior comment", func() {
			first := "first round comment"
			res, err := f.candidate.Advance(ctx, c.ID, 5, &first)
			So(err, ShouldBeNil)
			updated, err := f.candidate.Rate(ctx, res.CandidateStage.ID, 9, nil)
			So(err, ShouldBeNil)
			So(updated.Notes, ShouldNotBeNil)
			So(*updated.Notes, ShouldEqual, "first round comment")
		})

		Convey("Rate: non-empty notes overwrites prior value", func() {
			first := "first"
			res, err := f.candidate.Advance(ctx, c.ID, 5, &first)
			So(err, ShouldBeNil)
			second := "second"
			updated, err := f.candidate.Rate(ctx, res.CandidateStage.ID, 9, &second)
			So(err, ShouldBeNil)
			So(updated.Notes, ShouldNotBeNil)
			So(*updated.Notes, ShouldEqual, "second")
		})

		Convey("Rollback: deletes latest score and moves back", func() {
			_, _ = f.candidate.Advance(ctx, c.ID, 5, nil)
			res2, _ := f.candidate.Advance(ctx, c.ID, 6, nil)
			So(res2.Candidate.CurrentStageID, ShouldEqual, stages[2].ID)

			out, err := f.candidate.Rollback(ctx, c.ID)
			So(err, ShouldBeNil)
			So(out.CurrentStageID, ShouldEqual, stages[1].ID)
			So(out.CompletedAt, ShouldBeNil)
		})

		Convey("Rollback: clears completedAt when rolling back from finished", func() {
			_, _ = f.candidate.Advance(ctx, c.ID, 5, nil)
			_, _ = f.candidate.Advance(ctx, c.ID, 6, nil)
			_, _ = f.candidate.Advance(ctx, c.ID, 7, nil)

			out, err := f.candidate.Rollback(ctx, c.ID)
			So(err, ShouldBeNil)
			So(out.CompletedAt, ShouldBeNil)
			So(out.CurrentStageID, ShouldEqual, stages[2].ID)
		})

		Convey("Rollback: refuses when no scores", func() {
			_, err := f.candidate.Rollback(ctx, c.ID)
			So(err, ShouldEqual, ErrCannotRollback)
		})

		Convey("Rollback: 404 on unknown candidate", func() {
			_, err := f.candidate.Rollback(ctx, 999)
			So(err, ShouldEqual, ErrCandidateNotFound)
		})
	})
}

// =============================================================================
// CandidateService — list aggregations, sorting, kanban, history
// =============================================================================

func TestDB_CandidateService_Aggregates(t *testing.T) {
	Convey("Get / Kanban / History aggregation", t, func() {
		f := newRPCFixtures(t)
		ctx := t.Context()
		stages := makeStages(t, ctx, f.stage, 3)
		alpha := makeCandidate(t, ctx, f.candidate, "alpha", stages[0].ID)
		beta := makeCandidate(t, ctx, f.candidate, "beta", stages[0].ID)
		gamma := makeCandidate(t, ctx, f.candidate, "gamma", stages[0].ID)
		_, _ = f.candidate.Advance(ctx, beta.ID, 8, nil)  // beta now at stage 2, totalPoints=8
		_, _ = f.candidate.Advance(ctx, gamma.ID, 5, nil) // gamma at 2, totalPoints=5
		_, _ = f.candidate.Advance(ctx, gamma.ID, 9, nil) // gamma at 3, totalPoints=14

		Convey("sort by points: descending totalPoints", func() {
			list, err := f.candidate.Get(ctx, CandidateSortPoints)
			So(err, ShouldBeNil)
			So(list, ShouldHaveLength, 3)
			So(list[0].Handle, ShouldEqual, "gamma")
			So(list[0].TotalPoints, ShouldEqual, 14)
			So(list[1].Handle, ShouldEqual, "beta")
			So(list[2].TotalPoints, ShouldEqual, 0)
		})

		Convey("sort by stage: descending current stage order", func() {
			list, err := f.candidate.Get(ctx, CandidateSortStage)
			So(err, ShouldBeNil)
			So(list[0].Handle, ShouldEqual, "gamma")
			So(list[2].Handle, ShouldEqual, "alpha")
		})

		Convey("sort by name: locale-aware Russian collation", func() {
			// rename to Russian names with accents/case differences.
			alpha.Name = "Анна"
			_, _ = f.candidate.Update(ctx, *alpha)
			beta.Name = "БОРИС"
			_, _ = f.candidate.Update(ctx, *beta)
			gamma.Name = "ёлка" // ё after е, before ж
			_, _ = f.candidate.Update(ctx, *gamma)

			list, err := f.candidate.Get(ctx, CandidateSortName)
			So(err, ShouldBeNil)
			So(list[0].Name, ShouldEqual, "Анна")
			So(list[1].Name, ShouldEqual, "БОРИС")
			So(list[2].Name, ShouldEqual, "ёлка")
		})

		Convey("aggregate counters reflect score sum and stage count", func() {
			list, _ := f.candidate.Get(ctx, CandidateSortPoints)
			for _, c := range list {
				So(c.MaxPoints, ShouldEqual, 30) // 3 stages × 10
				So(c.StageCount, ShouldEqual, 3)
			}
		})

		Convey("Kanban: groups candidates per stage", func() {
			cols, err := f.candidate.Kanban(ctx)
			So(err, ShouldBeNil)
			So(cols, ShouldHaveLength, 3)

			byStage := make(map[int]int)
			for _, col := range cols {
				byStage[col.Stage.ID] = len(col.Candidates)
			}
			So(byStage[stages[0].ID], ShouldEqual, 1) // alpha only
			So(byStage[stages[1].ID], ShouldEqual, 1) // beta
			So(byStage[stages[2].ID], ShouldEqual, 1) // gamma
		})

		Convey("GetByID: history exposes done/current/todo for each stage", func() {
			detail, err := f.candidate.GetByID(ctx, gamma.ID)
			So(err, ShouldBeNil)
			So(detail.History, ShouldHaveLength, 3)
			So(detail.History[0].Status, ShouldEqual, StageStatusDone)
			So(detail.History[1].Status, ShouldEqual, StageStatusDone)
			So(detail.History[2].Status, ShouldEqual, StageStatusCurrent)
			So(*detail.History[0].Score, ShouldEqual, 5)
			So(*detail.History[1].Score, ShouldEqual, 9)
		})

		Convey("GetByID: history and currentCandidateStage carry notes when set", func() {
			// Attach a note to a previously-scored stage[0] row via Rate, and
			// to gamma's current empty row via repo write (no RPC for setNotes).
			doneCS := loadCurrentCandidateStage(t, ctx, f.repo, gamma.ID, stages[0].ID)
			doneNotes := "stage 0 retro note"
			_, err := f.candidate.Rate(ctx, doneCS.ID, *doneCS.Score, &doneNotes)
			So(err, ShouldBeNil)

			curCS := loadCurrentCandidateStage(t, ctx, f.repo, gamma.ID, stages[2].ID)
			curNotes := "current stage live note"
			curCS.Notes = &curNotes
			_, err = f.repo.UpdateCandidateStage(ctx, curCS, db.WithColumns(db.Columns.CandidateStage.Notes))
			So(err, ShouldBeNil)

			detail, err := f.candidate.GetByID(ctx, gamma.ID)
			So(err, ShouldBeNil)
			So(detail.History[0].Notes, ShouldNotBeNil)
			So(*detail.History[0].Notes, ShouldEqual, doneNotes)
			So(detail.History[1].Notes, ShouldBeNil)
			So(detail.History[2].Notes, ShouldNotBeNil)
			So(*detail.History[2].Notes, ShouldEqual, curNotes)

			So(detail.CurrentCandidateStage, ShouldNotBeNil)
			So(detail.CurrentCandidateStage.Notes, ShouldNotBeNil)
			So(*detail.CurrentCandidateStage.Notes, ShouldEqual, curNotes)
		})

		Convey("currentCandidateStage in summary tracks current stage's CandidateStage row", func() {
			// alpha: untouched, sits on stage[0]; gamma: advanced twice, sits on stage[2].
			alphaCS := loadCurrentCandidateStage(t, ctx, f.repo, alpha.ID, stages[0].ID)
			gammaCS := loadCurrentCandidateStage(t, ctx, f.repo, gamma.ID, stages[2].ID)

			Convey("Get exposes link/deadline/createdAt of the current row", func() {
				list, err := f.candidate.Get(ctx, CandidateSortPoints)
				So(err, ShouldBeNil)
				byHandle := indexByHandle(list)

				So(byHandle["alpha"].CurrentCandidateStage, ShouldNotBeNil)
				So(byHandle["alpha"].CurrentCandidateStage.Link, ShouldBeNil)
				So(byHandle["alpha"].CurrentCandidateStage.CreatedAt, ShouldEqual, formatTime(alphaCS.CreatedAt))

				So(byHandle["gamma"].CurrentCandidateStage, ShouldNotBeNil)
				So(byHandle["gamma"].CurrentCandidateStage.CreatedAt, ShouldEqual, formatTime(gammaCS.CreatedAt))
			})

			Convey("Link set on the current row is reflected on next Get", func() {
				link := "https://example.com/alpha"
				alphaCS.Link = &link
				_, err := f.repo.UpdateCandidateStage(ctx, alphaCS, db.WithColumns(db.Columns.CandidateStage.Link))
				So(err, ShouldBeNil)

				list, _ := f.candidate.Get(ctx, CandidateSortPoints)
				byHandle := indexByHandle(list)
				So(byHandle["alpha"].CurrentCandidateStage.Link, ShouldNotBeNil)
				So(*byHandle["alpha"].CurrentCandidateStage.Link, ShouldEqual, link)
			})

			Convey("Advance moves currentCandidateStage to the new (empty) row", func() {
				_, err := f.candidate.Advance(ctx, alpha.ID, 6, nil)
				So(err, ShouldBeNil)
				newCS := loadCurrentCandidateStage(t, ctx, f.repo, alpha.ID, stages[1].ID)

				list, _ := f.candidate.Get(ctx, CandidateSortPoints)
				byHandle := indexByHandle(list)
				So(byHandle["alpha"].CurrentCandidateStage, ShouldNotBeNil)
				So(byHandle["alpha"].CurrentCandidateStage.Link, ShouldBeNil)
				So(byHandle["alpha"].CurrentCandidateStage.CreatedAt, ShouldEqual, formatTime(newCS.CreatedAt))
			})

			Convey("GetByID surfaces the same currentCandidateStage", func() {
				detail, err := f.candidate.GetByID(ctx, gamma.ID)
				So(err, ShouldBeNil)
				So(detail.CurrentCandidateStage, ShouldNotBeNil)
				So(detail.CurrentCandidateStage.CreatedAt, ShouldEqual, formatTime(gammaCS.CreatedAt))
			})
		})
	})
}

// loadCurrentCandidateStage reads the CandidateStage row for a (candidate, stage)
// pair directly via the repo so tests can assert on its on-disk timestamps and
// mutate it without going through SetLink (which needs a principal in ctx).
func loadCurrentCandidateStage(t *testing.T, ctx context.Context, repo db.ApprenticeRepo, candidateID, stageID int) *db.CandidateStage {
	t.Helper()
	cs, err := repo.OneCandidateStage(ctx, &db.CandidateStageSearch{
		CandidateID: &candidateID,
		StageID:     &stageID,
	})
	if err != nil {
		t.Fatalf("load CandidateStage(%d,%d): %v", candidateID, stageID, err)
	}
	if cs == nil {
		t.Fatalf("CandidateStage(%d,%d) not found", candidateID, stageID)
	}
	return cs
}

func indexByHandle(list []CandidateSummary) map[string]CandidateSummary {
	out := make(map[string]CandidateSummary, len(list))
	for _, c := range list {
		out[c.Handle] = c
	}
	return out
}

func TestDB_CandidateService_UpdateProfile(t *testing.T) {
	Convey("CandidateService.UpdateProfile (self-only)", t, func() {
		f := newRPCFixtures(t)
		ctx := t.Context()
		_ = makeStages(t, ctx, f.stage, 1)

		key, err := f.auth.SignUp(ctx, SignUpParams{
			Login: "polly.profile", Password: "passw0rd!", Name: "Polly", AvatarColor: "#000",
		})
		So(err, ShouldBeNil)

		dbCand, err := f.repo.EnabledCandidateByAuthKey(ctx, key)
		So(err, ShouldBeNil)
		candCtx := context.WithValue(ctx, candidateKey, dbCand)

		baseProfile := CandidateProfile{
			Login:       "polly.profile",
			Name:        "Polly Updated",
			Handle:      "polly.handle",
			City:        "СПб",
			Bio:         "fullstack",
			AvatarColor: "#abc",
			Initials:    "PU",
			Strengths:   []string{"go"},
			Weaknesses:  []string{"sql"},
		}

		Convey("self: updates own profile", func() {
			out, err := f.candidate.UpdateProfile(candCtx, baseProfile)
			So(err, ShouldBeNil)
			So(out, ShouldNotBeNil)
			So(out.ID, ShouldEqual, dbCand.ID)
			So(out.Name, ShouldEqual, "Polly Updated")
			So(out.Handle, ShouldEqual, "polly.handle")
			So(out.City, ShouldEqual, "СПб")
		})

		Convey("anonymous → ErrForbidden (no candidate in ctx)", func() {
			_, err := f.candidate.UpdateProfile(ctx, baseProfile)
			So(err, ShouldEqual, ErrForbidden)
		})

		Convey("admin context → ErrForbidden (admin uses candidate.update)", func() {
			adminUser := &db.User{ID: 1, Login: "admin", StatusID: db.StatusEnabled}
			adminCtx := context.WithValue(ctx, adminKey, adminUser)
			_, err := f.candidate.UpdateProfile(adminCtx, baseProfile)
			So(err, ShouldEqual, ErrForbidden)
		})

		Convey("login change: existing authKey survives", func() {
			renamed := baseProfile
			renamed.Login = "polly.renamed"
			_, err := f.candidate.UpdateProfile(candCtx, renamed)
			So(err, ShouldBeNil)
			cand, err := f.repo.EnabledCandidateByAuthKey(ctx, key)
			So(err, ShouldBeNil)
			So(cand, ShouldNotBeNil)
			So(cand.Login, ShouldEqual, "polly.renamed")
		})

		Convey("login taken by another candidate → ErrLoginTaken", func() {
			otherKey, err := f.auth.SignUp(ctx, SignUpParams{
				Login: "polly.other", Password: "passw0rd!", Name: "Other", AvatarColor: "#fff",
			})
			So(err, ShouldBeNil)
			So(otherKey, ShouldNotBeBlank)

			renamed := baseProfile
			renamed.Login = "polly.other"
			_, err = f.candidate.UpdateProfile(candCtx, renamed)
			So(err, ShouldEqual, ErrLoginTaken)
		})

		Convey("handle taken by another candidate → ErrHandleTaken", func() {
			handle := "shared.collide"
			_, err := f.auth.SignUp(ctx, SignUpParams{
				Login: "polly.h2", Password: "passw0rd!", Name: "Other", Handle: &handle, AvatarColor: "#fff",
			})
			So(err, ShouldBeNil)

			collide := baseProfile
			collide.Handle = handle
			_, err = f.candidate.UpdateProfile(candCtx, collide)
			So(err, ShouldEqual, ErrHandleTaken)
		})

		Convey("empty Name → ValidationError", func() {
			bad := baseProfile
			bad.Name = ""
			_, err := f.candidate.UpdateProfile(candCtx, bad)
			So(err, ShouldNotBeNil)
		})
	})
}

// =============================================================================
// DashboardService
// =============================================================================

func TestDB_DashboardService_Summary(t *testing.T) {
	Convey("Summary aggregates", t, func() {
		f := newRPCFixtures(t)
		ctx := t.Context()

		Convey("Empty database: zero counters, zero average", func() {
			out, err := f.dashboard.Summary(ctx)
			So(err, ShouldBeNil)
			So(out.CandidatesCount, ShouldEqual, 0)
			So(out.StagesCount, ShouldEqual, 0)
			So(out.TotalPoints, ShouldEqual, 0)
			So(out.MaxPoints, ShouldEqual, 0)
			So(out.AverageOrder, ShouldEqual, 0)
		})

		Convey("With seeded data", func() {
			stages := makeStages(t, ctx, f.stage, 3)
			alpha := makeCandidate(t, ctx, f.candidate, "alpha", stages[0].ID)
			beta := makeCandidate(t, ctx, f.candidate, "beta", stages[0].ID)
			_, _ = alpha, beta
			_, _ = f.candidate.Advance(ctx, beta.ID, 8, nil) // beta now at stage 2

			out, err := f.dashboard.Summary(ctx)
			So(err, ShouldBeNil)
			So(out.CandidatesCount, ShouldEqual, 2)
			So(out.StagesCount, ShouldEqual, 3)
			So(out.TotalPoints, ShouldEqual, 8)
			So(out.MaxPoints, ShouldEqual, 60) // 2 candidates × 3 × 10
			// alpha at order 1, beta at order 2 → average = 1.5
			So(out.AverageOrder, ShouldEqual, 1.5)
		})
	})
}
