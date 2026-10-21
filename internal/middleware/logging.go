package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// unmatchedRoute labels requests that matched no route (404/405), so scanners cannot create
// unbounded distinct values in the route field.
const unmatchedRoute = "unmatched"

// Logger writes one structured line per request after it completes. It logs the route pattern
// rather than the raw path and never logs the query string, which may carry filters or tokens.
// Probe endpoints are logged at debug level unless they fail.
func Logger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = unmatchedRoute
		}
		status := c.Writer.Status()

		attrs := []slog.Attr{
			slog.String("method", c.Request.Method),
			slog.String("route", route),
			slog.Int("status", status),
			slog.Duration("duration", time.Since(start)),
			slog.Int("bytes", max(c.Writer.Size(), 0)),
			slog.String("client_ip", c.ClientIP()),
			slog.String("request_id", c.Writer.Header().Get(RequestIDHeader)),
		}
		// handler.RespondError already logs the full error for 500s; don't repeat it here.
		if last := c.Errors.Last(); last != nil && status != http.StatusInternalServerError {
			attrs = append(attrs, slog.String("error", last.Err.Error()))
		}

		logger.LogAttrs(c.Request.Context(), levelFor(route, status), "request", attrs...)
	}
}

func levelFor(route string, status int) slog.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	case status >= http.StatusBadRequest:
		return slog.LevelWarn
	case route == "/healthz" || route == "/readyz":
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}
