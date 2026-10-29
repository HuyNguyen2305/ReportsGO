package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/service"
)

// AccountsAgingService is what the handler needs from the service layer. *service.AccountsAgingService satisfies it.
type AccountsAgingService interface {
	List(ctx context.Context, p service.ListAgingParams) (*service.ListAgingResult, error)
	FilterOptions(ctx context.Context) (*service.AgingFilterOptions, error)
}

// AccountsAgingHandler serves the Accounts Aging report. It is a RouteRegistrar, mounted under ReportsPrefix.
type AccountsAgingHandler struct {
	svc    AccountsAgingService
	logger *slog.Logger
}

// NewAccountsAgingHandler returns the Accounts Aging report handler.
func NewAccountsAgingHandler(svc AccountsAgingService, logger *slog.Logger) *AccountsAgingHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &AccountsAgingHandler{svc: svc, logger: logger}
}

// Register mounts GET /accounts-aging and GET /accounts-aging/filters.
func (h *AccountsAgingHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/accounts-aging", h.List)
	rg.GET("/accounts-aging/filters", h.Filters)
}

// List serves one page of the report.
func (h *AccountsAgingHandler) List(c *gin.Context) {
	params, err := parseAgingQuery(c.Request.URL.RawQuery)
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
	c.JSON(http.StatusOK, toAgingListResponse(res))
}

// Filters serves the data behind the report's filter controls.
func (h *AccountsAgingHandler) Filters(c *gin.Context) {
	opts, err := h.svc.FilterOptions(c.Request.Context())
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toAgingFiltersResponse(opts))
}

// Query parameters accepted by the list endpoint.
var (
	agingSingleParams = []string{"as_of", "days", "email", "q", "sort", "order", "limit", "cursor"}
	agingMultiParams  = []string{"service_type"}
)

// parseAgingQuery turns the raw query string into service params. See parseStrictQuery for what is rejected.
func parseAgingQuery(rawQuery string) (service.ListAgingParams, error) {
	values, err := parseStrictQuery(rawQuery, agingSingleParams, agingMultiParams)
	if err != nil {
		return service.ListAgingParams{}, err
	}

	return service.ListAgingParams{
		AsOf:        values.Get("as_of"),
		Days:        values.Get("days"),
		Email:       values.Get("email"),
		Search:      values.Get("q"),
		Sort:        values.Get("sort"),
		Order:       values.Get("order"),
		Limit:       values.Get("limit"),
		Cursor:      values.Get("cursor"),
		ServiceType: flatten(values["service_type"]),
	}, nil
}
