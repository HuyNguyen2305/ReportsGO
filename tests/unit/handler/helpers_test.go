package handler_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/dto"
	"reportsgo/internal/handler"
)

func init() { gin.SetMode(gin.TestMode) }

// registrarFunc adapts a function to handler.RouteRegistrar.
type registrarFunc func(rg *gin.RouterGroup)

func (f registrarFunc) Register(rg *gin.RouterGroup) { f(rg) }

// newLogger returns a text logger writing to the returned buffer, at debug level.
func newLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

// do performs a request against the engine and returns the recorded response.
func do(engine http.Handler, method, target, body string) *httptest.ResponseRecorder {
	var r io.Reader
	if body != "" {
		r = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, target, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
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

// errorRouter returns an engine with GET /api/v1/reports/boom answering with RespondError(err).
func errorRouter(logger *slog.Logger, requestID string, err error) *gin.Engine {
	return handler.NewRouter(handler.Deps{
		Logger: logger,
		Reports: []handler.RouteRegistrar{registrarFunc(func(rg *gin.RouterGroup) {
			rg.GET("/boom", func(c *gin.Context) {
				if requestID != "" {
					c.Header("X-Request-ID", requestID)
				}
				handler.RespondError(c, logger, err)
			})
		})},
	})
}
