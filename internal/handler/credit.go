package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/service"
)

// CreditService is what the handler needs from the service layer. *service.CreditService satisfies it.
type CreditService interface {
	List(ctx context.Context, p service.ListCreditsParams) (*service.ListCreditsResult, error)
}

// CreditHandler serves the Credits report. It is a RouteRegistrar, mounted under ReportsPrefix.
type CreditHandler struct {
	svc    CreditService
	logger *slog.Logger
}

// NewCreditHandler returns the Credits report handler.
func NewCreditHandler(svc CreditService, logger *slog.Logger) *CreditHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &CreditHandler{svc: svc, logger: logger}
}

// Register mounts GET /credits and GET /credits/filters.
func (h *CreditHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/credits", h.List)
	rg.GET("/credits/filters", h.Filters)
}

// List serves one page of the report.
func (h *CreditHandler) List(c *gin.Context) {
	params, err := parseCreditQuery(c.Request.URL.RawQuery)
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
	c.JSON(http.StatusOK, toCreditListResponse(res))
}

// Filters serves the data behind the report's filter controls. The choices are fixed, so no service is involved.
func (h *CreditHandler) Filters(c *gin.Context) {
	c.JSON(http.StatusOK, creditFiltersResponse())
}

// Query parameters accepted by the list endpoint. Credits have no status and no Active/Deleted tab, so
// "status" and "lifecycle" are not among them and are rejected like any other unknown parameter.
var (
	creditSingleValueParams = []string{"from", "to", "q", "sort", "order", "limit", "cursor"}
	creditMultiValueParams  = []string{"method"}
)

// parseCreditQuery turns the raw query string into service params. See parseStrictQuery for what is rejected.
func parseCreditQuery(rawQuery string) (service.ListCreditsParams, error) {
	values, err := parseStrictQuery(rawQuery, creditSingleValueParams, creditMultiValueParams)
	if err != nil {
		return service.ListCreditsParams{}, err
	}

	return service.ListCreditsParams{
		From:   values.Get("from"),
		To:     values.Get("to"),
		Search: values.Get("q"),
		Sort:   values.Get("sort"),
		Order:  values.Get("order"),
		Limit:  values.Get("limit"),
		Cursor: values.Get("cursor"),
		Method: flatten(values["method"]),
	}, nil
}
