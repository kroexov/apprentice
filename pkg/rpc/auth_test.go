package rpc

import (
	"errors"
	"sync"
	"testing"
	"time"

	"apisrv/pkg/db"

	. "github.com/smartystreets/goconvey/convey"
)

func TestDB_AuthService_Register(t *testing.T) {
	Convey("AuthService.Register (user only)", t, func() {
		f := newRPCFixtures(t)
		ctx := t.Context()
		auth := f.auth

		Convey("admin self-registration is disabled", func() {
			_, err := auth.Register(ctx, "admin.alice", "passw0rd!", UserTypeAdmin)
			So(err, ShouldEqual, ErrInvalidUserType)
		})

		Convey("user: needs at least one stage", func() {
			_, err := auth.Register(ctx, "user.first", "passw0rd!", UserTypeUser)
			So(err, ShouldEqual, ErrNoStagesAvailable)
		})

		Convey("user: happy path creates candidate with auto-filled fields", func() {
			stages := makeStages(t, ctx, f.stage, 2)
			key, err := auth.Register(ctx, "kate.smith", "passw0rd!", UserTypeUser)
			So(err, ShouldBeNil)
			So(len(key), ShouldBeGreaterThan, 20)

			cand, err := f.repo.EnabledCandidateByAuthKey(ctx, key)
			So(err, ShouldBeNil)
			So(cand, ShouldNotBeNil)
			So(cand.Login, ShouldEqual, "kate.smith")
			So(cand.Handle, ShouldEqual, "kate.smith")
			So(cand.Name, ShouldEqual, "kate.smith")
			So(cand.Initials, ShouldEqual, "KA")
			So(cand.CurrentStageID, ShouldEqual, stages[0].ID)
		})

		Convey("user: duplicate login → ErrLoginTaken", func() {
			_ = makeStages(t, ctx, f.stage, 1)
			_, err := auth.Register(ctx, "twin.user", "passw0rd!", UserTypeUser)
			So(err, ShouldBeNil)
			_, err = auth.Register(ctx, "twin.user", "passw0rd!", UserTypeUser)
			So(err, ShouldEqual, ErrLoginTaken)
		})

		Convey("user: handle collision with existing candidate → ErrLoginTaken", func() {
			stages := makeStages(t, ctx, f.stage, 1)
			_ = makeCandidate(t, ctx, f.candidate, "shared.handle", stages[0].ID)
			_, err := auth.Register(ctx, "shared.handle", "passw0rd!", UserTypeUser)
			// Login uniqueness check fires first because makeCandidate sets Login=Handle.
			So(err, ShouldEqual, ErrLoginTaken)
		})

		Convey("rejects bad userType", func() {
			_, err := auth.Register(ctx, "x.y", "passw0rd!", "ghost")
			So(err, ShouldEqual, ErrInvalidUserType)
		})

		Convey("rejects bad login format", func() {
			_, err := auth.Register(ctx, "BAD HANDLE", "passw0rd!", UserTypeUser)
			So(err, ShouldEqual, ErrInvalidLoginPassword)
		})

		Convey("rejects empty login/password", func() {
			_, err := auth.Register(ctx, "", "passw0rd!", UserTypeUser)
			So(err, ShouldEqual, ErrInvalidLoginPassword)
			_, err = auth.Register(ctx, "ok.login", "", UserTypeUser)
			So(err, ShouldEqual, ErrInvalidLoginPassword)
		})

		Convey("rejects too-short password", func() {
			_, err := auth.Register(ctx, "ok.login", "short", UserTypeUser)
			So(err, ShouldEqual, ErrPasswordPolicy)
		})

		Convey("rejects too-long password (>72 bytes)", func() {
			long := make([]byte, 73)
			for i := range long {
				long[i] = 'a'
			}
			_, err := auth.Register(ctx, "ok.login", string(long), UserTypeUser)
			So(err, ShouldEqual, ErrPasswordPolicy)
		})
	})
}

