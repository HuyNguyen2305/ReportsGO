package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/service"
)

// JobService is what the handler needs from the service layer. *service.JobService satisfies it.
type JobService interface {
	List(ctx context.Context, p service.ListJobsParams) (*service.ListJobsResult, error)
	FilterOptions(ctx context.Context) (*service.JobFilterOptions, error)
}

// JobHandler serves the New Jobs report. It is a RouteRegistrar, mounted under ReportsPrefix.
type JobHandler struct {
	svc    JobService
	logger *slog.Logger
}

// NewJobHandler returns the New Jobs report handler.
func NewJobHandler(svc JobService, logger *slog.Logger) *JobHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &JobHandler{svc: svc, logger: logger}
}

// Register mounts GET /new-jobs and GET /new-jobs/filters.
func (h *JobHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/new-jobs", h.List)
	rg.GET("/new-jobs/filters", h.Filters)
}

// List serves one page of the report.
func (h *JobHandler) List(c *gin.Context) {
	params, err := parseJobQuery(c.Request.URL.RawQuery)
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
	c.JSON(http.StatusOK, toJobListResponse(res))
}

// Filters serves the data behind the report's filter controls.
func (h *JobHandler) Filters(c *gin.Context) {
	opts, err := h.svc.FilterOptions(c.Request.Context())
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toJobFiltersResponse(opts))
}

// Query parameters accepted by the list endpoint.
var (
	jobSingleValueParams = []string{"from", "to", "q", "sort", "order", "limit", "cursor"}
	jobMultiValueParams  = []string{"status", "service_type_id", "lead_source_id"}
)

// parseJobQuery turns the raw query string into service params. See parseStrictQuery for what is rejected.
func parseJobQuery(rawQuery string) (service.ListJobsParams, error) {
	values, err := parseStrictQuery(rawQuery, jobSingleValueParams, jobMultiValueParams)
	if err != nil {
		return service.ListJobsParams{}, err
	}

	return service.ListJobsParams{
		From:           values.Get("from"),
		To:             values.Get("to"),
		Search:         values.Get("q"),
		Sort:           values.Get("sort"),
		Order:          values.Get("order"),
		Limit:          values.Get("limit"),
		Cursor:         values.Get("cursor"),
		Status:         flatten(values["status"]),
		ServiceTypeIDs: flatten(values["service_type_id"]),
		LeadSourceIDs:  flatten(values["lead_source_id"]),
	}, nil
}
