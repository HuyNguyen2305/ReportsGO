package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/service"
)

// PaymentService is what the handler needs from the service layer. *service.PaymentService satisfies it.
type PaymentService interface {
	List(ctx context.Context, p service.ListPaymentsParams) (*service.ListPaymentsResult, error)
}

// PaymentHandler serves the Payments report. It is a RouteRegistrar, mounted under ReportsPrefix.
type PaymentHandler struct {
	svc    PaymentService
	logger *slog.Logger
}

// NewPaymentHandler returns the Payments report handler.
func NewPaymentHandler(svc PaymentService, logger *slog.Logger) *PaymentHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &PaymentHandler{svc: svc, logger: logger}
}

// Register mounts GET /payments and GET /payments/filters.
func (h *PaymentHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/payments", h.List)
	rg.GET("/payments/filters", h.Filters)
}

// List serves one page of the report.
func (h *PaymentHandler) List(c *gin.Context) {
	params, err := parsePaymentQuery(c.Request.URL.RawQuery)
	if errors.Is(err, errMalformedQuery) {
		WriteError(c, http.StatusBadRequest, "invalid_request", "query string is malformed")
		return
	}
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}

	res, err := h.svc.List(c.Request.Context(), params)
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toPaymentListResponse(res))
}

// Filters serves the data behind the report's filter controls. The choices are fixed, so no service is involved.
func (h *PaymentHandler) Filters(c *gin.Context) {
	c.JSON(http.StatusOK, paymentFiltersResponse())
}

// Query parameters accepted by the list endpoint.
var (
	paymentSingleValueParams = []string{"from", "to", "lifecycle", "q", "sort", "order", "limit", "cursor"}
	paymentMultiValueParams  = []string{"status", "method"}
)

// parsePaymentQuery turns the raw query string into service params. See parseStrictQuery for what is rejected.
func parsePaymentQuery(rawQuery string) (service.ListPaymentsParams, error) {
	values, err := parseStrictQuery(rawQuery, paymentSingleValueParams, paymentMultiValueParams)
	if err != nil {
		return service.ListPaymentsParams{}, err
	}

	return service.ListPaymentsParams{
		From:      values.Get("from"),
		To:        values.Get("to"),
		Lifecycle: values.Get("lifecycle"),
		Search:    values.Get("q"),
		Sort:      values.Get("sort"),
		Order:     values.Get("order"),
		Limit:     values.Get("limit"),
		Cursor:    values.Get("cursor"),
		Status:    flatten(values["status"]),
		Method:    flatten(values["method"]),
	}, nil
}