func TestDB_AuthService_SignUp(t *testing.T) {
	Convey("AuthService.SignUp", t, func() {
		f := newRPCFixtures(t)
		ctx := t.Context()
		auth := f.auth

		Convey("happy path: persists every basic field, defaults handle/initials", func() {
			stages := makeStages(t, ctx, f.stage, 2)
			age := 27
			avatar := "https://example.com/a.png"
			key, err := auth.SignUp(ctx, SignUpParams{
				Login:       "polly.signup",
				Password:    "passw0rd!",
				Name:        "Полина Знаменская",
				City:        "Москва",
				Age:         &age,
				Bio:         "frontend engineer",
				AvatarColor: "#5b8def",
				AvatarURL:   &avatar,
				Strengths:   []string{"go", "react"},
				Weaknesses:  []string{"docs"},
			})
			So(err, ShouldBeNil)
			So(len(key), ShouldBeGreaterThan, 20)

			cand, err := f.repo.EnabledCandidateByAuthKey(ctx, key)
			So(err, ShouldBeNil)
			So(cand, ShouldNotBeNil)
			So(cand.Login, ShouldEqual, "polly.signup")
			So(cand.Handle, ShouldEqual, "polly.signup")
			So(cand.Name, ShouldEqual, "Полина Знаменская")
			So(cand.City, ShouldEqual, "Москва")
			So(*cand.Age, ShouldEqual, 27)
			So(cand.Bio, ShouldEqual, "frontend engineer")
			So(cand.AvatarColor, ShouldEqual, "#5b8def")
			So(cand.Initials, ShouldEqual, "PO")
			So(*cand.AvatarUrl, ShouldEqual, avatar)
			So(cand.Strengths, ShouldResemble, []string{"go", "react"})
			So(cand.Weaknesses, ShouldResemble, []string{"docs"})
			So(cand.CurrentStageID, ShouldEqual, stages[0].ID)
		})

		Convey("custom handle and initials override defaults", func() {
			_ = makeStages(t, ctx, f.stage, 1)
			handle := "polly.h"
			initials := "ПЗ"
			_, err := auth.SignUp(ctx, SignUpParams{
				Login:    "polly.custom",
				Password: "passw0rd!",
				Name:     "Полина",
				Handle:   &handle,
				Initials: &initials,
			})
			So(err, ShouldBeNil)

			cand, err := f.repo.OneCandidate(ctx, &db.CandidateSearch{Login: ptrString("polly.custom")})
			So(err, ShouldBeNil)
			So(cand.Handle, ShouldEqual, "polly.h")
			So(cand.Initials, ShouldEqual, "ПЗ")
		})

		Convey("empty Name → ValidationError", func() {
			_ = makeStages(t, ctx, f.stage, 1)
			_, err := auth.SignUp(ctx, SignUpParams{
				Login:    "polly.noname",
				Password: "passw0rd!",
				Name:     "",
			})
			So(err, ShouldNotBeNil)
		})

		Convey("duplicate login → ErrLoginTaken", func() {
			_ = makeStages(t, ctx, f.stage, 1)
			_, err := auth.SignUp(ctx, SignUpParams{
				Login: "twin.signup", Password: "passw0rd!", Name: "Twin",
			})
			So(err, ShouldBeNil)
			_, err = auth.SignUp(ctx, SignUpParams{
				Login: "twin.signup", Password: "passw0rd!", Name: "Twin Two",
			})
			So(err, ShouldEqual, ErrLoginTaken)
		})

		Convey("duplicate handle → ErrHandleTaken", func() {
			_ = makeStages(t, ctx, f.stage, 1)
			handle := "common.handle"
			_, err := auth.SignUp(ctx, SignUpParams{
				Login: "first.signup", Password: "passw0rd!", Name: "First", Handle: &handle,
			})
			So(err, ShouldBeNil)
			_, err = auth.SignUp(ctx, SignUpParams{
				Login: "second.signup", Password: "passw0rd!", Name: "Second", Handle: &handle,
			})
			So(err, ShouldEqual, ErrHandleTaken)
		})

		Convey("no stages available → ErrNoStagesAvailable", func() {
			_, err := auth.SignUp(ctx, SignUpParams{
				Login: "polly.nostages", Password: "passw0rd!", Name: "P",
			})
			So(err, ShouldEqual, ErrNoStagesAvailable)
		})

		Convey("rejects bad password policy", func() {
			_, err := auth.SignUp(ctx, SignUpParams{
				Login: "polly.short", Password: "short", Name: "P",
			})
			So(err, ShouldEqual, ErrPasswordPolicy)
		})
	})
}

