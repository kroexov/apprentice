package rpc

import (
	"errors"
	"sync"
	"testing"
	"time"

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

		Convey("validateCredentials covers required + format + policy", func() {
			So(validateCredentials("", "passw0rd!"), ShouldEqual, ErrInvalidLoginPassword)
			So(validateCredentials("ok.login", ""), ShouldEqual, ErrInvalidLoginPassword)
			So(validateCredentials("BAD HANDLE", "passw0rd!"), ShouldEqual, ErrInvalidLoginPassword)
			So(validateCredentials("ok", "short"), ShouldEqual, ErrPasswordPolicy)
			So(validateCredentials("ok", "passw0rd!"), ShouldBeNil)
		})

		Convey("checkHash matches bcrypt round-trip", func() {
			h, err := passwordHash("hello123")
			So(err, ShouldBeNil)
			So(checkHash("hello123", h), ShouldBeTrue)
			So(checkHash("nope", h), ShouldBeFalse)
		})
	})
}
