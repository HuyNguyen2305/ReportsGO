package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ReportsPrefix is the URL prefix every report is mounted under.
const ReportsPrefix = "/api/v1/reports"

// RouteRegistrar mounts one report's routes on the shared reports group.
type RouteRegistrar interface {
	Register(rg *gin.RouterGroup)
}

// Deps are the collaborators NewRouter needs.
type Deps struct {
	Logger       *slog.Logger
	Pinger       Pinger
	ReadyTimeout time.Duration
	Reports      []RouteRegistrar
	// Middleware is installed before any route is registered; Gin's Use does not affect routes that
	// already exist, so it must be passed here rather than added to the returned engine.
	Middleware []gin.HandlerFunc
}

// NewRouter builds the Gin engine: health endpoints at the root and each report under ReportsPrefix.
// Gin's mode is set by the caller.
func NewRouter(deps Deps) *gin.Engine {
	registerValidator()

	engine := gin.New()
	engine.Use(deps.Middleware...)
	engine.HandleMethodNotAllowed = true
	engine.NoRoute(func(c *gin.Context) {
		WriteError(c, http.StatusNotFound, "not_found", "route not found")
	})
	engine.NoMethod(func(c *gin.Context) {
		WriteError(c, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})

	health := NewHealthHandler(deps.Pinger, deps.ReadyTimeout, deps.Logger)
	engine.GET("/healthz", health.Healthz)
	engine.GET("/readyz", health.Readyz)

	reports := engine.Group(ReportsPrefix)
	for _, r := range deps.Reports {
		r.Register(reports)
	}
	return engine
}
