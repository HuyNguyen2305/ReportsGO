package middleware_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/middleware"
)

func requestIDEngine() *gin.Engine {
	return newEngine([]gin.HandlerFunc{middleware.RequestID()}, nil, func(rg *gin.RouterGroup) {
		rg.GET("/id", func(c *gin.Context) {
			c.String(http.StatusOK, middleware.RequestIDFromContext(c.Request.Context()))
		})
	})
}

func TestRequestIDGeneratesUUIDv7WhenAbsent(t *testing.T) {
	rec := send(requestIDEngine(), request{target: reportsPath + "/id"})

	id := rec.Header().Get(middleware.RequestIDHeader)
	parsed, err := uuid.Parse(id)
	require.NoError(t, err, "generated id must be a UUID, got %q", id)
	require.Equal(t, uuid.Version(7), parsed.Version())
}

func TestRequestIDIsUniquePerRequest(t *testing.T) {
	engine := requestIDEngine()
	first := send(engine, request{target: reportsPath + "/id"}).Header().Get(middleware.RequestIDHeader)
	second := send(engine, request{target: reportsPath + "/id"}).Header().Get(middleware.RequestIDHeader)
	require.NotEqual(t, first, second)
}

func TestRequestIDIncomingValue(t *testing.T) {
	tests := []struct {
		name     string
		incoming string
		reused   bool
	}{
		{"simple", "abc123", true},
		{"allowed punctuation", "trace-1_2.3", true},
		{"uuid from a proxy", "0190f2a4-7b1c-7d3e-8f10-aabbccddeeff", true},
		{"exactly 64 chars", strings.Repeat("a", 64), true},
		{"65 chars", strings.Repeat("a", 65), false},
		{"space", "bad id", false},
		{"slash", "a/b", false},
		{"html", "<script>alert(1)</script>", false},
		{"log injection", "id\nlevel=ERROR", false},
		{"non-ascii", "idé", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := send(requestIDEngine(), request{
				target:  reportsPath + "/id",
				headers: map[string]string{middleware.RequestIDHeader: tt.incoming},
			})

			got := rec.Header().Get(middleware.RequestIDHeader)
			if tt.reused {
				require.Equal(t, tt.incoming, got)
			} else {
				require.NotEqual(t, tt.incoming, got)
				_, err := uuid.Parse(got)
				require.NoError(t, err)
			}
			require.Equal(t, got, rec.Body.String(), "request context must carry the same id as the response header")
		})
	}
}

func TestRequestIDPresentOnUnknownRoute(t *testing.T) {
	rec := send(requestIDEngine(), request{target: "/nope"})

	require.Equal(t, http.StatusNotFound, rec.Code)
	id := rec.Header().Get(middleware.RequestIDHeader)
	require.NotEmpty(t, id)
	require.Equal(t, id, decodeError(t, rec).RequestID, "error body must carry the same request id")
}

func TestRequestIDFromContextWithoutMiddleware(t *testing.T) {
	require.Empty(t, middleware.RequestIDFromContext(context.Background()))
}
