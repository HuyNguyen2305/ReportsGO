package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/service"
)

// CustomerService is what the handler needs from the service layer. *service.CustomerService satisfies it.
type CustomerService interface {
	List(ctx context.Context, p service.ListCustomersParams) (*service.ListCustomersResult, error)
	FilterOptions(ctx context.Context) (*service.CustomerFilterOptions, error)
}

// CustomerHandler serves the New Customers report. It is a RouteRegistrar, mounted under ReportsPrefix.
type CustomerHandler struct {
	svc    CustomerService
	logger *slog.Logger
}

// NewCustomerHandler returns the New Customers report handler.
func NewCustomerHandler(svc CustomerService, logger *slog.Logger) *CustomerHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &CustomerHandler{svc: svc, logger: logger}
}

// Register mounts GET /new-customers and GET /new-customers/filters.
func (h *CustomerHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/new-customers", h.List)
	rg.GET("/new-customers/filters", h.Filters)
}

// List serves one page of the report.
func (h *CustomerHandler) List(c *gin.Context) {
	params, err := parseCustomerQuery(c.Request.URL.RawQuery)
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
	c.JSON(http.StatusOK, toCustomerListResponse(res))
}

// Filters serves the data behind the report's filter controls.
func (h *CustomerHandler) Filters(c *gin.Context) {
	opts, err := h.svc.FilterOptions(c.Request.Context())
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toCustomerFiltersResponse(opts))
}

// Query parameters accepted by the list endpoint.
var (
	customerSingleValueParams = []string{"from", "to", "q", "sort", "order", "limit", "cursor"}
	customerMultiValueParams  = []string{"lead_source_id"}
)

// parseCustomerQuery turns the raw query string into service params. See parseStrictQuery for what is rejected.
func parseCustomerQuery(rawQuery string) (service.ListCustomersParams, error) {
	values, err := parseStrictQuery(rawQuery, customerSingleValueParams, customerMultiValueParams)
	if err != nil {
		return service.ListCustomersParams{}, err
	}

	return service.ListCustomersParams{
		From:          values.Get("from"),
		To:            values.Get("to"),
		Search:        values.Get("q"),
		Sort:          values.Get("sort"),
		Order:         values.Get("order"),
		Limit:         values.Get("limit"),
		Cursor:        values.Get("cursor"),
		LeadSourceIDs: flatten(values["lead_source_id"]),
	}, nil
}
