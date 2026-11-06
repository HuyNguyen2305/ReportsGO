package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/service"
)

// DeviceTrackingService is what the handler needs from the service layer. *service.DeviceTrackingService satisfies it.
type DeviceTrackingService interface {
	List(ctx context.Context, p service.ListDeviceTrackingParams) (*service.ListDeviceTrackingResult, error)
	FilterOptions(ctx context.Context) (*service.DeviceTrackingFilterOptions, error)
}

// DeviceTrackingHandler serves the Device Tracking report. It is a RouteRegistrar, mounted under ReportsPrefix.
type DeviceTrackingHandler struct {
	svc    DeviceTrackingService
	logger *slog.Logger
}

// NewDeviceTrackingHandler returns the Device Tracking report handler.
func NewDeviceTrackingHandler(svc DeviceTrackingService, logger *slog.Logger) *DeviceTrackingHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &DeviceTrackingHandler{svc: svc, logger: logger}
}

// Register mounts GET /device-tracking and GET /device-tracking/filters.
func (h *DeviceTrackingHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/device-tracking", h.List)
	rg.GET("/device-tracking/filters", h.Filters)
}

// List serves one page of the report.
func (h *DeviceTrackingHandler) List(c *gin.Context) {
	params, err := parseDeviceTrackingQuery(c.Request.URL.RawQuery)
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
	c.JSON(http.StatusOK, toDeviceTrackingListResponse(res))
}

// Filters serves the data behind the report's filter controls.
func (h *DeviceTrackingHandler) Filters(c *gin.Context) {
	opts, err := h.svc.FilterOptions(c.Request.Context())
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toDeviceTrackingFiltersResponse(opts))
}

// Query parameters accepted by the list endpoint.
var (
	deviceTrackingSingleValueParams = []string{"from", "to", "q", "sort", "order", "limit", "cursor"}
	deviceTrackingMultiValueParams  = []string{"client_id"}
)

// parseDeviceTrackingQuery turns the raw query string into service params. See parseStrictQuery for what is rejected.
func parseDeviceTrackingQuery(rawQuery string) (service.ListDeviceTrackingParams, error) {
	values, err := parseStrictQuery(rawQuery, deviceTrackingSingleValueParams, deviceTrackingMultiValueParams)
	if err != nil {
		return service.ListDeviceTrackingParams{}, err
	}

	return service.ListDeviceTrackingParams{
		From:        values.Get("from"),
		To:          values.Get("to"),
		Search:      values.Get("q"),
		Sort:        values.Get("sort"),
		Order:       values.Get("order"),
		Limit:       values.Get("limit"),
		Cursor:      values.Get("cursor"),
		CustomerIDs: flatten(values["client_id"]),
	}, nil
}
