package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/service"
)

// OnlineBookingService is what the handler needs from the service layer. *service.OnlineBookingService satisfies it.
type OnlineBookingService interface {
	List(ctx context.Context, p service.ListOnlineBookingsParams) (*service.ListOnlineBookingsResult, error)
	FilterOptions(ctx context.Context) (*service.OnlineBookingFilterOptions, error)
}

// OnlineBookingHandler serves the Online Bookings report. It is a RouteRegistrar, mounted under ReportsPrefix.
type OnlineBookingHandler struct {
	svc    OnlineBookingService
	logger *slog.Logger
}

// NewOnlineBookingHandler returns the Online Bookings report handler.
func NewOnlineBookingHandler(svc OnlineBookingService, logger *slog.Logger) *OnlineBookingHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &OnlineBookingHandler{svc: svc, logger: logger}
}

// Register mounts GET /online-bookings and GET /online-bookings/filters.
func (h *OnlineBookingHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/online-bookings", h.List)
	rg.GET("/online-bookings/filters", h.Filters)
}

// List serves one page of the report.
func (h *OnlineBookingHandler) List(c *gin.Context) {
	params, err := parseOnlineBookingQuery(c.Request.URL.RawQuery)
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
	c.JSON(http.StatusOK, toOnlineBookingListResponse(res))
}

// Filters serves the data behind the report's filter controls.
func (h *OnlineBookingHandler) Filters(c *gin.Context) {
	opts, err := h.svc.FilterOptions(c.Request.Context())
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toOnlineBookingFiltersResponse(opts))
}

// Query parameters accepted by the list endpoint.
var (
	onlineBookingSingleValueParams = []string{"from", "to", "name_format", "q", "sort", "order", "limit", "cursor"}
	onlineBookingMultiValueParams  = []string{"source_id"}
)

// parseOnlineBookingQuery turns the raw query string into service params. See parseStrictQuery for what is rejected.
func parseOnlineBookingQuery(rawQuery string) (service.ListOnlineBookingsParams, error) {
	values, err := parseStrictQuery(rawQuery, onlineBookingSingleValueParams, onlineBookingMultiValueParams)
	if err != nil {
		return service.ListOnlineBookingsParams{}, err
	}

	return service.ListOnlineBookingsParams{
		From:       values.Get("from"),
		To:         values.Get("to"),
		NameFormat: values.Get("name_format"),
		Search:     values.Get("q"),
		Sort:       values.Get("sort"),
		Order:      values.Get("order"),
		Limit:      values.Get("limit"),
		Cursor:     values.Get("cursor"),
		SourceIDs:  flatten(values["source_id"]),
	}, nil
}
