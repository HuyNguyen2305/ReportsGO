package middleware_test

import (
	"errors"
	"log/slog"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/handler"
	"reportsgo/internal/middleware"
	"reportsgo/internal/model"
	handlermocks "reportsgo/tests/mocks/handler"
)

func loggingEngine(logger *slog.Logger, pinger handler.Pinger) *gin.Engine {
	return newEngine([]gin.HandlerFunc{middleware.RequestID(), middleware.Logger(logger)}, pinger, func(rg *gin.RouterGroup) {
		rg.GET("/items/:id", func(c *gin.Context) { c.String(http.StatusOK, "item") })
		rg.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
		rg.GET("/bad", func(c *gin.Context) { c.Status(http.StatusBadRequest) })
		rg.GET("/missing", func(c *gin.Context) { handler.RespondError(c, logger, model.ErrNotFound) })
		rg.GET("/boom", func(c *gin.Context) { handler.RespondError(c, logger, errors.New("db exploded")) })
	})
}

func TestLoggerWritesOneLineWithAllFields(t *testing.T) {
	logger, logs := newCapture(slog.LevelInfo)
	rec := send(loggingEngine(logger, nil), request{target: reportsPath + "/items/42?secret=tok&from=2026-10-01"})

	records := logs.byMessage(t, "request")
	require.Len(t, records, 1)
	r := records[0]

	require.Equal(t, "INFO", r["level"])
	require.Equal(t, "GET", r["method"])
	require.Equal(t, reportsPath+"/items/:id", r["route"])
	require.EqualValues(t, 200, r["status"])
	require.EqualValues(t, len("item"), r["bytes"])
	require.Equal(t, "192.0.2.1", r["client_ip"])
	require.Equal(t, rec.Header().Get(middleware.RequestIDHeader), r["request_id"])
	require.Contains(t, r, "duration")
}

func TestLoggerNeverLogsQueryStringOrRawPath(t *testing.T) {
	logger, logs := newCapture(slog.LevelDebug)
	send(loggingEngine(logger, nil), request{target: reportsPath + "/items/12345?token=s3cr3t&customer=Jane"})

	raw := logs.raw()
	require.NotContains(t, raw, "s3cr3t")
	require.NotContains(t, raw, "Jane")
	require.NotContains(t, raw, "12345", "the raw path (with ids) must not be logged, only the route pattern")
}

func TestLoggerLevelsFollowStatus(t *testing.T) {
	tests := []struct {
		target    string
		wantLevel string
		wantCode  float64
	}{
		{reportsPath + "/ok", "INFO", 200},
		{reportsPath + "/bad", "WARN", 400},
		{reportsPath + "/missing", "WARN", 404},
		{reportsPath + "/boom", "ERROR", 500},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			logger, logs := newCapture(slog.LevelDebug)
			send(loggingEngine(logger, nil), request{target: tt.target})

			records := logs.byMessage(t, "request")
			require.Len(t, records, 1)
			require.Equal(t, tt.wantLevel, records[0]["level"])
			require.EqualValues(t, tt.wantCode, records[0]["status"])
		})
	}
}

func TestLoggerLabelsUnmatchedRoutes(t *testing.T) {
	tests := []struct {
		name   string
		method string
		target string
		status float64
	}{
		{"unknown path", http.MethodGet, "/wp-admin/setup.php?x=1", 404},
		{"wrong method", http.MethodPost, "/healthz", 405},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, logs := newCapture(slog.LevelInfo)
			send(loggingEngine(logger, nil), request{method: tt.method, target: tt.target})

			records := logs.byMessage(t, "request")
			require.Len(t, records, 1)
			require.Equal(t, "unmatched", records[0]["route"])
			require.EqualValues(t, tt.status, records[0]["status"])
			require.Equal(t, "WARN", records[0]["level"])
			require.NotContains(t, logs.raw(), "wp-admin")
		})
	}
}

func TestLoggerHealthProbesAreDebugUnlessFailing(t *testing.T) {
	t.Run("healthy probe hidden at info", func(t *testing.T) {
		logger, logs := newCapture(slog.LevelInfo)
		send(loggingEngine(logger, nil), request{target: "/healthz"})
		require.Empty(t, logs.records(t))
	})

	t.Run("healthy probe visible at debug", func(t *testing.T) {
		logger, logs := newCapture(slog.LevelDebug)
		send(loggingEngine(logger, nil), request{target: "/healthz"})

		records := logs.byMessage(t, "request")
		require.Len(t, records, 1)
		require.Equal(t, "DEBUG", records[0]["level"])
	})

	t.Run("failing readiness is always visible", func(t *testing.T) {
		pinger := handlermocks.NewMockPinger(t)
		pinger.EXPECT().Ping(mock.Anything).Return(errors.New("down")).Once()

		logger, logs := newCapture(slog.LevelInfo)
		send(loggingEngine(logger, pinger), request{target: "/readyz"})

		records := logs.byMessage(t, "request")
		require.Len(t, records, 1)
		require.Equal(t, "ERROR", records[0]["level"])
		require.EqualValues(t, 503, records[0]["status"])
	})
}

func TestLoggerIncludesErrorFromRespondErrorExceptOn500(t *testing.T) {
	t.Run("client error carries the error", func(t *testing.T) {
		logger, logs := newCapture(slog.LevelInfo)
		send(loggingEngine(logger, nil), request{target: reportsPath + "/missing"})

		records := logs.byMessage(t, "request")
		require.Len(t, records, 1)
		require.Contains(t, records[0]["error"], "not found")
	})

	t.Run("500 is logged once by RespondError, not repeated", func(t *testing.T) {
		logger, logs := newCapture(slog.LevelInfo)
		send(loggingEngine(logger, nil), request{target: reportsPath + "/boom"})

		access := logs.byMessage(t, "request")
		require.Len(t, access, 1)
		require.NotContains(t, access[0], "error")

		failed := logs.byMessage(t, "request failed")
		require.Len(t, failed, 1)
		require.Contains(t, failed[0]["error"], "db exploded")
	})
}
