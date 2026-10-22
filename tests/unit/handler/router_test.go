package handler_test

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/handler"
)

func TestRouterUnknownRouteReturnsJSONNotFound(t *testing.T) {
	engine := handler.NewRouter(handler.Deps{})
	engine.Use(func(c *gin.Context) { c.Header("X-Request-ID", "rid-1") })

	rec := do(engine, http.MethodGet, "/nope", "")

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	body := decodeError(t, rec)
	require.Equal(t, "not_found", body.Code)
	require.Equal(t, "rid-1", body.RequestID)
}

func TestRouterWrongMethodReturnsJSONMethodNotAllowed(t *testing.T) {
	engine := handler.NewRouter(handler.Deps{})

	rec := do(engine, http.MethodPost, "/healthz", "")

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	require.Equal(t, "method_not_allowed", decodeError(t, rec).Code)
}

type fakeReport struct{ name string }

func (f fakeReport) Register(rg *gin.RouterGroup) {
	rg.GET("/"+f.name, func(c *gin.Context) { c.String(http.StatusOK, f.name) })
}

func TestRouterMountsReportsUnderPrefix(t *testing.T) {
	engine := handler.NewRouter(handler.Deps{
		Reports: []handler.RouteRegistrar{fakeReport{"invoices"}, fakeReport{"payments"}},
	})

	tests := []struct {
		target     string
		wantStatus int
		wantBody   string
	}{
		{"/api/v1/reports/invoices", http.StatusOK, "invoices"},
		{"/api/v1/reports/payments", http.StatusOK, "payments"},
		{"/invoices", http.StatusNotFound, ""},
		{"/api/v1/invoices", http.StatusNotFound, ""},
		{"/api/v1/reports/no-such-report", http.StatusNotFound, ""},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			rec := do(engine, http.MethodGet, tt.target, "")
			require.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody != "" {
				require.Equal(t, tt.wantBody, rec.Body.String())
			}
		})
	}
}

func TestReportsPrefix(t *testing.T) {
	require.Equal(t, "/api/v1/reports", handler.ReportsPrefix)
}
