package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/service"
)

// InvoiceService is what the handler needs from the service layer. *service.InvoiceService satisfies it.
type InvoiceService interface {
	List(ctx context.Context, p service.ListInvoicesParams) (*service.ListInvoicesResult, error)
	FilterOptions(ctx context.Context) (*service.InvoiceFilterOptions, error)
}

// InvoiceHandler serves the Invoices report. It is a RouteRegistrar, mounted under ReportsPrefix.
type InvoiceHandler struct {
	svc    InvoiceService
	logger *slog.Logger
}

// NewInvoiceHandler returns the Invoices report handler.
func NewInvoiceHandler(svc InvoiceService, logger *slog.Logger) *InvoiceHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &InvoiceHandler{svc: svc, logger: logger}
}

// Register mounts GET /invoices and GET /invoices/filters.
func (h *InvoiceHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/invoices", h.List)
	rg.GET("/invoices/filters", h.Filters)
}

// List serves one page of the report.
func (h *InvoiceHandler) List(c *gin.Context) {
	params, err := parseInvoiceQuery(c.Request.URL.RawQuery)
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
	c.JSON(http.StatusOK, toInvoiceListResponse(res))
}

// Filters serves the data behind the report's filter controls.
func (h *InvoiceHandler) Filters(c *gin.Context) {
	opts, err := h.svc.FilterOptions(c.Request.Context())
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toInvoiceFiltersResponse(opts))
}

// Query parameters accepted by the list endpoint.
var (
	singleValueParams = []string{"from", "to", "lifecycle", "q", "sort", "order", "limit", "cursor"}
	multiValueParams  = []string{"status", "signature_status", "service_id", "tag_id", "payment_term_id"}
)

// parseInvoiceQuery turns the raw query string into service params. See parseStrictQuery for what is rejected.
func parseInvoiceQuery(rawQuery string) (service.ListInvoicesParams, error) {
	values, err := parseStrictQuery(rawQuery, singleValueParams, multiValueParams)
	if err != nil {
		return service.ListInvoicesParams{}, err
	}

	return service.ListInvoicesParams{
		From:            values.Get("from"),
		To:              values.Get("to"),
		Lifecycle:       values.Get("lifecycle"),
		Search:          values.Get("q"),
		Sort:            values.Get("sort"),
		Order:           values.Get("order"),
		Limit:           values.Get("limit"),
		Cursor:          values.Get("cursor"),
		Status:          flatten(values["status"]),
		SignatureStatus: flatten(values["signature_status"]),
		ServiceIDs:      flatten(values["service_id"]),
		TagIDs:          flatten(values["tag_id"]),
		PaymentTermIDs:  flatten(values["payment_term_id"]),
	}, nil
}
