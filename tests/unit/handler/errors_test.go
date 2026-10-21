package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/dto"
	"reportsgo/internal/handler"
	"reportsgo/internal/model"
)

func TestRespondErrorMapsErrorsToStatusAndCode(t *testing.T) {
	var syntaxErr, typeErr error
	var target struct{ N int }
	syntaxErr = json.Unmarshal([]byte(`{"n":`), &target)
	typeErr = json.Unmarshal([]byte(`{"N":"text"}`), &target)
	_, numErr := strconv.Atoi("abc")
	_, timeErr := time.Parse("2006-01-02", "not-a-date")

	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantCode    string
		wantDetails []dto.ErrorDetail
	}{
		{"not found", model.ErrNotFound, http.StatusNotFound, "not_found", nil},
		{"wrapped not found", fmt.Errorf("get invoice 5: %w", model.ErrNotFound), http.StatusNotFound, "not_found", nil},
		{"joined not found", errors.Join(errors.New("noise"), model.ErrNotFound), http.StatusNotFound, "not_found", nil},
		{"conflict", fmt.Errorf("create: %w", model.ErrConflict), http.StatusConflict, "conflict", nil},
		{"bare invalid input", model.ErrInvalidInput, http.StatusUnprocessableEntity, "invalid_input", nil},
		{
			"invalid input with fields",
			fmt.Errorf("list: %w", model.InvalidInput(model.FieldError{Field: "to", Message: "must not be before from"})),
			http.StatusUnprocessableEntity, "invalid_input",
			[]dto.ErrorDetail{{Field: "to", Message: "must not be before from"}},
		},
		{"deadline exceeded", fmt.Errorf("query: %w", context.DeadlineExceeded), http.StatusGatewayTimeout, "timeout", nil},
		{"canceled", fmt.Errorf("query: %w", context.Canceled), 499, "client_closed_request", nil},
		{"json syntax error", syntaxErr, http.StatusBadRequest, "invalid_request", nil},
		{
			"json type error", typeErr, http.StatusBadRequest, "invalid_request",
			[]dto.ErrorDetail{{Field: "N", Message: "must be of type int"}},
		},
		{"empty body", io.EOF, http.StatusBadRequest, "invalid_request", nil},
		{"truncated body", io.ErrUnexpectedEOF, http.StatusBadRequest, "invalid_request", nil},
		{"number parse error", numErr, http.StatusBadRequest, "invalid_request", nil},
		{"time parse error", timeErr, http.StatusBadRequest, "invalid_request", nil},
		{"unknown error", errors.New("pq: connection refused"), http.StatusInternalServerError, "internal_error", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, _ := newLogger()
			rec := do(errorRouter(logger, "", tt.err), http.MethodGet, "/api/v1/reports/boom", "")

			require.Equal(t, tt.wantStatus, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, tt.wantCode, body.Code)
			require.NotEmpty(t, body.Message)
			require.Equal(t, tt.wantDetails, body.Details)
		})
	}
}

func TestRespondErrorHidesInternalErrorsButLogsThem(t *testing.T) {
	logger, logs := newLogger()
	secret := errors.New("pq: password authentication failed for user postgres")

	rec := do(errorRouter(logger, "req-42", secret), http.MethodGet, "/api/v1/reports/boom", "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), "password")
	require.Equal(t, "internal server error", decodeError(t, rec).Message)

	require.Contains(t, logs.String(), "level=ERROR")
	require.Contains(t, logs.String(), "password authentication failed")
	require.Contains(t, logs.String(), "request_id=req-42")
}

func TestRespondErrorDoesNotLogClientErrors(t *testing.T) {
	logger, logs := newLogger()
	do(errorRouter(logger, "", model.ErrNotFound), http.MethodGet, "/api/v1/reports/boom", "")
	require.Empty(t, logs.String())
}

func TestRespondErrorLogsCanceledAtDebugOnly(t *testing.T) {
	logger, logs := newLogger()
	do(errorRouter(logger, "", context.Canceled), http.MethodGet, "/api/v1/reports/boom", "")
	require.Contains(t, logs.String(), "level=DEBUG")
	require.NotContains(t, logs.String(), "level=ERROR")
}

func TestRespondErrorIncludesRequestID(t *testing.T) {
	tests := []struct {
		name      string
		requestID string
	}{
		{"echoes the header", "abc-123"},
		{"omitted when absent", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, _ := newLogger()
			rec := do(errorRouter(logger, tt.requestID, model.ErrNotFound), http.MethodGet, "/api/v1/reports/boom", "")
			require.Equal(t, tt.requestID, decodeError(t, rec).RequestID)
			if tt.requestID == "" {
				require.NotContains(t, rec.Body.String(), "request_id")
			}
		})
	}
}

