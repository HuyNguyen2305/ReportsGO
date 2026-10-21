// Package middleware holds the cross-cutting HTTP middleware: request ids, logging, panic recovery and CORS.
package middleware

import (
	"context"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestIDHeader carries the request id on both the request and the response.
const RequestIDHeader = "X-Request-ID"

type requestIDKey struct{}

// validRequestID limits what is accepted from clients, so a request id is always safe to log and echo.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// RequestID makes sure every request has an id. A well-formed incoming X-Request-ID is reused
// (so ids survive a proxy hop); anything else is replaced by a new UUIDv7. The id is set on the
// response before the handler runs, and is available through RequestIDFromContext.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if !validRequestID.MatchString(id) {
			id = newRequestID()
		}

		c.Header(RequestIDHeader, id)
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), requestIDKey{}, id))
		c.Next()
	}
}

// RequestIDFromContext returns the request id stored by RequestID, or "" when there is none.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func newRequestID() string {
	id, err := uuid.NewV7()
	if err != nil {
		// Only happens if the system random source fails; v4 is an acceptable fallback for an id.
		return uuid.NewString()
	}
	return id.String()
}
