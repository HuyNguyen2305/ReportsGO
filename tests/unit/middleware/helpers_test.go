package middleware_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/dto"
	"reportsgo/internal/handler"
)

func init() { gin.SetMode(gin.TestMode) }

type registrarFunc func(rg *gin.RouterGroup)

func (f registrarFunc) Register(rg *gin.RouterGroup) { f(rg) }

// logCapture collects structured log records.
type logCapture struct {
	buf bytes.Buffer
}

func newCapture(level slog.Level) (*slog.Logger, *logCapture) {
	c := &logCapture{}
	return slog.New(slog.NewJSONHandler(&c.buf, &slog.HandlerOptions{Level: level})), c
}

// records parses every JSON log line.
func (c *logCapture) records(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(c.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), "line: %s", line)
		out = append(out, rec)
	}
	return out
}

// byMessage returns the records whose msg equals message.
func (c *logCapture) byMessage(t *testing.T, message string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, rec := range c.records(t) {
		if rec["msg"] == message {
			out = append(out, rec)
		}
	}
	return out
}

func (c *logCapture) raw() string { return c.buf.String() }

// newEngine builds the real router with the given middleware and report routes.
func newEngine(mw []gin.HandlerFunc, pinger handler.Pinger, register func(rg *gin.RouterGroup)) *gin.Engine {
	deps := handler.Deps{Middleware: mw, Pinger: pinger, Logger: slog.New(slog.DiscardHandler)}
	if register != nil {
		deps.Reports = []handler.RouteRegistrar{registrarFunc(register)}
	}
	return handler.NewRouter(deps)
}

type request struct {
	method  string
	target  string
	headers map[string]string
}

func send(engine http.Handler, r request) *httptest.ResponseRecorder {
	method := r.method
	if method == "" {
		method = http.MethodGet
	}
	req := httptest.NewRequest(method, r.target, nil)
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) dto.ErrorBody {
	t.Helper()
	var resp dto.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp), "body: %s", rec.Body.String())
	return resp.Error
}

const reportsPath = "/api/v1/reports"
