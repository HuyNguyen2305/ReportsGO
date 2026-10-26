// Package handler binds and validates requests, calls services, and maps results to HTTP responses.
// It never contains SQL.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5/pgconn"

	"reportsgo/internal/dto"
	"reportsgo/internal/model"
)

// requestIDHeader is set on the response by the request-id middleware.
const requestIDHeader = "X-Request-ID"

// statusClientClosedRequest is the de-facto status for a client that went away mid-request.
const statusClientClosedRequest = 499

// sqlStateQueryCanceled is what Postgres reports when it stops a statement, including when the
// connection's statement_timeout expires.
const sqlStateQueryCanceled = "57014"

// RespondError is the one place where errors become HTTP responses. It aborts the request with the
// standard error envelope. Unexpected errors are logged and reported to the client as a generic 500.
func RespondError(c *gin.Context, logger *slog.Logger, err error) {
	if logger == nil {
		logger = slog.Default()
	}
	_ = c.Error(err) // lets logging middleware see the error

	status, body := mapError(err)
	body.RequestID = c.Writer.Header().Get(requestIDHeader)

	switch status {
	case http.StatusInternalServerError:
		logger.ErrorContext(c.Request.Context(), "request failed",
			"error", err, "request_id", body.RequestID, "method", c.Request.Method, "path", c.FullPath())
	case http.StatusGatewayTimeout:
		logger.WarnContext(c.Request.Context(), "request timed out",
			"error", err, "request_id", body.RequestID, "method", c.Request.Method, "path", c.FullPath())
	case statusClientClosedRequest:
		logger.DebugContext(c.Request.Context(), "client closed request",
			"request_id", body.RequestID, "method", c.Request.Method, "path", c.FullPath())
	}

	c.AbortWithStatusJSON(status, dto.ErrorResponse{Error: body})
}

// BindError marks an error that came from reading the client's request (decoding a body, parsing a query
// string). Only errors carrying this mark are reported as 400; the same parse error raised deeper in the
// stack is a server fault, not bad input.
type BindError struct{ Err error }

func (e *BindError) Error() string { return "bind request: " + e.Err.Error() }
func (e *BindError) Unwrap() error { return e.Err }

// RespondBindError reports a failed ShouldBind* call: a 400 built from the binding error.
func RespondBindError(c *gin.Context, logger *slog.Logger, err error) {
	RespondError(c, logger, &BindError{Err: err})
}

// WriteError aborts with a fixed status and code, for responses that do not come from an error value.
func WriteError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, dto.ErrorResponse{Error: dto.ErrorBody{
		Code:      code,
		Message:   message,
		RequestID: c.Writer.Header().Get(requestIDHeader),
	}})
}

func mapError(err error) (int, dto.ErrorBody) {
	var (
		validationErrs validator.ValidationErrors
		invalidInput   *model.InvalidInputError
		typeErr        *json.UnmarshalTypeError
		bindErr        *BindError
		pgErr          *pgconn.PgError
	)

	switch {
	case errors.As(err, &validationErrs):
		details := make([]dto.ErrorDetail, len(validationErrs))
		for i, fe := range validationErrs {
			details[i] = dto.ErrorDetail{Field: fe.Field(), Rule: fe.Tag(), Message: fieldMessage(fe)}
		}
		return http.StatusBadRequest, dto.ErrorBody{
			Code: "validation_failed", Message: "request validation failed", Details: details,
		}

	case errors.As(err, &typeErr):
		return http.StatusBadRequest, dto.ErrorBody{
			Code: "invalid_request", Message: "request is malformed",
			Details: []dto.ErrorDetail{{Field: typeErr.Field, Message: "must be of type " + typeErr.Type.String()}},
		}

	case errors.As(err, &bindErr):
		return http.StatusBadRequest, dto.ErrorBody{Code: "invalid_request", Message: "request is malformed"}

	case errors.As(err, &invalidInput):
		details := make([]dto.ErrorDetail, len(invalidInput.Fields))
		for i, f := range invalidInput.Fields {
			details[i] = dto.ErrorDetail{Field: f.Field, Message: f.Message}
		}
		return http.StatusUnprocessableEntity, dto.ErrorBody{
			Code: "invalid_input", Message: "request contains invalid values", Details: details,
		}

	case errors.Is(err, model.ErrInvalidInput):
		return http.StatusUnprocessableEntity, dto.ErrorBody{Code: "invalid_input", Message: "request contains invalid values"}

	case errors.Is(err, model.ErrNotFound):
		return http.StatusNotFound, dto.ErrorBody{Code: "not_found", Message: "resource not found"}

	case errors.Is(err, model.ErrConflict):
		return http.StatusConflict, dto.ErrorBody{Code: "conflict", Message: "request conflicts with the current state"}

	case errors.Is(err, context.DeadlineExceeded),
		errors.As(err, &pgErr) && pgErr.Code == sqlStateQueryCanceled:
		return http.StatusGatewayTimeout, dto.ErrorBody{Code: "timeout", Message: "request timed out"}

	case errors.Is(err, context.Canceled):
		return statusClientClosedRequest, dto.ErrorBody{Code: "client_closed_request", Message: "client closed the request"}

	default:
		return http.StatusInternalServerError, dto.ErrorBody{Code: "internal_error", Message: "internal server error"}
	}
}

func fieldMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "min", "max":
		bound := "at least"
		if fe.Tag() == "max" {
			bound = "at most"
		}
		switch fe.Kind() { //nolint:exhaustive // only the kinds with a length need special wording
		case reflect.String:
			return fmt.Sprintf("must be %s %s characters long", bound, fe.Param())
		case reflect.Slice, reflect.Array, reflect.Map:
			return fmt.Sprintf("must contain %s %s items", bound, fe.Param())
		default:
			return fmt.Sprintf("must be %s %s", bound, fe.Param())
		}
	case "gte":
		return "must be greater than or equal to " + fe.Param()
	case "lte":
		return "must be less than or equal to " + fe.Param()
	case "gt":
		return "must be greater than " + fe.Param()
	case "lt":
		return "must be less than " + fe.Param()
	case "oneof":
		return "must be one of: " + strings.ReplaceAll(fe.Param(), " ", ", ")
	case "email":
		return "must be a valid email address"
	case "datetime":
		return "must match the format " + fe.Param()
	default:
		return "failed validation rule " + fe.Tag()
	}
}

var registerValidatorOnce sync.Once

// registerValidator makes validation errors report the JSON/form/URI field name instead of the Go name.
func registerValidator() {
	registerValidatorOnce.Do(func() {
		v, ok := binding.Validator.Engine().(*validator.Validate)
		if !ok {
			return
		}
		v.RegisterTagNameFunc(func(f reflect.StructField) string {
			for _, tag := range []string{"json", "form", "uri"} {
				if name, _, _ := strings.Cut(f.Tag.Get(tag), ","); name != "" && name != "-" {
					return name
				}
			}
			return f.Name
		})
	})
}
