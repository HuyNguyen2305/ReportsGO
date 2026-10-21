package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/dto"
)

// DefaultReadyTimeout bounds the database ping done by the readiness endpoint.
const DefaultReadyTimeout = 2 * time.Second

// Pinger checks that a dependency is reachable. *pgxpool.Pool satisfies it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler serves the liveness and readiness endpoints.
type HealthHandler struct {
	pinger  Pinger
	timeout time.Duration
	logger  *slog.Logger
}

// NewHealthHandler builds a HealthHandler; a zero timeout uses DefaultReadyTimeout.
func NewHealthHandler(pinger Pinger, timeout time.Duration, logger *slog.Logger) *HealthHandler {
	if timeout <= 0 {
		timeout = DefaultReadyTimeout
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &HealthHandler{pinger: pinger, timeout: timeout, logger: logger}
}

// Healthz reports that the process is serving requests. It never touches dependencies.
func (h *HealthHandler) Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, dto.HealthResponse{Status: "ok"})
}

// Readyz reports whether the dependencies needed to serve reports are reachable.
// The underlying error is logged but never sent to the client.
func (h *HealthHandler) Readyz(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), h.timeout)
	defer cancel()

	if err := h.pinger.Ping(ctx); err != nil {
		h.logger.WarnContext(c.Request.Context(), "readiness check failed", "check", "database", "error", err)
		c.JSON(http.StatusServiceUnavailable, dto.ReadyResponse{
			Status: "unavailable",
			Checks: map[string]string{"database": "down"},
		})
		return
	}
	c.JSON(http.StatusOK, dto.ReadyResponse{
		Status: "ready",
		Checks: map[string]string{"database": "ok"},
	})
}
