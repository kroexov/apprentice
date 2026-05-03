package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"apisrv/pkg/db"
	"apisrv/pkg/db/test"

	. "github.com/smartystreets/goconvey/convey"
)

// rpcResponse is just enough of a JSON-RPC envelope to assert on results vs.
// errors. We assert on `error.code` to distinguish 401 (Unauthorized) from
// validation/internal failures.
type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

func newHTTPHarness(t *testing.T) (*httptest.Server, db.DB) {
	t.Helper()
	dbo, logger := test.Setup(t)
	resetApprenticeDB(t, dbo)

	srv := New(dbo, logger, true)
	hs := httptest.NewServer(srv)
	t.Cleanup(hs.Close)
	return hs, dbo
}

func rpcCall(t *testing.T, hs *httptest.Server, header, ns, method string, params ...any) rpcResponse {
	t.Helper()
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  ns + "." + method,
		"params":  params,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, hs.URL, bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if header != "" {
		req.Header.Set(AuthKey, header)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out rpcResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal %q: %v", string(raw), err)
	}
	return out
}

func TestDB_Middleware_BypassWhitelist(t *testing.T) {
	Convey("Open methods accessible without auth header", t, func() {
		hs, _ := newHTTPHarness(t)

		Convey("auth.login on unknown user → validation error, not 401", func() {
			r := rpcCall(t, hs, "", NSAuth, RPC.AuthService.Login, "ghost", "passw0rd!", UserTypeAdmin)
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusBadRequest)
		})

		Convey("auth.register without header → 200", func() {
			r := rpcCall(t, hs, "", NSAuth, RPC.AuthService.Register, "anon.user", "passw0rd!", UserTypeUser)
			// auth.register requires a stage seeded; here it expects ErrNoStagesAvailable.
			// Important: not 401.
			if r.Error != nil {
				So(r.Error.Code, ShouldNotEqual, http.StatusUnauthorized)
			}
		})

		Convey("stage.get without header → 200", func() {
			r := rpcCall(t, hs, "", NSStage, RPC.StageService.Get)
			So(r.Error, ShouldBeNil)
		})

		Convey("candidate.get without header → 200", func() {
			r := rpcCall(t, hs, "", NSCandidate, RPC.CandidateService.Get, CandidateSortPoints)
			So(r.Error, ShouldBeNil)
		})

		Convey("stage.getbyid without header is not rejected by auth", func() {
			r := rpcCall(t, hs, "", NSStage, RPC.StageService.GetByID, 999)
			if r.Error != nil {
				So(r.Error.Code, ShouldNotEqual, http.StatusUnauthorized)
			}
		})

		Convey("dashboard.summary without header → 200", func() {
			r := rpcCall(t, hs, "", NSDashboard, RPC.DashboardService.Summary)
			So(r.Error, ShouldBeNil)
		})
	})
}

func TestDB_Middleware_RejectsUnauthenticated(t *testing.T) {
	Convey("Protected methods require admin auth", t, func() {
		hs, _ := newHTTPHarness(t)

		Convey("stage.add without header → 401", func() {
			r := rpcCall(t, hs, "", NSStage, RPC.StageService.Add, map[string]any{
				"alias": "x", "order": 1, "title": "t", "shortTitle": "s", "maxScore": 10,
			})
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusUnauthorized)
		})

		Convey("candidate.delete with bogus key → 401", func() {
			r := rpcCall(t, hs, "definitely-not-a-real-authkey", NSCandidate, RPC.CandidateService.Delete, 1)
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusUnauthorized)
		})
	})
}

