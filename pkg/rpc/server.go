package rpc

import (
	"net/http"

	"apisrv/pkg/db"

	"github.com/vmkteam/embedlog"
	zm "github.com/vmkteam/zenrpc-middleware"
	"github.com/vmkteam/zenrpc/v2"
)

const (
	NSStage     = "stage"
	NSCandidate = "candidate"
	NSDashboard = "dashboard"
	NSAuth      = "auth"
)

var (
	ErrUnauthorized = zenrpc.NewStringError(http.StatusUnauthorized, http.StatusText(http.StatusUnauthorized))

	ErrInvalidLoginPassword = zenrpc.NewStringError(http.StatusBadRequest, "invalid login or password")
	ErrLoginTaken           = zenrpc.NewStringError(http.StatusBadRequest, "login is already taken")
	ErrInvalidUserType      = zenrpc.NewStringError(http.StatusBadRequest, "invalid userType")
	ErrPasswordPolicy       = zenrpc.NewStringError(http.StatusBadRequest, "password policy violation")
	ErrNoStagesAvailable    = zenrpc.NewStringError(http.StatusBadRequest, "register requires at least one stage")
)

func InternalError(err error) *zenrpc.Error {
	return zenrpc.NewError(http.StatusInternalServerError, err)
}

var allowDebugFn = func() zm.AllowDebugFunc {
	return func(req *http.Request) bool {
		return req != nil && req.FormValue("__level") == "5"
	}
}

//go:generate go tool zenrpc

// New returns new zenrpc Server.
func New(dbo db.DB, logger embedlog.Logger, isDevel bool) *zenrpc.Server {
	rpc := zenrpc.NewServer(zenrpc.Options{
		ExposeSMD: true,
		AllowCORS: true,
	})

	commonRepo := db.NewCommonRepo(dbo)
	apprRepo := db.NewApprenticeRepo(dbo)

	rpc.Use(
		zm.WithDevel(isDevel),
		zm.WithHeaders(),
		zm.WithSentry(zm.DefaultServerName),
		zm.WithNoCancelContext(),
		zm.WithMetrics(zm.DefaultServerName),
		zm.WithTiming(isDevel, allowDebugFn()),
		zm.WithSQLLogger(dbo.DB, isDevel, allowDebugFn(), allowDebugFn()),
	)

	rpc.Use(
		zm.WithSLog(logger.Print, zm.DefaultServerName, nil),
		zm.WithErrorSLog(logger.Print, zm.DefaultServerName, nil),
	)

	rpc.Use(authMiddleware(&commonRepo, &apprRepo, logger))

	// services
	rpc.RegisterAll(map[string]zenrpc.Invoker{
		NSStage:     NewStageService(dbo, logger),
		NSCandidate: NewCandidateService(dbo, logger),
		NSDashboard: NewDashboardService(dbo, logger),
		NSAuth:      NewAuthService(dbo, logger),
	})

	return rpc
}