func TestDB_AuthService_Register_Concurrent(t *testing.T) {
	Convey("Concurrent Register for the same login → exactly one wins", t, func() {
		f := newRPCFixtures(t)
		ctx := t.Context()
		_ = makeStages(t, ctx, f.stage, 1)

		const total = 4
		var wg sync.WaitGroup
		results := make([]error, total)
		wg.Add(total)
		for i := range total {
			go func(idx int) {
				defer wg.Done()
				_, results[idx] = f.auth.Register(ctx, "race.user", "passw0rd!", UserTypeUser)
			}(i)
		}
		wg.Wait()

		successes, taken := 0, 0
		for _, err := range results {
			switch {
			case err == nil:
				successes++
			case errors.Is(err, ErrLoginTaken):
				taken++
			default:
				t.Fatalf("unexpected error: %v", err)
			}
		}
		So(successes, ShouldEqual, 1)
		So(taken, ShouldEqual, total-1)
	})
}

func TestDB_AuthService_Login(t *testing.T) {
	Convey("AuthService.Login", t, func() {
		f := newRPCFixtures(t)
		ctx := t.Context()
		auth := f.auth

		Convey("admin: login of seeded admin user", func() {
			seedAdmin(t, ctx, f.dbo, "admin.dave")

			key, err := auth.Login(ctx, "admin.dave", "passw0rd!", UserTypeAdmin)
			So(err, ShouldBeNil)
			So(len(key), ShouldBeGreaterThan, 20)
		})

		Convey("admin: legacy short password from init.sql still works", func() {
			// bcrypt hash of "12345" — same one init.sql seeds for admin and
			// every candidate. Login must accept it even though Register
			// would reject 12345 today via the password policy.
			const seedHash = "$2y$14$4IpqlaJ2Rvfgs.wb8f6lPODVLb/Ygl6zw1ZCUKz5CuT6WB6CV44AG"
			seedAdminWithRawHash(t, ctx, f.dbo, "admin.legacy", seedHash)

			key, err := auth.Login(ctx, "admin.legacy", "12345", UserTypeAdmin)
			So(err, ShouldBeNil)
			So(len(key), ShouldBeGreaterThan, 20)
		})

		Convey("admin: wrong password → ErrInvalidLoginPassword", func() {
			seedAdmin(t, ctx, f.dbo, "admin.dave")
			_, err := auth.Login(ctx, "admin.dave", "wrong-pass", UserTypeAdmin)
			So(err, ShouldEqual, ErrInvalidLoginPassword)
		})

		Convey("admin: unknown login → ErrInvalidLoginPassword", func() {
			_, err := auth.Login(ctx, "ghost.user", "passw0rd!", UserTypeAdmin)
			So(err, ShouldEqual, ErrInvalidLoginPassword)
		})

		Convey("user: login of registered candidate", func() {
			_ = makeStages(t, ctx, f.stage, 1)
			_, err := auth.Register(ctx, "user.alice", "passw0rd!", UserTypeUser)
			So(err, ShouldBeNil)

			key, err := auth.Login(ctx, "user.alice", "passw0rd!", UserTypeUser)
			So(err, ShouldBeNil)
			So(len(key), ShouldBeGreaterThan, 20)
		})

		Convey("user: wrong password → ErrInvalidLoginPassword", func() {
			_ = makeStages(t, ctx, f.stage, 1)
			_, err := auth.Register(ctx, "user.alice", "passw0rd!", UserTypeUser)
			So(err, ShouldBeNil)
			_, err = auth.Login(ctx, "user.alice", "WRONG-pass", UserTypeUser)
			So(err, ShouldEqual, ErrInvalidLoginPassword)
		})

		Convey("user: invalid userType", func() {
			_, err := auth.Login(ctx, "x.y", "passw0rd!", "ghost")
			So(err, ShouldEqual, ErrInvalidUserType)
		})
	})
}

