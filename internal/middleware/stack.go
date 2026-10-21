package middleware

import (
	"log/slog"

	"github.com/gin-gonic/gin"
)

// Default returns the standard middleware stack, in the only order that works:
//
//  1. RequestID first, so every later step (and every error body) has an id.
//  2. Logger outside Recovery, so a panic still produces a 500 log line.
//  3. Recovery wrapping CORS and the handlers, so no panic escapes.
//  4. CORS last, so error responses, including recovered panics, carry CORS headers.
func Default(logger *slog.Logger, corsOrigins []string) []gin.HandlerFunc {
	return []gin.HandlerFunc{
		RequestID(),
		Logger(logger),
		Recovery(logger),
		CORS(corsOrigins),
	}
}
