package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/service"
)

// InboundLeadService is what the handler needs from the service layer. *service.InboundLeadService satisfies it.
type InboundLeadService interface {
	List(ctx context.Context, p service.ListInboundLeadsParams) (*service.ListInboundLeadsResult, error)
	FilterOptions(ctx context.Context) (*service.InboundLeadFilterOptions, error)
}

// InboundLeadHandler serves the Inbound Leads report. It is a RouteRegistrar, mounted under ReportsPrefix.
type InboundLeadHandler struct {
	svc    InboundLeadService
	logger *slog.Logger
}

// NewInboundLeadHandler returns the Inbound Leads report handler.
func NewInboundLeadHandler(svc InboundLeadService, logger *slog.Logger) *InboundLeadHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &InboundLeadHandler{svc: svc, logger: logger}
}

// Register mounts GET /inbound-leads and GET /inbound-leads/filters.
func (h *InboundLeadHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/inbound-leads", h.List)
	rg.GET("/inbound-leads/filters", h.Filters)
}

// List serves one page of the report.
func (h *InboundLeadHandler) List(c *gin.Context) {
	params, err := parseInboundLeadQuery(c.Request.URL.RawQuery)
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
	c.JSON(http.StatusOK, toInboundLeadListResponse(res))
}

// Filters serves the data behind the report's filter controls.
func (h *InboundLeadHandler) Filters(c *gin.Context) {
	opts, err := h.svc.FilterOptions(c.Request.Context())
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toInboundLeadFiltersResponse(opts))
}

// Query parameters accepted by the list endpoint.
var (
	inboundLeadSingleValueParams = []string{"from", "to", "name_format", "q", "sort", "order", "limit", "cursor"}
	inboundLeadMultiValueParams  = []string{"source_id"}
)

// parseInboundLeadQuery turns the raw query string into service params. See parseStrictQuery for what is rejected.
func parseInboundLeadQuery(rawQuery string) (service.ListInboundLeadsParams, error) {
	values, err := parseStrictQuery(rawQuery, inboundLeadSingleValueParams, inboundLeadMultiValueParams)
	if err != nil {
		return service.ListInboundLeadsParams{}, err
	}

	return service.ListInboundLeadsParams{
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
