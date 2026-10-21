package middleware_test

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/middleware"
)

func recoveryEngine(logger *slog.Logger) *gin.Engine {
	return newEngine([]gin.HandlerFunc{middleware.RequestID(), middleware.Recovery(logger)}, nil, func(rg *gin.RouterGroup) {
		rg.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
		rg.GET("/panic", func(*gin.Context) { panic("kaboom: connection string password=hunter2") })
		rg.GET("/panic-error", func(*gin.Context) { panic(errors.New("db exploded")) })
		rg.GET("/panic-after-write", func(c *gin.Context) {
			c.String(http.StatusOK, "partial")
			panic("late failure")
		})
		rg.GET("/abort", func(*gin.Context) { panic(http.ErrAbortHandler) })
		rg.GET("/broken-pipe", func(*gin.Context) {
			panic(&net.OpError{Op: "write", Err: &os.SyscallError{Syscall: "write", Err: syscall.EPIPE}})
		})
	})
}

func TestRecoveryAnswersPanicWithStandard500(t *testing.T) {
	logger, logs := newCapture(slog.LevelInfo)
	rec := send(recoveryEngine(logger), request{target: reportsPath + "/panic"})

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	body := decodeError(t, rec)
	require.Equal(t, "internal_error", body.Code)
	require.Equal(t, "internal server error", body.Message)
	require.Equal(t, rec.Header().Get(middleware.RequestIDHeader), body.RequestID)
	require.NotContains(t, rec.Body.String(), "kaboom")
	require.NotContains(t, rec.Body.String(), "hunter2")

	records := logs.byMessage(t, "panic recovered")
	require.Len(t, records, 1)
	require.Equal(t, "ERROR", records[0]["level"])
	require.Contains(t, records[0]["panic"], "kaboom")
	require.Contains(t, records[0]["stack"], "goroutine")
	require.Equal(t, body.RequestID, records[0]["request_id"])
}

func TestRecoveryHandlesErrorPanicValues(t *testing.T) {
	logger, logs := newCapture(slog.LevelInfo)
	rec := send(recoveryEngine(logger), request{target: reportsPath + "/panic-error"})

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Len(t, logs.byMessage(t, "panic recovered"), 1)
}

func TestRecoveryKeepsServingAfterAPanic(t *testing.T) {
	logger, _ := newCapture(slog.LevelInfo)
	engine := recoveryEngine(logger)

	require.Equal(t, http.StatusInternalServerError, send(engine, request{target: reportsPath + "/panic"}).Code)
	rec := send(engine, request{target: reportsPath + "/ok"})
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ok", rec.Body.String())
}

func TestRecoveryRepanicsErrAbortHandler(t *testing.T) {
	logger, logs := newCapture(slog.LevelInfo)
	engine := recoveryEngine(logger)

	require.PanicsWithValue(t, http.ErrAbortHandler, func() {
		send(engine, request{target: reportsPath + "/abort"})
	})
	require.Empty(t, logs.byMessage(t, "panic recovered"), "an intentional abort is not an error")
}

func TestRecoveryDoesNotCorruptAnAlreadyWrittenResponse(t *testing.T) {
	logger, logs := newCapture(slog.LevelInfo)
	rec := send(recoveryEngine(logger), request{target: reportsPath + "/panic-after-write"})

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "partial", rec.Body.String(), "no error envelope may be appended to a started response")
	require.Len(t, logs.byMessage(t, "panic recovered"), 1)
}

func TestRecoveryTreatsLostConnectionAsQuietEvent(t *testing.T) {
	logger, logs := newCapture(slog.LevelDebug)
	send(recoveryEngine(logger), request{target: reportsPath + "/broken-pipe"})

	require.Empty(t, logs.byMessage(t, "panic recovered"))
	require.NotContains(t, logs.raw(), "goroutine", "no stack trace for a client that went away")

	lost := logs.byMessage(t, "client connection lost")
	require.Len(t, lost, 1)
	require.Equal(t, "DEBUG", lost[0]["level"])
}
