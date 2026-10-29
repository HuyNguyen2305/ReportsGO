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

// NoActiveJobsService is what the handler needs from the service layer. *service.NoActiveJobsService satisfies it.
type NoActiveJobsService interface {
	List(ctx context.Context, grain model.NoActiveJobsGrain, p service.ListNoActiveJobsParams) (*service.ListNoActiveJobsResult, error)
}

// NoActiveJobsHandler serves the Customers w/o Active Jobs and Locations w/o Active Jobs reports. It is a
// RouteRegistrar, mounted under ReportsPrefix.
type NoActiveJobsHandler struct {
	svc    NoActiveJobsService
	logger *slog.Logger
}

// NewNoActiveJobsHandler returns the w/o Active Jobs report handler.
func NewNoActiveJobsHandler(svc NoActiveJobsService, logger *slog.Logger) *NoActiveJobsHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &NoActiveJobsHandler{svc: svc, logger: logger}
}

// Register mounts GET /customers-without-active-jobs and GET /locations-without-active-jobs, each with /filters.
func (h *NoActiveJobsHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/customers-without-active-jobs", h.Customers)
	rg.GET("/customers-without-active-jobs/filters", h.CustomersFilters)
	rg.GET("/locations-without-active-jobs", h.Locations)
	rg.GET("/locations-without-active-jobs/filters", h.LocationsFilters)
}

// Customers serves one page of the Customers w/o Active Jobs report.
func (h *NoActiveJobsHandler) Customers(c *gin.Context) {
	res, ok := h.list(c, model.NoActiveJobsCustomers)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, toNoActiveJobsCustomersResponse(res))
}

// Locations serves one page of the Locations w/o Active Jobs report.
func (h *NoActiveJobsHandler) Locations(c *gin.Context) {
	res, ok := h.list(c, model.NoActiveJobsLocations)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, toNoActiveJobsLocationsResponse(res))
}

// CustomersFilters serves the data behind the Customers report's filter controls.
func (h *NoActiveJobsHandler) CustomersFilters(c *gin.Context) {
	c.JSON(http.StatusOK, noActiveJobsFiltersResponse(model.NoActiveJobsCustomers))
}

// LocationsFilters serves the data behind the Locations report's filter controls.
func (h *NoActiveJobsHandler) LocationsFilters(c *gin.Context) {
	c.JSON(http.StatusOK, noActiveJobsFiltersResponse(model.NoActiveJobsLocations))
}

// list parses the request and calls the service; on failure it has already written the error response.
func (h *NoActiveJobsHandler) list(c *gin.Context, grain model.NoActiveJobsGrain) (*service.ListNoActiveJobsResult, bool) {
	params, err := parseNoActiveJobsQuery(c.Request.URL.RawQuery)
	if errors.Is(err, errMalformedQuery) {
		WriteError(c, http.StatusBadRequest, "invalid_request", "query string is malformed")
		return nil, false
	}
	if err != nil {
		RespondError(c, h.logger, err)
		return nil, false
	}

	res, err := h.svc.List(c.Request.Context(), grain, params)
	if err != nil {
		RespondError(c, h.logger, err)
		return nil, false
	}
	return res, true
}

// Query parameters accepted by the list endpoints.
var (
	noActiveJobsSingleParams = []string{"q", "sort", "order", "limit", "cursor"}
	noActiveJobsMultiParams  = []string{"status"}
)

// parseNoActiveJobsQuery turns the raw query string into service params. See parseStrictQuery for what is rejected.
func parseNoActiveJobsQuery(rawQuery string) (service.ListNoActiveJobsParams, error) {
	values, err := parseStrictQuery(rawQuery, noActiveJobsSingleParams, noActiveJobsMultiParams)
	if err != nil {
		return service.ListNoActiveJobsParams{}, err
	}

	return service.ListNoActiveJobsParams{
		Search: values.Get("q"),
		Sort:   values.Get("sort"),
		Order:  values.Get("order"),
		Limit:  values.Get("limit"),
		Cursor: values.Get("cursor"),
		Status: flatten(values["status"]),
	}, nil
}
