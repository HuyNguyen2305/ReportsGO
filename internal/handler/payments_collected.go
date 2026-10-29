package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/service"
)

// PaymentsCollectedService is what the handler needs from the service layer. *service.PaymentsCollectedService
// satisfies it.
type PaymentsCollectedService interface {
	List(ctx context.Context, p service.ListPaymentsCollectedParams) (*service.ListPaymentsCollectedResult, error)
	FilterOptions(ctx context.Context) (*service.PaymentsCollectedFilterOptions, error)
}

// PaymentsCollectedHandler serves the Payments Collected report. It is a RouteRegistrar, mounted under
// ReportsPrefix.
type PaymentsCollectedHandler struct {
	svc    PaymentsCollectedService
	logger *slog.Logger
}

// NewPaymentsCollectedHandler returns the Payments Collected report handler.
func NewPaymentsCollectedHandler(svc PaymentsCollectedService, logger *slog.Logger) *PaymentsCollectedHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &PaymentsCollectedHandler{svc: svc, logger: logger}
}

// Register mounts GET /payments-collected and GET /payments-collected/filters.
func (h *PaymentsCollectedHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/payments-collected", h.List)
	rg.GET("/payments-collected/filters", h.Filters)
}

// List serves one page of the report.
func (h *PaymentsCollectedHandler) List(c *gin.Context) {
	params, err := parsePaymentsCollectedQuery(c.Request.URL.RawQuery)
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
	c.JSON(http.StatusOK, toPaymentCollectedListResponse(res))
}

// Filters serves the data behind the report's filter controls.
func (h *PaymentsCollectedHandler) Filters(c *gin.Context) {
	opts, err := h.svc.FilterOptions(c.Request.Context())
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toPaymentsCollectedFiltersResponse(opts))
}

// Query parameters accepted by the list endpoint.
var (
	paymentsCollectedSingleParams = []string{"from", "to", "payment_for", "q", "sort", "order", "limit", "cursor"}
	paymentsCollectedMultiParams  = []string{"method", "customer_id", "staff_id"}
)

// parsePaymentsCollectedQuery turns the raw query string into service params. See parseStrictQuery for what is
// rejected.
func parsePaymentsCollectedQuery(rawQuery string) (service.ListPaymentsCollectedParams, error) {
	values, err := parseStrictQuery(rawQuery, paymentsCollectedSingleParams, paymentsCollectedMultiParams)
	if err != nil {
		return service.ListPaymentsCollectedParams{}, err
	}

	return service.ListPaymentsCollectedParams{
		From:        values.Get("from"),
		To:          values.Get("to"),
		PaymentFor:  values.Get("payment_for"),
		Search:      values.Get("q"),
		Sort:        values.Get("sort"),
		Order:       values.Get("order"),
		Limit:       values.Get("limit"),
		Cursor:      values.Get("cursor"),
		Method:      flatten(values["method"]),
		CustomerIDs: flatten(values["customer_id"]),
		StaffIDs:    flatten(values["staff_id"]),
	}, nil
}
