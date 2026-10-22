package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/service"
)

// EstimateService is what the handler needs from the service layer. *service.EstimateService satisfies it.
type EstimateService interface {
	List(ctx context.Context, p service.ListEstimatesParams) (*service.ListEstimatesResult, error)
}

// EstimateHandler serves the Estimates report. It is a RouteRegistrar, mounted under ReportsPrefix.
type EstimateHandler struct {
	svc    EstimateService
	logger *slog.Logger
}

// NewEstimateHandler returns the Estimates report handler.
func NewEstimateHandler(svc EstimateService, logger *slog.Logger) *EstimateHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &EstimateHandler{svc: svc, logger: logger}
}

// Register mounts GET /estimates and GET /estimates/filters.
func (h *EstimateHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/estimates", h.List)
	rg.GET("/estimates/filters", h.Filters)
}

// List serves one page of the report.
func (h *EstimateHandler) List(c *gin.Context) {
	params, err := parseEstimateQuery(c.Request.URL.RawQuery)
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
	c.JSON(http.StatusOK, toEstimateListResponse(res))
}

// Filters serves the choices behind the report's filter controls. They are fixed, so no service call is made.
func (h *EstimateHandler) Filters(c *gin.Context) {
	c.JSON(http.StatusOK, toEstimateFiltersResponse())
}

// Query parameters accepted by the list endpoint.
var (
	estimateSingleValueParams = []string{"from", "to", "lifecycle", "q", "sort", "order", "limit", "cursor"}
	estimateMultiValueParams  = []string{"status", "signature_status"}
)

// parseEstimateQuery turns the raw query string into service params. See parseStrictQuery for what is rejected.
func parseEstimateQuery(rawQuery string) (service.ListEstimatesParams, error) {
	values, err := parseStrictQuery(rawQuery, estimateSingleValueParams, estimateMultiValueParams)
	if err != nil {
		return service.ListEstimatesParams{}, err
	}

	return service.ListEstimatesParams{
		From:            values.Get("from"),
		To:              values.Get("to"),
		Lifecycle:       values.Get("lifecycle"),
		Search:          values.Get("q"),
		Sort:            values.Get("sort"),
		Order:           values.Get("order"),
		Limit:           values.Get("limit"),
		Cursor:          values.Get("cursor"),
		Status:          flatten(values["status"]),
		SignatureStatus: flatten(values["signature_status"]),
	}, nil
}