func TestDB_Middleware_AcceptsAdmin(t *testing.T) {
	Convey("Admin authKey passes the middleware", t, func() {
		hs, dbo := newHTTPHarness(t)
		key := seedAdmin(t, t.Context(), dbo, "admin.gateway")

		Convey("dashboard.summary with admin key → 200", func() {
			r := rpcCall(t, hs, key, NSDashboard, RPC.DashboardService.Summary)
			So(r.Error, ShouldBeNil)
			So(r.Result, ShouldNotBeNil)
		})

		Convey("stage.add with admin key → 200", func() {
			r := rpcCall(t, hs, key, NSStage, RPC.StageService.Add, map[string]any{
				"alias": "first", "order": 1, "title": "t", "shortTitle": "s", "maxScore": 10,
			})
			So(r.Error, ShouldBeNil)
		})
	})
}

func TestDB_Middleware_RejectsCandidateOnAdminMethod(t *testing.T) {
	Convey("Candidate authKey is rejected on protected admin methods", t, func() {
		hs, dbo := newHTTPHarness(t)
		adminKey := seedAdmin(t, t.Context(), dbo, "admin.seed")

		stageResp := rpcCall(t, hs, adminKey, NSStage, RPC.StageService.Add, map[string]any{
			"alias": "intro", "order": 1, "title": "t", "shortTitle": "s", "maxScore": 10,
		})
		So(stageResp.Error, ShouldBeNil)

		userResp := rpcCall(t, hs, "", NSAuth, RPC.AuthService.Register, "ivan.user", "passw0rd!", UserTypeUser)
		So(userResp.Error, ShouldBeNil)
		var candKey string
		So(json.Unmarshal(userResp.Result, &candKey), ShouldBeNil)

		Convey("candidate authKey on candidate.add → 401", func() {
			r := rpcCall(t, hs, candKey, NSCandidate, RPC.CandidateService.Add, map[string]any{
				"name": "Y", "handle": "y.y", "login": "y.y", "initials": "YY", "currentStageId": 1,
			})
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusUnauthorized)
		})

		Convey("but candidate authKey is OK on stage.get (open method)", func() {
			r := rpcCall(t, hs, candKey, NSStage, RPC.StageService.Get)
			So(r.Error, ShouldBeNil)
		})
	})
}

// TestDB_Middleware_RegisteredTier covers the third auth tier (admin OR
// candidate, but never anonymous), used by candidate.SetLink and intended
// for any future "any authenticated" RPC.
func TestDB_Middleware_RegisteredTier(t *testing.T) {
	Convey("Registered methods accept admin or candidate, reject anonymous", t, func() {
		hs, dbo := newHTTPHarness(t)
		ctx := t.Context()

		adminKey := seedAdmin(t, ctx, dbo, "admin.reg")

		// Seed one stage so candidate.Add succeeds and produces a candidateStage row.
		stageResp := rpcCall(t, hs, adminKey, NSStage, RPC.StageService.Add, map[string]any{
			"alias": "s1", "order": 1, "title": "t", "shortTitle": "s", "maxScore": 10,
		})
		So(stageResp.Error, ShouldBeNil)

		// Two candidates so we can also test cross-candidate access.
		ownerCand, ownerKey := registerCandidate(t, ctx, hs, dbo, "owner.user")
		_, otherKey := registerCandidate(t, ctx, hs, dbo, "other.user")

		// Find the empty CandidateStage created for ownerCand.
		repo := db.NewApprenticeRepo(dbo)
		csList, err := repo.CandidateStagesByFilters(ctx,
			&db.CandidateStageSearch{CandidateID: &ownerCand.ID}, db.PagerNoLimit)
		So(err, ShouldBeNil)
		So(csList, ShouldHaveLength, 1)
		csID := csList[0].ID

		Convey("anonymous → 401", func() {
			r := rpcCall(t, hs, "", NSCandidate, RPC.CandidateService.SetLink, csID, "https://example.com/x")
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusUnauthorized)
		})

		Convey("admin can set on any candidateStage", func() {
			r := rpcCall(t, hs, adminKey, NSCandidate, RPC.CandidateService.SetLink, csID, "https://example.com/admin")
			So(r.Error, ShouldBeNil)
		})

		Convey("candidate can set on own candidateStage", func() {
			r := rpcCall(t, hs, ownerKey, NSCandidate, RPC.CandidateService.SetLink, csID, "https://example.com/own")
			So(r.Error, ShouldBeNil)

			updated, err := repo.CandidateStageByID(ctx, csID, repo.FullCandidateStage())
			So(err, ShouldBeNil)
			So(updated.Link, ShouldNotBeNil)
			So(*updated.Link, ShouldEqual, "https://example.com/own")
		})

		Convey("candidate cannot set on someone else's candidateStage → 403", func() {
			r := rpcCall(t, hs, otherKey, NSCandidate, RPC.CandidateService.SetLink, csID, "https://example.com/oops")
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusForbidden)
		})

		Convey("nil link detaches", func() {
			// Attach first.
			r := rpcCall(t, hs, ownerKey, NSCandidate, RPC.CandidateService.SetLink, csID, "https://example.com/a")
			So(r.Error, ShouldBeNil)
			// Then detach with explicit null (raw JSON to ensure null vs missing).
			r = rpcCallRaw(t, hs, ownerKey, NSCandidate, RPC.CandidateService.SetLink, []byte(`[`+strconv.Itoa(csID)+`,null]`))
			So(r.Error, ShouldBeNil)

			updated, err := repo.CandidateStageByID(ctx, csID, repo.FullCandidateStage())
			So(err, ShouldBeNil)
			So(updated.Link, ShouldBeNil)
		})

		Convey("invalid link → 400", func() {
			r := rpcCall(t, hs, ownerKey, NSCandidate, RPC.CandidateService.SetLink, csID, "ftp://nope")
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusBadRequest)
		})

		Convey("unknown candidateStageId → 404", func() {
			r := rpcCall(t, hs, adminKey, NSCandidate, RPC.CandidateService.SetLink, 99999, "https://example.com/x")
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusNotFound)
		})
	})
}

