package middleware_test

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/middleware"
)

func corsEngine(origins []string) *gin.Engine {
	return newEngine([]gin.HandlerFunc{middleware.RequestID(), middleware.CORS(origins)}, nil, func(rg *gin.RouterGroup) {
		rg.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	})
}

var allowedOrigins = []string{"https://app.example.com", "http://localhost:3000/"}

func TestCORSSimpleRequests(t *testing.T) {
	tests := []struct {
		name        string
		origin      string // "" means no Origin header
		wantAllowed bool
		wantVary    bool
	}{
		{"no origin header", "", false, true},
		{"allowed origin", "https://app.example.com", true, true},
		{"allowed with different case", "HTTPS://APP.EXAMPLE.COM", true, true},
		{"allowed, configured with trailing slash", "http://localhost:3000", true, true},
		{"foreign origin", "https://evil.example", false, true},
		{"null origin", "null", false, true},
		{"subdomain of an allowed origin", "https://sub.app.example.com", false, true},
		{"allowed host as a prefix of another", "https://app.example.com.evil.io", false, true},
		{"wrong scheme", "http://app.example.com", false, true},
		{"wrong port", "http://localhost:3001", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := map[string]string{}
			if tt.origin != "" {
				headers["Origin"] = tt.origin
			}
			rec := send(corsEngine(allowedOrigins), request{target: reportsPath + "/ok", headers: headers})

			require.Equal(t, http.StatusOK, rec.Code, "CORS is enforced by browsers; the handler still runs")
			h := rec.Header()

			if tt.wantAllowed {
				require.Equal(t, tt.origin, h.Get("Access-Control-Allow-Origin"))
				require.Equal(t, middleware.RequestIDHeader, h.Get("Access-Control-Expose-Headers"))
			} else {
				require.Empty(t, h.Get("Access-Control-Allow-Origin"))
				require.Empty(t, h.Get("Access-Control-Expose-Headers"))
			}
			if tt.wantVary {
				require.Contains(t, h.Values("Vary"), "Origin")
			} else {
				require.Empty(t, h.Values("Vary"))
			}
			require.NotEqual(t, "*", h.Get("Access-Control-Allow-Origin"))
			require.Empty(t, h.Get("Access-Control-Allow-Credentials"), "credentials must never be allowed")
		})
	}
}

func TestCORSPreflight(t *testing.T) {
	tests := []struct {
		name        string
		target      string
		origin      string
		method      string // Access-Control-Request-Method
		wantAllowed bool
	}{
		{"allowed origin, GET", reportsPath + "/ok", "https://app.example.com", "GET", true},
		{"allowed origin, HEAD (not served)", reportsPath + "/ok", "https://app.example.com", "HEAD", false},
		{"allowed origin, unknown path", "/does/not/exist", "https://app.example.com", "GET", true},
		{"allowed origin, POST", reportsPath + "/ok", "https://app.example.com", "POST", false},
		{"allowed origin, DELETE", reportsPath + "/ok", "https://app.example.com", "DELETE", false},
		{"foreign origin, GET", reportsPath + "/ok", "https://evil.example", "GET", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := send(corsEngine(allowedOrigins), request{
				method: http.MethodOptions,
				target: tt.target,
				headers: map[string]string{
					"Origin":                         tt.origin,
					"Access-Control-Request-Method":  tt.method,
					"Access-Control-Request-Headers": "content-type",
				},
			})

			require.Equal(t, http.StatusNoContent, rec.Code)
			require.Empty(t, rec.Body.String())
			h := rec.Header()
			require.Contains(t, h.Values("Vary"), "Origin")

			if tt.wantAllowed {
				require.Equal(t, tt.origin, h.Get("Access-Control-Allow-Origin"))
				require.Equal(t, "GET, OPTIONS", h.Get("Access-Control-Allow-Methods"))
				require.Equal(t, "Accept, Content-Type, X-Request-ID", h.Get("Access-Control-Allow-Headers"))
				require.Equal(t, "600", h.Get("Access-Control-Max-Age"))
			} else {
				require.Empty(t, h.Get("Access-Control-Allow-Origin"))
				require.Empty(t, h.Get("Access-Control-Allow-Methods"))
				require.Empty(t, h.Get("Access-Control-Allow-Headers"))
			}
			require.Empty(t, h.Get("Access-Control-Allow-Credentials"))
		})
	}
}

func TestCORSOptionsWithoutRequestMethodIsNotAPreflight(t *testing.T) {
	rec := send(corsEngine(allowedOrigins), request{
		method:  http.MethodOptions,
		target:  reportsPath + "/ok",
		headers: map[string]string{"Origin": "https://app.example.com"},
	})

	// No OPTIONS route exists, so a plain OPTIONS reaches the router's 405, with the allow header attached.
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	require.Equal(t, "https://app.example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Methods"))
}

func TestCORSErrorResponsesCarryHeaders(t *testing.T) {
	rec := send(corsEngine(allowedOrigins), request{
		target:  "/nope",
		headers: map[string]string{"Origin": "https://app.example.com"},
	})

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, "https://app.example.com", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORSDisabledWhenNoOriginsConfigured(t *testing.T) {
	for name, origins := range map[string][]string{"nil": nil, "empty": {}, "blank entries": {"", "  "}} {
		t.Run(name, func(t *testing.T) {
			rec := send(corsEngine(origins), request{
				target:  reportsPath + "/ok",
				headers: map[string]string{"Origin": "https://app.example.com"},
			})
			require.Equal(t, http.StatusOK, rec.Code)
			require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
			require.Empty(t, rec.Header().Values("Vary"))

			pre := send(corsEngine(origins), request{
				method: http.MethodOptions,
				target: reportsPath + "/ok",
				headers: map[string]string{
					"Origin":                        "https://app.example.com",
					"Access-Control-Request-Method": "GET",
				},
			})
			require.Equal(t, http.StatusMethodNotAllowed, pre.Code, "with CORS off, a preflight is just an unsupported OPTIONS")
			require.Empty(t, pre.Header().Get("Access-Control-Allow-Origin"))
		})
	}
}
