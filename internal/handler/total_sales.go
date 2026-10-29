package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/service"
)

// TotalSalesService is what the handler needs from the service layer. *service.TotalSalesService satisfies it.
type TotalSalesService interface {
	List(ctx context.Context, p service.ListTotalSalesParams) (*service.ListTotalSalesResult, error)
}

// TotalSalesHandler serves the Total Sales report. It is a RouteRegistrar, mounted under ReportsPrefix.
type TotalSalesHandler struct {
	svc    TotalSalesService
	logger *slog.Logger
}

// NewTotalSalesHandler returns the Total Sales report handler.
func NewTotalSalesHandler(svc TotalSalesService, logger *slog.Logger) *TotalSalesHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &TotalSalesHandler{svc: svc, logger: logger}
}

// Register mounts GET /total-sales and GET /total-sales/filters.
func (h *TotalSalesHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/total-sales", h.List)
	rg.GET("/total-sales/filters", h.Filters)
}

// List serves one page of the report.
func (h *TotalSalesHandler) List(c *gin.Context) {
	params, err := parseTotalSalesQuery(c.Request.URL.RawQuery)
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
	c.JSON(http.StatusOK, toTotalSaleListResponse(res))
}

// Filters serves the data behind the report's filter controls. It is fixed, so it needs no service call.
func (h *TotalSalesHandler) Filters(c *gin.Context) {
	c.JSON(http.StatusOK, totalSaleFiltersResponse())
}

// Query parameters accepted by the list endpoint.
var (
	totalSalesSingleParams = []string{"from", "to", "q", "sort", "order", "limit", "cursor"}
	totalSalesMultiParams  = []string{"status", "customer_id"}
)

// parseTotalSalesQuery turns the raw query string into service params. See parseStrictQuery for what is rejected.
func parseTotalSalesQuery(rawQuery string) (service.ListTotalSalesParams, error) {
	values, err := parseStrictQuery(rawQuery, totalSalesSingleParams, totalSalesMultiParams)
	if err != nil {
		return service.ListTotalSalesParams{}, err
	}

	return service.ListTotalSalesParams{
		From:        values.Get("from"),
		To:          values.Get("to"),
		Search:      values.Get("q"),
		Sort:        values.Get("sort"),
		Order:       values.Get("order"),
		Limit:       values.Get("limit"),
		Cursor:      values.Get("cursor"),
		Status:      flatten(values["status"]),
		CustomerIDs: flatten(values["customer_id"]),
	}, nil
}
