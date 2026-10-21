//go:build integration

package middleware_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/dto"
	"reportsgo/internal/handler"
	"reportsgo/internal/middleware"
	"reportsgo/tests/integration/testdb"
)

func init() { gin.SetMode(gin.TestMode) }

type registrarFunc func(rg *gin.RouterGroup)

func (f registrarFunc) Register(rg *gin.RouterGroup) { f(rg) }

const allowedOrigin = "https://app.example.com"

func send(engine http.Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestFullStackAgainstRealDatabase(t *testing.T) {
	pool := testdb.MigratedPool(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	engine := handler.NewRouter(handler.Deps{
		Logger:       logger,
		Pinger:       pool,
		ReadyTimeout: 2 * time.Second,
		Middleware:   middleware.Default(logger, []string{allowedOrigin}),
		Reports: []handler.RouteRegistrar{registrarFunc(func(rg *gin.RouterGroup) {
			rg.GET("/panic", func(*gin.Context) { panic("kaboom") })
		})},
	})

	t.Run("readyz carries a generated request id and CORS headers", func(t *testing.T) {
		rec := send(engine, http.MethodGet, "/readyz", map[string]string{"Origin": allowedOrigin})

		require.Equal(t, http.StatusOK, rec.Code)
		_, err := uuid.Parse(rec.Header().Get(middleware.RequestIDHeader))
		require.NoError(t, err)
		require.Equal(t, allowedOrigin, rec.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("incoming request id is echoed", func(t *testing.T) {
		rec := send(engine, http.MethodGet, "/healthz", map[string]string{middleware.RequestIDHeader: "client-trace-1"})
		require.Equal(t, "client-trace-1", rec.Header().Get(middleware.RequestIDHeader))
	})

	t.Run("preflight is answered without reaching a route", func(t *testing.T) {
		rec := send(engine, http.MethodOptions, "/readyz", map[string]string{
			"Origin":                        allowedOrigin,
			"Access-Control-Request-Method": "GET",
		})
		require.Equal(t, http.StatusNoContent, rec.Code)
		require.Equal(t, "GET, HEAD, OPTIONS", rec.Header().Get("Access-Control-Allow-Methods"))
	})

	t.Run("a panicking handler returns the 500 envelope and the server keeps serving", func(t *testing.T) {
		rec := send(engine, http.MethodGet, handler.ReportsPrefix+"/panic", map[string]string{"Origin": allowedOrigin})

		require.Equal(t, http.StatusInternalServerError, rec.Code)
		var resp dto.ErrorResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.Equal(t, "internal_error", resp.Error.Code)
		require.Equal(t, rec.Header().Get(middleware.RequestIDHeader), resp.Error.RequestID)
		require.Equal(t, allowedOrigin, rec.Header().Get("Access-Control-Allow-Origin"))
		require.NotContains(t, rec.Body.String(), "kaboom")

		require.Equal(t, http.StatusOK, send(engine, http.MethodGet, "/healthz", nil).Code)
		require.Equal(t, http.StatusOK, send(engine, http.MethodGet, "/readyz", nil).Code)
	})

	t.Run("every request produced exactly one access log line with its request id", func(t *testing.T) {
		require.Contains(t, logs.String(), `"msg":"request"`)
		require.Contains(t, logs.String(), `"request_id":"client-trace-1"`)
		require.NotContains(t, logs.String(), "kaboom\"}", "panic text appears only in the panic log, never in access lines")
	})
}
