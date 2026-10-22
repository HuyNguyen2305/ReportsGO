package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/service"
)

// DocumentService is what the handler needs from the service layer. *service.DocumentService satisfies it.
type DocumentService interface {
	List(ctx context.Context, p service.ListDocumentsParams) (*service.ListDocumentsResult, error)
	FilterOptions(ctx context.Context) (*service.DocumentFilterOptions, error)
}

// DocumentHandler serves the Documents report. It is a RouteRegistrar, mounted under ReportsPrefix.
type DocumentHandler struct {
	svc    DocumentService
	logger *slog.Logger
}

// NewDocumentHandler returns the Documents report handler.
func NewDocumentHandler(svc DocumentService, logger *slog.Logger) *DocumentHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &DocumentHandler{svc: svc, logger: logger}
}

// Register mounts GET /documents and GET /documents/filters.
func (h *DocumentHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/documents", h.List)
	rg.GET("/documents/filters", h.Filters)
}

// List serves one page of the report.
func (h *DocumentHandler) List(c *gin.Context) {
	params, err := parseDocumentQuery(c.Request.URL.RawQuery)
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
	c.JSON(http.StatusOK, toDocumentListResponse(res))
}

// Filters serves the data behind the report's filter controls.
func (h *DocumentHandler) Filters(c *gin.Context) {
	opts, err := h.svc.FilterOptions(c.Request.Context())
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toDocumentFiltersResponse(opts))
}

// Query parameters accepted by the list endpoint.
var (
	documentSingleValueParams = []string{"from", "to", "lifecycle", "q", "sort", "order", "limit", "cursor"}
	documentMultiValueParams  = []string{"status", "document_type_id"}
)

// parseDocumentQuery turns the raw query string into service params. See parseStrictQuery for what is rejected.
func parseDocumentQuery(rawQuery string) (service.ListDocumentsParams, error) {
	values, err := parseStrictQuery(rawQuery, documentSingleValueParams, documentMultiValueParams)
	if err != nil {
		return service.ListDocumentsParams{}, err
	}

	return service.ListDocumentsParams{
		From:            values.Get("from"),
		To:              values.Get("to"),
		Lifecycle:       values.Get("lifecycle"),
		Search:          values.Get("q"),
		Sort:            values.Get("sort"),
		Order:           values.Get("order"),
		Limit:           values.Get("limit"),
		Cursor:          values.Get("cursor"),
		Status:          flatten(values["status"]),
		DocumentTypeIDs: flatten(values["document_type_id"]),
	}, nil
}
