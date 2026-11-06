package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/service"
)

// SystemEmailService is what the handler needs from the service layer. *service.SystemEmailService satisfies it.
type SystemEmailService interface {
	List(ctx context.Context, p service.ListSystemEmailsParams) (*service.ListSystemEmailsResult, error)
	FilterOptions(ctx context.Context) (*service.SystemEmailFilterOptions, error)
}

// SystemEmailHandler serves the System Email History report. It is a RouteRegistrar, mounted under ReportsPrefix.
type SystemEmailHandler struct {
	svc    SystemEmailService
	logger *slog.Logger
}

// NewSystemEmailHandler returns the System Email History report handler.
func NewSystemEmailHandler(svc SystemEmailService, logger *slog.Logger) *SystemEmailHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &SystemEmailHandler{svc: svc, logger: logger}
}

// Register mounts GET /system-email-history and GET /system-email-history/filters.
func (h *SystemEmailHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/system-email-history", h.List)
	rg.GET("/system-email-history/filters", h.Filters)
}

// List serves one page of the report.
func (h *SystemEmailHandler) List(c *gin.Context) {
	params, err := parseSystemEmailQuery(c.Request.URL.RawQuery)
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
	c.JSON(http.StatusOK, toSystemEmailListResponse(res))
}

// Filters serves the data behind the report's filter controls.
func (h *SystemEmailHandler) Filters(c *gin.Context) {
	opts, err := h.svc.FilterOptions(c.Request.Context())
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toSystemEmailFiltersResponse(opts))
}

// Query parameters accepted by the list endpoint.
var (
	systemEmailSingleValueParams = []string{"from", "to", "name_format", "q", "sort", "order", "limit", "cursor"}
	systemEmailMultiValueParams  = []string{"status", "client_id"}
)

// parseSystemEmailQuery turns the raw query string into service params. See parseStrictQuery for what is rejected.
func parseSystemEmailQuery(rawQuery string) (service.ListSystemEmailsParams, error) {
	values, err := parseStrictQuery(rawQuery, systemEmailSingleValueParams, systemEmailMultiValueParams)
	if err != nil {
		return service.ListSystemEmailsParams{}, err
	}

	return service.ListSystemEmailsParams{
		From:        values.Get("from"),
		To:          values.Get("to"),
		NameFormat:  values.Get("name_format"),
		Search:      values.Get("q"),
		Sort:        values.Get("sort"),
		Order:       values.Get("order"),
		Limit:       values.Get("limit"),
		Cursor:      values.Get("cursor"),
		Status:      flatten(values["status"]),
		CustomerIDs: flatten(values["client_id"]),
	}, nil
}