// TestDB_CandidateService_SetAvatarURL covers candidate.setavatarurl across the
// same auth tiers as SetLink: anonymous→401, admin OK, self-candidate OK, other
// candidate→403, plus URL validation and detach-on-null.
func TestDB_CandidateService_SetAvatarURL(t *testing.T) {
	Convey("candidate.setavatarurl", t, func() {
		hs, dbo := newHTTPHarness(t)
		ctx := t.Context()

		adminKey := seedAdmin(t, ctx, dbo, "admin.av")

		stageResp := rpcCall(t, hs, adminKey, NSStage, RPC.StageService.Add, map[string]any{
			"alias": "s1", "order": 1, "title": "t", "shortTitle": "s", "maxScore": 10,
		})
		So(stageResp.Error, ShouldBeNil)

		ownerCand, ownerKey := registerCandidate(t, ctx, hs, dbo, "owner.av")
		_, otherKey := registerCandidate(t, ctx, hs, dbo, "other.av")

		repo := db.NewApprenticeRepo(dbo)

		Convey("anonymous → 401", func() {
			r := rpcCall(t, hs, "", NSCandidate, RPC.CandidateService.SetAvatarURL, ownerCand.ID, "https://example.com/a.png")
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusUnauthorized)
		})

		Convey("admin can set on any candidate", func() {
			r := rpcCall(t, hs, adminKey, NSCandidate, RPC.CandidateService.SetAvatarURL, ownerCand.ID, "https://example.com/admin.png")
			So(r.Error, ShouldBeNil)

			updated, err := repo.CandidateByID(ctx, ownerCand.ID)
			So(err, ShouldBeNil)
			So(updated.AvatarUrl, ShouldNotBeNil)
			So(*updated.AvatarUrl, ShouldEqual, "https://example.com/admin.png")
		})

		Convey("candidate can set on self", func() {
			r := rpcCall(t, hs, ownerKey, NSCandidate, RPC.CandidateService.SetAvatarURL, ownerCand.ID, "https://example.com/own.png")
			So(r.Error, ShouldBeNil)

			updated, err := repo.CandidateByID(ctx, ownerCand.ID)
			So(err, ShouldBeNil)
			So(updated.AvatarUrl, ShouldNotBeNil)
			So(*updated.AvatarUrl, ShouldEqual, "https://example.com/own.png")
		})

		Convey("candidate cannot set on someone else → 403", func() {
			r := rpcCall(t, hs, otherKey, NSCandidate, RPC.CandidateService.SetAvatarURL, ownerCand.ID, "https://example.com/oops.png")
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusForbidden)
		})

		Convey("nil avatarUrl detaches", func() {
			r := rpcCall(t, hs, ownerKey, NSCandidate, RPC.CandidateService.SetAvatarURL, ownerCand.ID, "https://example.com/a.png")
			So(r.Error, ShouldBeNil)
			r = rpcCallRaw(t, hs, ownerKey, NSCandidate, RPC.CandidateService.SetAvatarURL, []byte(`[`+strconv.Itoa(ownerCand.ID)+`,null]`))
			So(r.Error, ShouldBeNil)

			updated, err := repo.CandidateByID(ctx, ownerCand.ID)
			So(err, ShouldBeNil)
			So(updated.AvatarUrl, ShouldBeNil)
		})

		Convey("empty/whitespace string detaches", func() {
			r := rpcCall(t, hs, ownerKey, NSCandidate, RPC.CandidateService.SetAvatarURL, ownerCand.ID, "https://example.com/a.png")
			So(r.Error, ShouldBeNil)

			r = rpcCall(t, hs, ownerKey, NSCandidate, RPC.CandidateService.SetAvatarURL, ownerCand.ID, "")
			So(r.Error, ShouldBeNil)
			updated, err := repo.CandidateByID(ctx, ownerCand.ID)
			So(err, ShouldBeNil)
			So(updated.AvatarUrl, ShouldBeNil)

			r = rpcCall(t, hs, ownerKey, NSCandidate, RPC.CandidateService.SetAvatarURL, ownerCand.ID, "https://example.com/b.png")
			So(r.Error, ShouldBeNil)

			r = rpcCall(t, hs, ownerKey, NSCandidate, RPC.CandidateService.SetAvatarURL, ownerCand.ID, "   ")
			So(r.Error, ShouldBeNil)
			updated, err = repo.CandidateByID(ctx, ownerCand.ID)
			So(err, ShouldBeNil)
			So(updated.AvatarUrl, ShouldBeNil)
		})

		Convey("invalid scheme → 400", func() {
			r := rpcCall(t, hs, ownerKey, NSCandidate, RPC.CandidateService.SetAvatarURL, ownerCand.ID, "ftp://nope")
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusBadRequest)
		})

		Convey("too-long URL → 400", func() {
			long := "https://example.com/" + strings.Repeat("x", candidateStageLinkMaxLen)
			r := rpcCall(t, hs, ownerKey, NSCandidate, RPC.CandidateService.SetAvatarURL, ownerCand.ID, long)
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusBadRequest)
		})

		Convey("unknown candidateId → 404", func() {
			r := rpcCall(t, hs, adminKey, NSCandidate, RPC.CandidateService.SetAvatarURL, 99999, "https://example.com/x.png")
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusNotFound)
		})
	})
}

