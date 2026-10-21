package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/handler"
)

// Recovery turns a panic in a handler into a logged error and a standard 500 response, so one bad
// request cannot take the process down. The panic value and stack go to the log, never to the client.
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			p := recover()
			if p == nil {
				return
			}
			err, _ := p.(error)

			// net/http uses this panic to abort a response on purpose; it must keep unwinding.
			if err != nil && errors.Is(err, http.ErrAbortHandler) {
				panic(p)
			}

			requestID := c.Writer.Header().Get(RequestIDHeader)

			// The client is gone; there is nobody to answer and no bug to report.
			if isConnectionLost(err) {
				logger.DebugContext(c.Request.Context(), "client connection lost",
					"error", err, "request_id", requestID)
				c.Abort()
				return
			}

			logger.ErrorContext(c.Request.Context(), "panic recovered",
				"panic", p,
				"stack", string(debug.Stack()),
				"request_id", requestID,
				"method", c.Request.Method,
				"route", c.FullPath(),
			)

			if c.Writer.Written() {
				c.Abort() // headers are already out; appending a body would corrupt the response
				return
			}
			handler.WriteError(c, http.StatusInternalServerError, "internal_error", "internal server error")
		}()

		c.Next()
	}
}

func isConnectionLost(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "broken pipe") || strings.Contains(msg, "connection reset by peer")
}
