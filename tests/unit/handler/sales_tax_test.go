package handler_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/dto"
	"reportsgo/internal/handler"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
	handlermocks "reportsgo/tests/mocks/handler"
)

const salesTaxPath = "/api/v1/reports/sales-tax-summary"

func salesTaxEngine(svc handler.SalesTaxService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewSalesTaxHandler(svc, logger)},
	})
}

func emptySalesTaxResult() *service.ListSalesTaxResult {
	return &service.ListSalesTaxResult{Items: []model.SalesTaxRow{}, Basis: model.SalesTaxCollected}
}

// salesTaxParamsFor performs GET /sales-tax-summary?query against a mocked service and returns the params it saw.
func salesTaxParamsFor(t *testing.T, query string) service.ListSalesTaxParams {
	t.Helper()

	svc := handlermocks.NewMockSalesTaxService(t)
	var got service.ListSalesTaxParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListSalesTaxParams) { got = p }).
		Return(emptySalesTaxResult(), nil).Once()

	rec := do(salesTaxEngine(svc), http.MethodGet, salesTaxPath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

func salesTaxNeverCalled(t *testing.T) *handlermocks.MockSalesTaxService {
	return handlermocks.NewMockSalesTaxService(t)
}

func TestSalesTaxListResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockSalesTaxService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListSalesTaxResult{
		Items: []model.SalesTaxRow{
			{ID: 1, Name: "VAT", Rate: "10.00", TaxableSales: 55322, TaxCollected: 5533, SurchargeTax: 292},
			{ID: 4, Name: "VAT2", Rate: "15.00"},
		},
		Summary: model.SalesTaxSummary{Count: 2, TaxableSales: 55322, TaxCollected: 5533, SurchargeTax: 292},
		Basis:   model.SalesTaxInvoiced,
		From:    time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		To:      time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(salesTaxEngine(svc), http.MethodGet, salesTaxPath+"?basis=invoiced", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{"id": 1, "name": "VAT", "taxable_sales": "553.22", "tax_rate": "10.00", "tax_collected": "55.33", "surcharge_tax": "2.92"},
			{"id": 4, "name": "VAT2", "taxable_sales": "0.00", "tax_rate": "15.00", "tax_collected": "0.00", "surcharge_tax": "0.00"}
		],
		"summary": {"count": 2, "taxable_sales": "553.22", "tax_collected": "55.33", "surcharge_tax": "2.92"},
		"range": {"from": "2026-10-01", "to": "2026-10-31"},
		"basis": "invoiced"
	}`, rec.Body.String())
}

func TestSalesTaxListResponseForAnEmptyReport(t *testing.T) {
	svc := handlermocks.NewMockSalesTaxService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListSalesTaxResult{
		Items: []model.SalesTaxRow{}, Basis: model.SalesTaxCollected, From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(salesTaxEngine(svc), http.MethodGet, salesTaxPath, "")

	require.JSONEq(t, `{
		"data": [],
		"summary": {"count": 0, "taxable_sales": "0.00", "tax_collected": "0.00", "surcharge_tax": "0.00"},
		"range": {"from": "2026-10-01", "to": null},
		"basis": "collected"
	}`, rec.Body.String())
}

func TestSalesTaxListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListSalesTaxParams
	}{
		{"no query", "", service.ListSalesTaxParams{}},
		{"every parameter", "?from=2026-10-01&to=2026-10-31&basis=collected&sort=name&order=desc&method=card",
			service.ListSalesTaxParams{From: "2026-10-01", To: "2026-10-31", Basis: "collected", Sort: "name", Order: "desc", Method: []string{"card"}}},
		{"repeated key", "?method=cash&method=check", service.ListSalesTaxParams{Method: []string{"cash", "check"}}},
		{"comma separated", "?method=cash,check", service.ListSalesTaxParams{Method: []string{"cash", "check"}}},
		{"whitespace and blanks are dropped", "?method=%20cash%20,,card,&method=", service.ListSalesTaxParams{Method: []string{"cash", "card"}}},
		{"empty single values stay empty strings", "?from=&basis=&sort=", service.ListSalesTaxParams{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, salesTaxParamsFor(t, tt.query))
		})
	}
}

func TestSalesTaxListRejectsUnsupportedAndDuplicatedParameters(t *testing.T) {
	unsupported := func(name string) []dto.ErrorDetail {
		return []dto.ErrorDetail{{Field: name, Message: "is not a supported parameter"}}
	}
	tests := []struct {
		name  string
		query string
		want  []dto.ErrorDetail
	}{
		{"the report has no paging: limit", "?limit=5", unsupported("limit")},
		{"the report has no paging: cursor", "?cursor=abc", unsupported("cursor")},
		{"the report has no search", "?q=vat", unsupported("q")},
		{"typo in a parameter name", "?methods=cash", unsupported("methods")},
		{"payment parameter status", "?status=paid", unsupported("status")},
		{"cache buster", "?_=1699999999", unsupported("_")},
		{"basis twice", "?basis=collected&basis=invoiced", []dto.ErrorDetail{{Field: "basis", Message: "must be given only once"}}},
		{"problems are listed in a stable order", "?zzz=1&aaa=2&sort=name&sort=tax_rate", []dto.ErrorDetail{
			{Field: "aaa", Message: "is not a supported parameter"},
			{Field: "sort", Message: "must be given only once"},
			{Field: "zzz", Message: "is not a supported parameter"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(salesTaxEngine(salesTaxNeverCalled(t)), http.MethodGet, salesTaxPath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, tt.want, body.Details)
		})
	}

	t.Run("a very long unknown name is truncated in the error", func(t *testing.T) {
		rec := do(salesTaxEngine(salesTaxNeverCalled(t)), http.MethodGet, salesTaxPath+"?"+strings.Repeat("x", 5000)+"=1", "")

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Equal(t, strings.Repeat("x", 64)+"...", decodeError(t, rec).Details[0].Field)
	})
}

func TestSalesTaxListRejectsMalformedQueryStrings(t *testing.T) {
	for _, query := range []string{"?basis=%zz", "?from=%", "?sort=a;b=c"} {
		t.Run(query, func(t *testing.T) {
			rec := do(salesTaxEngine(salesTaxNeverCalled(t)), http.MethodGet, salesTaxPath+query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestSalesTaxListMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation error", model.InvalidInput(model.FieldError{Field: "basis", Message: "must be one of: collected, invoiced"}), 422, "invalid_input"},
		{"wrapped validation error", fmt.Errorf("list: %w", model.InvalidInput(model.FieldError{Field: "method", Message: "x"})), 422, "invalid_input"},
		{"storage error", errors.New("pq: password authentication failed"), 500, "internal_error"},
		{"deadline exceeded", fmt.Errorf("list sales tax summary: %w", context.DeadlineExceeded), 504, "timeout"},
		{"client went away", fmt.Errorf("list sales tax summary: %w", context.Canceled), 499, "client_closed_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := handlermocks.NewMockSalesTaxService(t)
			svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, tt.err).Once()

			rec := do(salesTaxEngine(svc), http.MethodGet, salesTaxPath, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
			require.NotContains(t, rec.Body.String(), "password", "internal error text must not leak")
		})
	}

	t.Run("validation details reach the client", func(t *testing.T) {
		svc := handlermocks.NewMockSalesTaxService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil,
			model.InvalidInput(model.FieldError{Field: "method", Message: "can only be used with basis collected"})).Once()

		rec := do(salesTaxEngine(svc), http.MethodGet, salesTaxPath+"?basis=invoiced&method=card", "")

		require.Equal(t, []dto.ErrorDetail{{Field: "method", Message: "can only be used with basis collected"}}, decodeError(t, rec).Details)
	})
}

func TestSalesTaxRatesResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockSalesTaxService(t)
	svc.EXPECT().Rates(mock.Anything).Return([]model.TaxRateOption{
		{ID: 3, Name: "d12", Rate: "12.00", Active: true},
		{ID: 10, Name: "US", Rate: "30.00", Active: false},
	}, nil).Once()

	rec := do(salesTaxEngine(svc), http.MethodGet, salesTaxPath+"/rates", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"data": [
		{"id": 3, "name": "d12", "tax_rate": "12.00", "active": true},
		{"id": 10, "name": "US", "tax_rate": "30.00", "active": false}
	]}`, rec.Body.String())
}

