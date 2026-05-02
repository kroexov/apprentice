package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"apisrv/pkg/db"

	"github.com/vmkteam/embedlog"
	"github.com/vmkteam/zenrpc/v2"
)

// AuthKey is the HTTP header carrying the bearer-style auth token. Same name
// as VT (`/v1/vt/`) so an admin authKey is effectively SSO between the two
// servers; rejects on candidate authKey are enforced inside this middleware.
const AuthKey = "Authorization2"

type userCtx string

const (
	adminKey     userCtx = "rpc.admin"
	candidateKey userCtx = "rpc.candidate"
)

// authMiddleware enforces admin-only access on every method that isn't on the
// open whitelist (auth.* and *.Get / *.GetByID). For open methods the header
// is optional — if present and valid, the principal is injected into context
// for downstream services to inspect; otherwise the request proceeds anonymous.
func authMiddleware(commonRepo *db.CommonRepo, apprRepo *db.ApprenticeRepo, logger embedlog.Logger) zenrpc.MiddlewareFunc {
	return func(h zenrpc.InvokeFunc) zenrpc.InvokeFunc {
		return func(ctx context.Context, method string, params json.RawMessage) zenrpc.Response {
			req, ok := zenrpc.RequestFromContext(ctx)
			if !ok {
				return h(ctx, method, params)
			}

			ns := zenrpc.NamespaceFromContext(ctx)
			authHeader := req.Header.Get(AuthKey)

			if isOpenMethod(ns, method) {
				// Opportunistic principal resolution — failure is silent.
				if authHeader != "" {
					ctx = injectPrincipal(ctx, commonRepo, apprRepo, authHeader)
				}
				return h(ctx, method, params)
			}

			if authHeader == "" {
				return zenrpc.NewResponseError(zenrpc.IDFromContext(ctx), ErrUnauthorized.Code, ErrUnauthorized.Message, ErrUnauthorized.Data)
			}

			// Protected methods accept admin authKey only. Candidate keys are
			// rejected here even when valid — admin-only is the contract.
			dbu, err := commonRepo.EnabledUserByAuthKey(ctx, authHeader)
			if err != nil {
				logger.Error(ctx, "auth lookup failed", "err", err)
				return zenrpc.NewResponseError(zenrpc.IDFromContext(ctx), http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError), nil)
			}
			if dbu == nil {
				return zenrpc.NewResponseError(zenrpc.IDFromContext(ctx), ErrUnauthorized.Code, ErrUnauthorized.Message, ErrUnauthorized.Data)
			}

			if dbu.LastActivityAt == nil || time.Since(*dbu.LastActivityAt) > 90*time.Second {
				if _, err := commonRepo.UpdateUserActivity(ctx, dbu); err != nil {
					logger.Error(ctx, "update admin activity", "err", err)
				}
			}

			return h(context.WithValue(ctx, adminKey, dbu), method, params)
		}
	}
}

// isOpenMethod is the bypass whitelist — any (namespace, method) tuple here
// runs without an auth header. Keep in sync with rpc_zenrpc.go constants.
func isOpenMethod(ns, method string) bool {
	switch ns {
	case NSAuth:
		return method == RPC.AuthService.Login || method == RPC.AuthService.Register
	case NSCandidate:
		return method == RPC.CandidateService.Get || method == RPC.CandidateService.GetByID
	case NSStage:
		return method == RPC.StageService.Get || method == RPC.StageService.GetByID
	}
	return false
}

// injectPrincipal opportunistically resolves an authHeader for open methods.
// Tries admin first, then candidate. Silent on failure — open methods don't
// require auth, so a bad header is just ignored.
func injectPrincipal(ctx context.Context, commonRepo *db.CommonRepo, apprRepo *db.ApprenticeRepo, authHeader string) context.Context {
	if dbu, err := commonRepo.EnabledUserByAuthKey(ctx, authHeader); err == nil && dbu != nil {
		return context.WithValue(ctx, adminKey, dbu)
	}
	if dbc, err := apprRepo.EnabledCandidateByAuthKey(ctx, authHeader); err == nil && dbc != nil {
		return context.WithValue(ctx, candidateKey, dbc)
	}
	return ctx
}

// AdminFromContext returns the admin user injected by authMiddleware on a
// protected (or opportunistically authenticated) request, or nil.
func AdminFromContext(ctx context.Context) *db.User {
	v, _ := ctx.Value(adminKey).(*db.User)
	return v
}

// CandidateFromContext returns the candidate principal injected by
// authMiddleware on an open method, or nil.
func CandidateFromContext(ctx context.Context) *db.Candidate {
	v, _ := ctx.Value(candidateKey).(*db.Candidate)
	return v
}
