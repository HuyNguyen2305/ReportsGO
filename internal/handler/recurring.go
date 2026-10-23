package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/service"
)

// RecurringService is what the handler needs from the service layer. *service.RecurringService satisfies it.
type RecurringService interface {
	List(ctx context.Context, p service.ListRecurringParams) (*service.ListRecurringResult, error)
}

// RecurringHandler serves the Recurring Invoices report. It is a RouteRegistrar, mounted under ReportsPrefix.
type RecurringHandler struct {
	svc    RecurringService
	logger *slog.Logger
}

// NewRecurringHandler returns the Recurring Invoices report handler.
func NewRecurringHandler(svc RecurringService, logger *slog.Logger) *RecurringHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &RecurringHandler{svc: svc, logger: logger}
}

// Register mounts GET /recurring-invoices and GET /recurring-invoices/filters.
func (h *RecurringHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/recurring-invoices", h.List)
	rg.GET("/recurring-invoices/filters", h.Filters)
}

// List serves one page of the report.
func (h *RecurringHandler) List(c *gin.Context) {
	params, err := parseRecurringQuery(c.Request.URL.RawQuery)
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
	c.JSON(http.StatusOK, toRecurringListResponse(res))
}

// Filters serves the data behind the report's filter controls. The choices are fixed, so no service is involved.
func (h *RecurringHandler) Filters(c *gin.Context) {
	c.JSON(http.StatusOK, recurringFiltersResponse())
}

// Query parameters accepted by the list endpoint. Recurring invoices have no Active/Deleted tab, so "lifecycle"
// is not among them and is rejected like any other unknown parameter.
var (
	recurringSingleValueParams = []string{"from", "to", "q", "sort", "order", "limit", "cursor"}
	recurringMultiValueParams  = []string{"status", "action"}
)

// parseRecurringQuery turns the raw query string into service params. See parseStrictQuery for what is rejected.
func parseRecurringQuery(rawQuery string) (service.ListRecurringParams, error) {
	values, err := parseStrictQuery(rawQuery, recurringSingleValueParams, recurringMultiValueParams)
	if err != nil {
		return service.ListRecurringParams{}, err
	}

	return service.ListRecurringParams{
		From:   values.Get("from"),
		To:     values.Get("to"),
		Search: values.Get("q"),
		Sort:   values.Get("sort"),
		Order:  values.Get("order"),
		Limit:  values.Get("limit"),
		Cursor: values.Get("cursor"),
		Status: flatten(values["status"]),
		Action: flatten(values["action"]),
	}, nil
}
