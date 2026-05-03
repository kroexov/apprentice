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

// authMiddleware enforces three access tiers:
//
//   - open (isOpenMethod): no header required; if a valid header is present,
//     principal is opportunistically injected for downstream services.
//   - registered (isRegisteredMethod): header required; admin OR candidate
//     authKey is accepted, anonymous is rejected. Per-row ownership checks
//     happen inside the service method.
//   - protected (default): header required; admin authKey only.
func authMiddleware(commonRepo *db.CommonRepo, apprRepo *db.ApprenticeRepo, logger embedlog.Logger) zenrpc.MiddlewareFunc {
	return func(h zenrpc.InvokeFunc) zenrpc.InvokeFunc {
		return func(ctx context.Context, method string, params json.RawMessage) zenrpc.Response {
			req, ok := zenrpc.RequestFromContext(ctx)
			if !ok {
				return h(ctx, method, params)
			}

			ns := zenrpc.NamespaceFromContext(ctx)
			authHeader := req.Header.Get(AuthKey)

			switch {
			case isOpenMethod(ns, method):
				if authHeader != "" {
					ctx = injectPrincipal(ctx, commonRepo, apprRepo, authHeader, logger)
				}
				return h(ctx, method, params)
			case isRegisteredMethod(ns, method):
				newCtx, status := handleRegistered(ctx, commonRepo, apprRepo, authHeader, logger)
				if status != 0 {
					return responseError(ctx, status)
				}
				return h(newCtx, method, params)
			default:
				newCtx, status := handleProtected(ctx, commonRepo, authHeader, logger)
				if status != 0 {
					return responseError(ctx, status)
				}
				return h(newCtx, method, params)
			}
		}
	}
}

// handleRegistered resolves admin-or-candidate, returning (ctx, 0) on success
// or (ctx, status) where status is the HTTP code to return.
func handleRegistered(ctx context.Context, commonRepo *db.CommonRepo, apprRepo *db.ApprenticeRepo, authHeader string, logger embedlog.Logger) (context.Context, int) {
	if authHeader == "" {
		return ctx, http.StatusUnauthorized
	}
	newCtx, ok, err := resolvePrincipal(ctx, commonRepo, apprRepo, authHeader, logger)
	if err != nil {
		return ctx, http.StatusInternalServerError
	}
	if !ok {
		return ctx, http.StatusUnauthorized
	}
	return newCtx, 0
}

// handleProtected enforces admin-only. Candidate authKeys are rejected here
// even when valid — admin-only is the contract for protected methods.
func handleProtected(ctx context.Context, commonRepo *db.CommonRepo, authHeader string, logger embedlog.Logger) (context.Context, int) {
	if authHeader == "" {
		return ctx, http.StatusUnauthorized
	}
	dbu, err := commonRepo.EnabledUserByAuthKey(ctx, authHeader)
	if err != nil {
		logger.Error(ctx, "auth lookup failed", "err", err)
		return ctx, http.StatusInternalServerError
	}
	if dbu == nil {
		return ctx, http.StatusUnauthorized
	}
	if dbu.LastActivityAt == nil || time.Since(*dbu.LastActivityAt) > 90*time.Second {
		if _, upErr := commonRepo.UpdateUserActivity(ctx, dbu); upErr != nil {
			logger.Error(ctx, "update admin activity", "err", upErr)
		}
	}
	return context.WithValue(ctx, adminKey, dbu), 0
}

// responseError translates a status code into the standard zenrpc error response.
func responseError(ctx context.Context, status int) zenrpc.Response {
	if status == http.StatusUnauthorized {
		return zenrpc.NewResponseError(zenrpc.IDFromContext(ctx), ErrUnauthorized.Code, ErrUnauthorized.Message, ErrUnauthorized.Data)
	}
	return zenrpc.NewResponseError(zenrpc.IDFromContext(ctx), status, http.StatusText(status), nil)
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
	case NSDashboard:
		return method == RPC.DashboardService.Summary
	}
	return false
}

// isRegisteredMethod lists methods that require any authenticated principal
// (admin or candidate) but reject anonymous requests. Per-row authorization
// (e.g. candidate may only edit own rows) happens inside the service method.
func isRegisteredMethod(ns, method string) bool {
	switch ns {
	case NSCandidate:
		return method == RPC.CandidateService.SetLink
	case NSAuth:
		return method == RPC.AuthService.Me
	}
	return false
}

// resolvePrincipal tries admin first, then candidate. On success, returns a
// new ctx with the principal injected and bumps lastActivityAt opportunistically.
// Returns (ctx, true, nil) on success, (ctx, false, nil) if neither matched,
// (ctx, false, err) on infrastructure errors.
func resolvePrincipal(ctx context.Context, commonRepo *db.CommonRepo, apprRepo *db.ApprenticeRepo, authHeader string, logger embedlog.Logger) (context.Context, bool, error) {
	dbu, err := commonRepo.EnabledUserByAuthKey(ctx, authHeader)
	if err != nil {
		logger.Error(ctx, "admin auth lookup failed", "err", err)
		return ctx, false, err
	}
	if dbu != nil {
		if dbu.LastActivityAt == nil || time.Since(*dbu.LastActivityAt) > 90*time.Second {
			if _, upErr := commonRepo.UpdateUserActivity(ctx, dbu); upErr != nil {
				logger.Error(ctx, "update admin activity", "err", upErr)
			}
		}
		return context.WithValue(ctx, adminKey, dbu), true, nil
	}

	dbc, err := apprRepo.EnabledCandidateByAuthKey(ctx, authHeader)
	if err != nil {
		logger.Error(ctx, "candidate auth lookup failed", "err", err)
		return ctx, false, err
	}
	if dbc != nil {
		if dbc.LastActivityAt == nil || time.Since(*dbc.LastActivityAt) > 90*time.Second {
			if _, upErr := apprRepo.UpdateCandidateActivity(ctx, dbc); upErr != nil {
				logger.Error(ctx, "update candidate activity", "err", upErr)
			}
		}
		return context.WithValue(ctx, candidateKey, dbc), true, nil
	}

	return ctx, false, nil
}

// injectPrincipal opportunistically resolves an authHeader for open methods.
// A bad/expired header just leaves the request anonymous (open methods don't
// require auth), but DB-side errors are logged so we don't silently miss
// repository failures during prod debugging. Activity bump intentionally
// skipped — open methods are read paths and should stay cheap.
func injectPrincipal(ctx context.Context, commonRepo *db.CommonRepo, apprRepo *db.ApprenticeRepo, authHeader string, logger embedlog.Logger) context.Context {
	dbu, err := commonRepo.EnabledUserByAuthKey(ctx, authHeader)
	if err != nil {
		logger.Error(ctx, "open-tier admin auth lookup failed", "err", err)
	} else if dbu != nil {
		return context.WithValue(ctx, adminKey, dbu)
	}
	dbc, err := apprRepo.EnabledCandidateByAuthKey(ctx, authHeader)
	if err != nil {
		logger.Error(ctx, "open-tier candidate auth lookup failed", "err", err)
	} else if dbc != nil {
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
// authMiddleware on an open or registered method, or nil.
func CandidateFromContext(ctx context.Context) *db.Candidate {
	v, _ := ctx.Value(candidateKey).(*db.Candidate)
	return v
}
