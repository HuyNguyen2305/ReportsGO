package handler_test

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/handler"
)

func TestRouterAppliesDepsMiddlewareToEveryRoute(t *testing.T) {
	tag := func(c *gin.Context) {
		c.Header("X-Tagged", "yes")
		c.Next()
	}
	engine := handler.NewRouter(handler.Deps{
		Middleware: []gin.HandlerFunc{tag},
		Reports:    []handler.RouteRegistrar{fakeReport{"invoices"}},
	})

	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
	}{
		{"liveness", http.MethodGet, "/healthz", http.StatusOK},
		{"report route", http.MethodGet, "/api/v1/reports/invoices", http.StatusOK},
		{"unknown route", http.MethodGet, "/nope", http.StatusNotFound},
		{"wrong method", http.MethodPost, "/healthz", http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(engine, tt.method, tt.target, "")
			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, "yes", rec.Header().Get("X-Tagged"))
		})
	}
}
