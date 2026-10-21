package middleware_test

import (
	"log/slog"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/middleware"
)

func TestDefaultStackHandlesAPanicEndToEnd(t *testing.T) {
	logger, logs := newCapture(slog.LevelInfo)
	engine := newEngine(middleware.Default(logger, []string{"https://app.example.com"}), nil, func(rg *gin.RouterGroup) {
		rg.GET("/panic", func(*gin.Context) { panic("kaboom") })
		rg.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	})

	rec := send(engine, request{
		target:  reportsPath + "/panic",
		headers: map[string]string{"Origin": "https://app.example.com", middleware.RequestIDHeader: "rid-9"},
	})

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "rid-9", rec.Header().Get(middleware.RequestIDHeader))
	require.Equal(t, "rid-9", decodeError(t, rec).RequestID)
	require.Equal(t, "https://app.example.com", rec.Header().Get("Access-Control-Allow-Origin"),
		"a recovered panic must still carry CORS headers or the browser hides the 500")

	require.Len(t, logs.byMessage(t, "panic recovered"), 1)
	access := logs.byMessage(t, "request")
	require.Len(t, access, 1, "Logger sits outside Recovery, so the panicking request is still logged")
	require.EqualValues(t, 500, access[0]["status"])
	require.Equal(t, "ERROR", access[0]["level"])
	require.Equal(t, "rid-9", access[0]["request_id"])

	require.Equal(t, http.StatusOK, send(engine, request{target: reportsPath + "/ok"}).Code)
}

func TestDefaultStackAppliesToBuiltInRoutes(t *testing.T) {
	logger, _ := newCapture(slog.LevelInfo)
	engine := newEngine(middleware.Default(logger, nil), nil, nil)

	rec := send(engine, request{target: "/healthz"})

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotEmpty(t, rec.Header().Get(middleware.RequestIDHeader), "middleware must wrap routes NewRouter registers itself")
}
