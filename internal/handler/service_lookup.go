package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/service"
)

// ServiceLookupService is what the handler needs from the service layer. *service.ServiceLookupService satisfies it.
type ServiceLookupService interface {
	List(ctx context.Context, p service.ListServiceLookupParams) (*service.ListServiceLookupResult, error)
	FilterOptions(ctx context.Context) (*service.ServiceLookupFilterOptions, error)
}

// ServiceLookupHandler serves the Service Lookup report. It is a RouteRegistrar, mounted under ReportsPrefix.
type ServiceLookupHandler struct {
	svc    ServiceLookupService
	logger *slog.Logger
}

// NewServiceLookupHandler returns the Service Lookup report handler.
func NewServiceLookupHandler(svc ServiceLookupService, logger *slog.Logger) *ServiceLookupHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ServiceLookupHandler{svc: svc, logger: logger}
}

// Register mounts GET /service-lookup and GET /service-lookup/filters.
func (h *ServiceLookupHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/service-lookup", h.List)
	rg.GET("/service-lookup/filters", h.Filters)
}

// List serves one page of the report.
func (h *ServiceLookupHandler) List(c *gin.Context) {
	params, err := parseServiceLookupQuery(c.Request.URL.RawQuery)
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
	c.JSON(http.StatusOK, toServiceLookupListResponse(res))
}

// Filters serves the data behind the report's filter controls.
func (h *ServiceLookupHandler) Filters(c *gin.Context) {
	opts, err := h.svc.FilterOptions(c.Request.Context())
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toServiceLookupFiltersResponse(opts))
}

// Query parameters accepted by the list endpoint.
var (
	serviceLookupSingleParams = []string{"tab", "from", "to", "q", "sort", "order", "limit", "cursor"}
	serviceLookupMultiParams  = []string{"status", "service_type", "assigned_to", "tag"}
)

// parseServiceLookupQuery turns the raw query string into service params. See parseStrictQuery for what is
// rejected.
func parseServiceLookupQuery(rawQuery string) (service.ListServiceLookupParams, error) {
	values, err := parseStrictQuery(rawQuery, serviceLookupSingleParams, serviceLookupMultiParams)
	if err != nil {
		return service.ListServiceLookupParams{}, err
	}

	return service.ListServiceLookupParams{
		Tab:         values.Get("tab"),
		From:        values.Get("from"),
		To:          values.Get("to"),
		Search:      values.Get("q"),
		Sort:        values.Get("sort"),
		Order:       values.Get("order"),
		Limit:       values.Get("limit"),
		Cursor:      values.Get("cursor"),
		Status:      flatten(values["status"]),
		ServiceType: flatten(values["service_type"]),
		AssignedTo:  flatten(values["assigned_to"]),
		Tag:         flatten(values["tag"]),
	}, nil
}