// TestDB_AuthService_Me covers auth.me end-to-end: anonymous is rejected, admin
// and candidate principals each round-trip their own login + userType.
func TestDB_AuthService_Me(t *testing.T) {
	Convey("auth.me", t, func() {
		hs, dbo := newHTTPHarness(t)
		ctx := t.Context()
		adminKey := seedAdmin(t, ctx, dbo, "admin.me")

		stageResp := rpcCall(t, hs, adminKey, NSStage, RPC.StageService.Add, map[string]any{
			"alias": "s1", "order": 1, "title": "t", "shortTitle": "s", "maxScore": 10,
		})
		So(stageResp.Error, ShouldBeNil)
		cand, candKey := registerCandidate(t, ctx, hs, dbo, "ivan.me")

		Convey("anonymous → 401", func() {
			r := rpcCall(t, hs, "", NSAuth, RPC.AuthService.Me)
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusUnauthorized)
		})

		Convey("bogus key → 401", func() {
			r := rpcCall(t, hs, "definitely-not-a-real-authkey", NSAuth, RPC.AuthService.Me)
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusUnauthorized)
		})

		Convey("admin key → admin payload", func() {
			r := rpcCall(t, hs, adminKey, NSAuth, RPC.AuthService.Me)
			So(r.Error, ShouldBeNil)
			var me Me
			So(json.Unmarshal(r.Result, &me), ShouldBeNil)
			So(me.Login, ShouldEqual, "admin.me")
			So(me.UserType, ShouldEqual, MeTypeAdmin)
			So(me.UserID, ShouldBeGreaterThan, 0)
		})

		Convey("candidate key → candidate payload", func() {
			r := rpcCall(t, hs, candKey, NSAuth, RPC.AuthService.Me)
			So(r.Error, ShouldBeNil)
			var me Me
			So(json.Unmarshal(r.Result, &me), ShouldBeNil)
			So(me.Login, ShouldEqual, "ivan.me")
			So(me.UserType, ShouldEqual, MeTypeCandidate)
			So(me.UserID, ShouldEqual, cand.ID)
		})

		// Re-issued authKey from auth.login (vs. the register-issued one above)
		// must also pass the registered tier and round-trip the same payload.
		Convey("candidate key issued via auth.login → candidate payload", func() {
			loginResp := rpcCall(t, hs, "", NSAuth, RPC.AuthService.Login, "ivan.me", "passw0rd!", UserTypeUser)
			So(loginResp.Error, ShouldBeNil)
			var loginKey string
			So(json.Unmarshal(loginResp.Result, &loginKey), ShouldBeNil)

			r := rpcCall(t, hs, loginKey, NSAuth, RPC.AuthService.Me)
			So(r.Error, ShouldBeNil)
			var me Me
			So(json.Unmarshal(r.Result, &me), ShouldBeNil)
			So(me.Login, ShouldEqual, "ivan.me")
			So(me.UserType, ShouldEqual, MeTypeCandidate)
			So(me.UserID, ShouldEqual, cand.ID)
		})
	})
}

