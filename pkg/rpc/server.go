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

	// services
	rpc.RegisterAll(map[string]zenrpc.Invoker{
		NSStage:     NewStageService(dbo, logger),
		NSCandidate: NewCandidateService(dbo, logger),
		NSDashboard: NewDashboardService(dbo, logger),
	})

	return rpc
}
