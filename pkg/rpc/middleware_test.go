package rpc

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	})
}

func TestDB_Middleware_RejectsUnauthenticated(t *testing.T) {
	Convey("Protected methods require admin auth", t, func() {
		hs, _ := newHTTPHarness(t)

		Convey("dashboard.summary without header → 401", func() {
			r := rpcCall(t, hs, "", NSDashboard, RPC.DashboardService.Summary)
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusUnauthorized)
		})

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

		Convey("candidate authKey on dashboard.summary → 401", func() {
			r := rpcCall(t, hs, candKey, NSDashboard, RPC.DashboardService.Summary)
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusUnauthorized)
		})

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
