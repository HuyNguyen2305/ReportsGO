package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

// SalesBreakdownService is what the handler needs from the service layer. *service.SalesBreakdownService satisfies
// it.
type SalesBreakdownService interface {
	List(ctx context.Context, p service.ListSalesBreakdownParams) (*service.ListSalesBreakdownResult, error)
	FilterOptions(ctx context.Context) (*model.SalesBreakdownGroupOptions, error)
}

// SalesBreakdownHandler serves the Revenue and Sales Forecast reports. It is a RouteRegistrar, mounted under
// ReportsPrefix.
type SalesBreakdownHandler struct {
	svc    SalesBreakdownService
	logger *slog.Logger
}

// NewSalesBreakdownHandler returns the Revenue and Sales Forecast report handler.
func NewSalesBreakdownHandler(svc SalesBreakdownService, logger *slog.Logger) *SalesBreakdownHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &SalesBreakdownHandler{svc: svc, logger: logger}
}

// Register mounts GET /sales-breakdown and GET /sales-breakdown/filters.
func (h *SalesBreakdownHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/sales-breakdown", h.List)
	rg.GET("/sales-breakdown/filters", h.Filters)
}

// List serves one page of the grid.
func (h *SalesBreakdownHandler) List(c *gin.Context) {
	params, err := parseSalesBreakdownQuery(c.Request.URL.RawQuery)
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
	c.JSON(http.StatusOK, toSalesBreakdownListResponse(res))
}

// Filters serves the data behind the reports' filter controls.
func (h *SalesBreakdownHandler) Filters(c *gin.Context) {
	opts, err := h.svc.FilterOptions(c.Request.Context())
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toSalesBreakdownFiltersResponse(opts))
}

// Query parameters accepted by the list endpoint.
var (
	salesBreakdownSingleParams = []string{"type", "by", "year", "measure", "q", "sort", "order", "limit", "cursor"}
	salesBreakdownMultiParams  = []string{"group", "method"}
)

// parseSalesBreakdownQuery turns the raw query string into service params. See parseStrictQuery for what is
// rejected.
func parseSalesBreakdownQuery(rawQuery string) (service.ListSalesBreakdownParams, error) {
	values, err := parseStrictQuery(rawQuery, salesBreakdownSingleParams, salesBreakdownMultiParams)
	if err != nil {
		return service.ListSalesBreakdownParams{}, err
	}

	return service.ListSalesBreakdownParams{
		Type:    values.Get("type"),
		By:      values.Get("by"),
		Year:    values.Get("year"),
		Measure: values.Get("measure"),
		Search:  values.Get("q"),
		Sort:    values.Get("sort"),
		Order:   values.Get("order"),
		Limit:   values.Get("limit"),
		Cursor:  values.Get("cursor"),
		Group:   flatten(values["group"]),
		Method:  flatten(values["method"]),
	}, nil
}
