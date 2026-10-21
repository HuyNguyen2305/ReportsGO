//go:build integration

package health_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/dto"
	"reportsgo/internal/handler"
	"reportsgo/tests/integration/testdb"
)

func init() { gin.SetMode(gin.TestMode) }

func get(engine http.Handler, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestHealthEndpointsWithRealDatabase(t *testing.T) {
	pool := testdb.MigratedPool(t)
	engine := handler.NewRouter(handler.Deps{Pinger: pool, ReadyTimeout: 2 * time.Second})

	t.Run("ready with a live pool", func(t *testing.T) {
		rec := get(engine, "/readyz")
		require.Equal(t, http.StatusOK, rec.Code)

		var got dto.ReadyResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Equal(t, "ready", got.Status)
		require.Equal(t, "ok", got.Checks["database"])
	})

	t.Run("not ready once the pool is closed, but still alive", func(t *testing.T) {
		pool.Close()

		rec := get(engine, "/readyz")
		require.Equal(t, http.StatusServiceUnavailable, rec.Code)

		var got dto.ReadyResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Equal(t, "unavailable", got.Status)
		require.Equal(t, "down", got.Checks["database"])

		require.Equal(t, http.StatusOK, get(engine, "/healthz").Code)
	})
}
