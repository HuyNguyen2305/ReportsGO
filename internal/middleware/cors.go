package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// The API is read-only, so cross-origin requests are limited to safe methods and no credentials.
const (
	corsAllowMethods = "GET, OPTIONS"
	corsAllowHeaders = "Accept, Content-Type, X-Request-ID"
	corsMaxAge       = "600"
)

// CORS allows cross-origin browser requests from an exact-match allowlist of origins. An empty list
// means same-origin only and the middleware does nothing. It never answers with "*" and never allows
// credentials. Every response varies on Origin, including those to requests without one, so a shared cache
// cannot serve a response that lacks CORS headers to a cross-origin caller. Browsers enforce CORS, so requests from other origins still reach the handler; they just
// receive no Access-Control-Allow-* headers.
func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if n := normalizeOrigin(o); n != "" {
			allowed[n] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return func(c *gin.Context) { c.Next() }
	}

	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Add("Vary", "Origin")

		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next() // not a cross-origin browser request
			return
		}

		_, originAllowed := allowed[normalizeOrigin(origin)]

		if isPreflight(c.Request) {
			h.Add("Vary", "Access-Control-Request-Method")
			h.Add("Vary", "Access-Control-Request-Headers")
			if originAllowed && safeMethod(c.GetHeader("Access-Control-Request-Method")) {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Methods", corsAllowMethods)
				h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
				h.Set("Access-Control-Max-Age", corsMaxAge)
			}
			// A refused preflight gets no allow headers, which makes the browser block the request.
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		if originAllowed {
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Expose-Headers", RequestIDHeader)
		}
		c.Next()
	}
}

func isPreflight(r *http.Request) bool {
	return r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodOptions
}

// normalizeOrigin makes origin comparison case-insensitive and ignores a trailing slash.
func normalizeOrigin(o string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(o), "/"))
}