func TestSalesTaxRatesWithNoRatesIsAnEmptyList(t *testing.T) {
	svc := handlermocks.NewMockSalesTaxService(t)
	svc.EXPECT().Rates(mock.Anything).Return([]model.TaxRateOption{}, nil).Once()

	rec := do(salesTaxEngine(svc), http.MethodGet, salesTaxPath+"/rates", "")

	require.JSONEq(t, `{"data": []}`, rec.Body.String())
}

func TestSalesTaxRatesRejectsAnyParameter(t *testing.T) {
	rec := do(salesTaxEngine(salesTaxNeverCalled(t)), http.MethodGet, salesTaxPath+"/rates?from=2026-10-01", "")

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Equal(t, []dto.ErrorDetail{{Field: "from", Message: "is not a supported parameter"}}, decodeError(t, rec).Details)

	rec = do(salesTaxEngine(salesTaxNeverCalled(t)), http.MethodGet, salesTaxPath+"/rates?q=%zz", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSalesTaxRatesMapsServiceErrors(t *testing.T) {
	svc := handlermocks.NewMockSalesTaxService(t)
	svc.EXPECT().Rates(mock.Anything).Return(nil, errors.New("pq: down")).Once()

	rec := do(salesTaxEngine(svc), http.MethodGet, salesTaxPath+"/rates", "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "internal_error", decodeError(t, rec).Code)
}

func TestSalesTaxFiltersResponseJSONContract(t *testing.T) {
	// the choices are fixed, so the service (a mock with no expectations) is never called
	rec := do(salesTaxEngine(salesTaxNeverCalled(t)), http.MethodGet, salesTaxPath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"bases": ["collected", "invoiced"],
		"methods": ["cash", "check", "card", "other"],
		"sort_keys": ["name", "taxable_sales", "tax_rate", "tax_collected", "surcharge_tax"],
		"default_sort": {"key": "taxable_sales", "order": "desc"}
	}`, rec.Body.String())
}

func TestSalesTaxListPassesTheRequestContextToTheService(t *testing.T) {
	type key struct{}
	svc := handlermocks.NewMockSalesTaxService(t)
	svc.EXPECT().List(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(key{}) == "marker" }), mock.Anything).
		Return(emptySalesTaxResult(), nil).Once()

	req := httptest.NewRequest(http.MethodGet, salesTaxPath, nil).
		WithContext(context.WithValue(context.Background(), key{}, "marker"))
	rec := httptest.NewRecorder()
	salesTaxEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestSalesTaxResponsesAreNeverCached(t *testing.T) {
	setRequestID := func(c *gin.Context) { c.Header("X-Request-ID", "rid-7"); c.Next() }

	t.Run("list", func(t *testing.T) {
		svc := handlermocks.NewMockSalesTaxService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(emptySalesTaxResult(), nil).Once()

		rec := do(salesTaxEngine(svc, setRequestID), http.MethodGet, salesTaxPath, "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		require.Equal(t, "rid-7", rec.Header().Get("X-Request-ID"))
	})

	t.Run("validation error", func(t *testing.T) {
		rec := do(salesTaxEngine(salesTaxNeverCalled(t), setRequestID), http.MethodGet, salesTaxPath+"?nope=1", "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "rid-7", decodeError(t, rec).RequestID)
	})

	t.Run("rates", func(t *testing.T) {
		svc := handlermocks.NewMockSalesTaxService(t)
		svc.EXPECT().Rates(mock.Anything).Return([]model.TaxRateOption{}, nil).Once()

		rec := do(salesTaxEngine(svc), http.MethodGet, salesTaxPath+"/rates", "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})

	t.Run("filters", func(t *testing.T) {
		rec := do(salesTaxEngine(salesTaxNeverCalled(t)), http.MethodGet, salesTaxPath+"/filters", "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})
}

func TestSalesTaxRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantCode   string
	}{
		{"POST list", http.MethodPost, salesTaxPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"POST rates", http.MethodPost, salesTaxPath + "/rates", http.StatusMethodNotAllowed, "method_not_allowed"},
		{"POST filters", http.MethodPost, salesTaxPath + "/filters", http.StatusMethodNotAllowed, "method_not_allowed"},
		{"unknown subpath", http.MethodGet, salesTaxPath + "/nope", http.StatusNotFound, "not_found"},
		{"not under the reports prefix", http.MethodGet, "/sales-tax-summary", http.StatusNotFound, "not_found"},
		{"the payments report is not served by this handler", http.MethodGet, paymentsPath, http.StatusNotFound, "not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(salesTaxEngine(salesTaxNeverCalled(t)), tt.method, tt.target, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
		})
	}
}

func TestNewSalesTaxHandlerAcceptsANilLogger(t *testing.T) {
	require.NotNil(t, handler.NewSalesTaxHandler(salesTaxNeverCalled(t), nil))
}
