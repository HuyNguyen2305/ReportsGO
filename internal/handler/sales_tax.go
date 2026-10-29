package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

// SalesTaxService is what the handler needs from the service layer. *service.SalesTaxService satisfies it.
type SalesTaxService interface {
	List(ctx context.Context, p service.ListSalesTaxParams) (*service.ListSalesTaxResult, error)
	Rates(ctx context.Context) ([]model.TaxRateOption, error)
}

// SalesTaxHandler serves the Sales Tax Summary. It is a RouteRegistrar, mounted under ReportsPrefix.
type SalesTaxHandler struct {
	svc    SalesTaxService
	logger *slog.Logger
}

// NewSalesTaxHandler returns the Sales Tax Summary handler.
func NewSalesTaxHandler(svc SalesTaxService, logger *slog.Logger) *SalesTaxHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &SalesTaxHandler{svc: svc, logger: logger}
}

// Register mounts GET /sales-tax-summary, GET /sales-tax-summary/rates and GET /sales-tax-summary/filters.
func (h *SalesTaxHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/sales-tax-summary", h.List)
	rg.GET("/sales-tax-summary/rates", h.Rates)
	rg.GET("/sales-tax-summary/filters", h.Filters)
}

// List serves the Sales Tax tab.
func (h *SalesTaxHandler) List(c *gin.Context) {
	params, err := parseSalesTaxQuery(c.Request.URL.RawQuery)
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
	c.JSON(http.StatusOK, toSalesTaxListResponse(res))
}

// Rates serves the Tax Rate tab. It takes no parameters, so any query string is rejected.
func (h *SalesTaxHandler) Rates(c *gin.Context) {
	if _, err := parseStrictQuery(c.Request.URL.RawQuery, nil, nil); err != nil {
		if errors.Is(err, errMalformedQuery) {
			WriteError(c, http.StatusBadRequest, "invalid_request", "query string is malformed")
			return
		}
		RespondError(c, h.logger, err)
		return
	}

	rates, err := h.svc.Rates(c.Request.Context())
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toTaxRatesResponse(rates))
}

// Filters serves the data behind the report's filter controls. It is fixed, so it needs no service call.
func (h *SalesTaxHandler) Filters(c *gin.Context) {
	c.JSON(http.StatusOK, salesTaxFiltersResponse())
}

// Query parameters accepted by the list endpoint.
var (
	salesTaxSingleParams = []string{"from", "to", "basis", "sort", "order"}
	salesTaxMultiParams  = []string{"method"}
)

// parseSalesTaxQuery turns the raw query string into service params. See parseStrictQuery for what is rejected.
func parseSalesTaxQuery(rawQuery string) (service.ListSalesTaxParams, error) {
	values, err := parseStrictQuery(rawQuery, salesTaxSingleParams, salesTaxMultiParams)
	if err != nil {
		return service.ListSalesTaxParams{}, err
	}

	return service.ListSalesTaxParams{
		From:   values.Get("from"),
		To:     values.Get("to"),
		Basis:  values.Get("basis"),
		Sort:   values.Get("sort"),
		Order:  values.Get("order"),
		Method: flatten(values["method"]),
	}, nil
}