// TestDB_AuthService_Login_Timing checks that login response times don't leak
// the existence of an account. With dummy-bcrypt-on-miss, the absent-user path
// must take comparable time to the present-user path. We don't assert exact
// equality (CPU jitter is real) — just that the absent-user case spends
// enough time to suggest a bcrypt was actually run.
func TestDB_AuthService_Login_Timing(t *testing.T) {
	// The whole point of this test is to verify that bcrypt is engaged on
	// both real and ghost logins (anti-enumeration via constant-time work).
	// At MinCost (the value the test binary forces for speed) hashing takes
	// ~3ms, well under our 50ms floor — and the test is meaningless anyway
	// since the protection only matters at production cost.
	if bcryptCost < 10 {
		t.Skip("timing test requires production-cost bcrypt")
	}
	Convey("Login response time doesn't leak user existence", t, func() {
		f := newRPCFixtures(t)
		ctx := t.Context()
		seedAdmin(t, ctx, f.dbo, "real.admin")

		measure := func(login string) time.Duration {
			start := time.Now()
			_, _ = f.auth.Login(ctx, login, "wrong-password", UserTypeAdmin)
			return time.Since(start)
		}

		realDur := measure("real.admin")
		ghostDur := measure("ghost.user")

		// Both paths must engage bcrypt — give a generous lower bound (50 ms)
		// since cost-14 takes ~1s but CI machines are noisy.
		So(realDur, ShouldBeGreaterThan, 50*time.Millisecond)
		So(ghostDur, ShouldBeGreaterThan, 50*time.Millisecond)
	})
}

func TestDB_AuthService_Helpers(t *testing.T) {
	Convey("AuthService helpers", t, func() {
		Convey("defaultInitials extracts up to 2 alphanumerics", func() {
			So(defaultInitials("ivan"), ShouldEqual, "IV")
			So(defaultInitials("a"), ShouldEqual, "A")
			So(defaultInitials("..."), ShouldEqual, "X")
			So(defaultInitials("k.smith"), ShouldEqual, "KS")
		})

		Convey("generateAuthKey returns sufficiently-long unique tokens", func() {
			seen := make(map[string]bool, 32)
			for range 32 {
				k := generateAuthKey()
				So(len(k), ShouldBeGreaterThanOrEqualTo, 32)
				So(seen[k], ShouldBeFalse)
				seen[k] = true
			}
		})

		Convey("validateLoginCredentials only rejects empty fields", func() {
			So(validateLoginCredentials("", "passw0rd!"), ShouldEqual, ErrInvalidLoginPassword)
			So(validateLoginCredentials("anything", ""), ShouldEqual, ErrInvalidLoginPassword)
			// Short password and bad format pass — bcrypt path takes over.
			So(validateLoginCredentials("anything", "12345"), ShouldBeNil)
			So(validateLoginCredentials("BAD HANDLE", "12345"), ShouldBeNil)
		})

		Convey("validateRegisterCredentials enforces full format + policy", func() {
			So(validateRegisterCredentials("", "passw0rd!"), ShouldEqual, ErrInvalidLoginPassword)
			So(validateRegisterCredentials("ok.login", ""), ShouldEqual, ErrInvalidLoginPassword)
			So(validateRegisterCredentials("BAD HANDLE", "passw0rd!"), ShouldEqual, ErrInvalidLoginPassword)
			So(validateRegisterCredentials("ok", "short"), ShouldEqual, ErrPasswordPolicy)
			So(validateRegisterCredentials("ok", "passw0rd!"), ShouldBeNil)
		})

		Convey("checkHash matches bcrypt round-trip", func() {
			h, err := passwordHash("hello123")
			So(err, ShouldBeNil)
			So(checkHash("hello123", h), ShouldBeTrue)
			So(checkHash("nope", h), ShouldBeFalse)
		})
	})
}
