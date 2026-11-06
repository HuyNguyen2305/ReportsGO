package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/service"
)

// MaterialUseService is what the handler needs from the service layer. *service.MaterialUseService satisfies it.
type MaterialUseService interface {
	List(ctx context.Context, p service.ListMaterialUseParams) (*service.ListMaterialUseResult, error)
	FilterOptions(ctx context.Context) (*service.MaterialUseFilterOptions, error)
}

// MaterialUseHandler serves the Material Use report. It is a RouteRegistrar, mounted under ReportsPrefix.
type MaterialUseHandler struct {
	svc    MaterialUseService
	logger *slog.Logger
}

// NewMaterialUseHandler returns the Material Use report handler.
func NewMaterialUseHandler(svc MaterialUseService, logger *slog.Logger) *MaterialUseHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &MaterialUseHandler{svc: svc, logger: logger}
}

// Register mounts GET /material-use and GET /material-use/filters.
func (h *MaterialUseHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/material-use", h.List)
	rg.GET("/material-use/filters", h.Filters)
}

// List serves one page of the report.
func (h *MaterialUseHandler) List(c *gin.Context) {
	params, err := parseMaterialUseQuery(c.Request.URL.RawQuery)
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
	c.JSON(http.StatusOK, toMaterialUseListResponse(res))
}

// Filters serves the data behind the report's filter controls.
func (h *MaterialUseHandler) Filters(c *gin.Context) {
	opts, err := h.svc.FilterOptions(c.Request.Context())
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toMaterialUseFiltersResponse(opts))
}

// Query parameters accepted by the list endpoint.
var (
	materialUseSingleValueParams = []string{"from", "to", "tab", "q", "sort", "order", "limit", "cursor"}
	materialUseMultiValueParams  = []string{"client_id", "material_id"}
)

// parseMaterialUseQuery turns the raw query string into service params. See parseStrictQuery for what is rejected.
func parseMaterialUseQuery(rawQuery string) (service.ListMaterialUseParams, error) {
	values, err := parseStrictQuery(rawQuery, materialUseSingleValueParams, materialUseMultiValueParams)
	if err != nil {
		return service.ListMaterialUseParams{}, err
	}

	return service.ListMaterialUseParams{
		From:        values.Get("from"),
		To:          values.Get("to"),
		Tab:         values.Get("tab"),
		Search:      values.Get("q"),
		Sort:        values.Get("sort"),
		Order:       values.Get("order"),
		Limit:       values.Get("limit"),
		Cursor:      values.Get("cursor"),
		CustomerIDs: flatten(values["client_id"]),
		MaterialIDs: flatten(values["material_id"]),
	}, nil
}