// rpcCallRaw lets a test send a raw JSON params array (e.g. to embed an explicit
// `null`) instead of the default Go-marshalled []any.
func rpcCallRaw(t *testing.T, hs *httptest.Server, header, ns, method string, rawParams []byte) rpcResponse {
	t.Helper()
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"` + ns + `.` + method + `","params":` + string(rawParams) + `}`)
	req, err := http.NewRequest(http.MethodPost, hs.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if header != "" {
		req.Header.Set(AuthKey, header)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out rpcResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal %q: %v", string(raw), err)
	}
	return out
}

// registerCandidate creates a candidate via auth.register, returning the
// resulting *Candidate and the authKey. Resolves the candidate via the live
// DB by login since auth.register only echoes back the authKey.
func registerCandidate(t *testing.T, ctx context.Context, hs *httptest.Server, dbo db.DB, login string) (*db.Candidate, string) {
	t.Helper()
	resp := rpcCall(t, hs, "", NSAuth, RPC.AuthService.Register, login, "passw0rd!", UserTypeUser)
	if resp.Error != nil {
		t.Fatalf("register %s: code=%d %s", login, resp.Error.Code, resp.Error.Message)
	}
	var key string
	if err := json.Unmarshal(resp.Result, &key); err != nil {
		t.Fatalf("unmarshal authKey: %v", err)
	}
	cand, err := db.NewApprenticeRepo(dbo).EnabledCandidateByLogin(ctx, login)
	if err != nil || cand == nil {
		t.Fatalf("resolve candidate %s: %v", login, err)
	}
	return cand, key
}