func TestRespondErrorAttachesErrorToGinContext(t *testing.T) {
	logger, _ := newLogger()
	var attached []*gin.Error
	engine := handler.NewRouter(handler.Deps{
		Logger: logger,
		Reports: []handler.RouteRegistrar{registrarFunc(func(rg *gin.RouterGroup) {
			rg.GET("/boom", func(c *gin.Context) {
				handler.RespondError(c, logger, model.ErrConflict)
				attached = c.Errors
			})
		})},
	})

	do(engine, http.MethodGet, "/api/v1/reports/boom", "")
	require.Len(t, attached, 1)
	require.ErrorIs(t, attached[0].Err, model.ErrConflict)
}

// --- validation errors from real Gin binding ---------------------------------------------------

type createRequest struct {
	Name  string   `json:"name" binding:"required,min=3"`
	Age   int      `json:"age" binding:"gte=0,lte=150"`
	Kind  string   `json:"kind" binding:"oneof=a b"`
	Tags  []string `json:"tags" binding:"max=2"`
	Email string   `json:"email" binding:"omitempty,email"`
}

type listQuery struct {
	Limit int `form:"limit" binding:"max=100"`
}

func bindingRouter() *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger: logger,
		Reports: []handler.RouteRegistrar{registrarFunc(func(rg *gin.RouterGroup) {
			rg.POST("/create", func(c *gin.Context) {
				var req createRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					handler.RespondError(c, logger, err)
					return
				}
				c.Status(http.StatusNoContent)
			})
			rg.GET("/list", func(c *gin.Context) {
				var q listQuery
				if err := c.ShouldBindQuery(&q); err != nil {
					handler.RespondError(c, logger, err)
					return
				}
				c.Status(http.StatusNoContent)
			})
		})},
	})
}

func TestValidationErrorsUseJSONFieldNamesAndReadableMessages(t *testing.T) {
	rec := do(bindingRouter(), http.MethodPost, "/api/v1/reports/create",
		`{"age":200,"kind":"z","tags":["a","b","c"],"email":"nope"}`)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeError(t, rec)
	require.Equal(t, "validation_failed", body.Code)

	byField := make(map[string]dto.ErrorDetail)
	for _, d := range body.Details {
		byField[d.Field] = d
	}
	require.Equal(t, dto.ErrorDetail{Field: "name", Rule: "required", Message: "is required"}, byField["name"])
	require.Equal(t, dto.ErrorDetail{Field: "age", Rule: "lte", Message: "must be less than or equal to 150"}, byField["age"])
	require.Equal(t, dto.ErrorDetail{Field: "kind", Rule: "oneof", Message: "must be one of: a, b"}, byField["kind"])
	require.Equal(t, dto.ErrorDetail{Field: "tags", Rule: "max", Message: "must contain at most 2 items"}, byField["tags"])
	require.Equal(t, dto.ErrorDetail{Field: "email", Rule: "email", Message: "must be a valid email address"}, byField["email"])
	require.Len(t, byField, 5)
}

func TestValidationMinMessageDependsOnKind(t *testing.T) {
	rec := do(bindingRouter(), http.MethodPost, "/api/v1/reports/create", `{"name":"ab","kind":"a"}`)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeError(t, rec)
	require.Equal(t, []dto.ErrorDetail{
		{Field: "name", Rule: "min", Message: "must be at least 3 characters long"},
	}, body.Details)
}

func TestBindingErrorsBecomeBadRequests(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"valid body", http.MethodPost, "/api/v1/reports/create", `{"name":"abc","kind":"a"}`, http.StatusNoContent, ""},
		{"malformed json", http.MethodPost, "/api/v1/reports/create", `{"name":`, http.StatusBadRequest, "invalid_request"},
		{"wrong json type", http.MethodPost, "/api/v1/reports/create", `{"name":123}`, http.StatusBadRequest, "invalid_request"},
		{"valid query", http.MethodGet, "/api/v1/reports/list?limit=10", "", http.StatusNoContent, ""},
		{"non-numeric query", http.MethodGet, "/api/v1/reports/list?limit=abc", "", http.StatusBadRequest, "invalid_request"},
		{"query rule violated", http.MethodGet, "/api/v1/reports/list?limit=500", "", http.StatusBadRequest, "validation_failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(bindingRouter(), tt.method, tt.target, tt.body)
			require.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantCode != "" {
				require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
			}
		})
	}
}

func TestQueryValidationUsesFormFieldName(t *testing.T) {
	rec := do(bindingRouter(), http.MethodGet, "/api/v1/reports/list?limit=500", "")
	require.Equal(t, []dto.ErrorDetail{
		{Field: "limit", Rule: "max", Message: "must be at most 100"},
	}, decodeError(t, rec).Details)
}
