//go:build integration

package salestaxreport_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/dto"
	"reportsgo/internal/handler"
	"reportsgo/internal/middleware"
	"reportsgo/internal/repository"
	"reportsgo/internal/service"
	"reportsgo/tests/integration/testdb"
)

func init() { gin.SetMode(gin.TestMode) }

const listPath = "/api/v1/reports/sales-tax-summary"

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

// newServer starts the real handler stack (middleware, router, handler, service, repository) behind a real HTTP
// listener, on a seeded database.
func newServer(t *testing.T) *httptest.Server {
	t.Helper()

	pool := testdb.SeededPool(t)
	logger := slog.New(slog.DiscardHandler)
	svc := service.NewSalesTaxService(repository.NewSalesTaxRepository(pool), func() time.Time { return october21 })
	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, nil),
		Reports:    []handler.RouteRegistrar{handler.NewSalesTaxHandler(svc, logger)},
	})
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, srv *httptest.Server, target string) (*http.Response, []byte) {
	t.Helper()
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(srv.URL + target)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, body
}

func list(t *testing.T, srv *httptest.Server, query string) dto.SalesTaxListResponse {
	t.Helper()
	resp, body := get(t, srv, listPath+query)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	var out dto.SalesTaxListResponse
	require.NoError(t, json.Unmarshal(body, &out))
	return out
}

func ptr(s string) *string { return &s }

func TestListReturnsTheWholeReportWithItsSummary(t *testing.T) {
	srv := newServer(t)

	resp, body := get(t, srv, listPath)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	require.NotEmpty(t, resp.Header.Get("X-Request-ID"))

	var out dto.SalesTaxListResponse
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.Data, 10)
	require.Equal(t, "collected", out.Basis)
	require.Equal(t, dto.DateRangeResponse{From: ptr("2026-10-01"), To: ptr("2026-10-31")}, out.Range)
	require.Equal(t, dto.SalesTaxSummaryResponse{Count: 10, TaxableSales: "721.50", TaxCollected: "117.78", SurchargeTax: "2.92"}, out.Summary)
	require.Equal(t, dto.SalesTaxResponse{
		ID: 1, Name: "VAT", TaxableSales: "420.00", TaxRate: "10.00", TaxCollected: "42.00", SurchargeTax: "2.92",
	}, out.Data[0])
	require.Equal(t, dto.SalesTaxResponse{
		ID: 4, Name: "VAT2", TaxableSales: "0.00", TaxRate: "15.00", TaxCollected: "0.00", SurchargeTax: "0.00",
	}, out.Data[3], "an idle tax is listed with zeros")
}

func TestListInvoicedBasis(t *testing.T) {
	srv := newServer(t)

	out := list(t, srv, "?basis=invoiced")

	require.Equal(t, "invoiced", out.Basis)
	require.Equal(t, dto.SalesTaxSummaryResponse{Count: 10, TaxableSales: "971.50", TaxCollected: "163.70", SurchargeTax: "2.92"}, out.Summary)
}

func TestListFiltersAndSorts(t *testing.T) {
	srv := newServer(t)

	tests := []struct {
		name      string
		query     string
		wantFirst string
		wantTax   string
	}{
		{"card payments only", "?method=card", "VN", "66.00"},
		{"sorted by name", "?sort=name", "d11", ""},
		{"highest rate first", "?sort=tax_rate&order=desc", "VN", ""},
		{"most tax first", "?sort=tax_collected&order=desc", "VN", "66.00"},
		{"september", "?from=2026-09-01&to=2026-09-30", "VAT", "1.00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := list(t, srv, tt.query)
			require.NotEmpty(t, out.Data)
			require.Equal(t, tt.wantFirst, out.Data[0].Name)
			if tt.wantTax != "" {
				require.Equal(t, tt.wantTax, out.Data[0].TaxCollected)
			}
		})
	}
}

func TestListRejectsInvalidRequestsWithFieldDetails(t *testing.T) {
	srv := newServer(t)

	tests := []struct {
		name      string
		query     string
		wantField string
		wantMsg   string
	}{
		{"unknown basis", "?basis=cash", "basis", "must be one of: collected, invoiced"},
		{"methods with the invoiced basis", "?basis=invoiced&method=card", "method", "can only be used with basis collected"},
		{"unknown method", "?method=wire", "method", "must be one of: cash, check, card, other"},
		{"bad date", "?from=yesterday", "from", "must be a date in YYYY-MM-DD format"},
		{"bad sort", "?sort=total", "sort", "must be one of: name, taxable_sales, tax_rate, tax_collected, surcharge_tax"},
		{"the report has no paging: limit", "?limit=5", "limit", "is not a supported parameter"},
		{"the report has no paging: cursor", "?cursor=x", "cursor", "is not a supported parameter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := get(t, srv, listPath+tt.query)

			require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
			require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
			var e dto.ErrorResponse
			require.NoError(t, json.Unmarshal(body, &e))
			require.Equal(t, "invalid_input", e.Error.Code)
			require.Equal(t, dto.ErrorDetail{Field: tt.wantField, Message: tt.wantMsg}, e.Error.Details[0])
		})
	}

	t.Run("a malformed query string is a bad request", func(t *testing.T) {
		resp, _ := get(t, srv, listPath+"?basis=%zz")
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

func TestRatesEndpoint(t *testing.T) {
	srv := newServer(t)

	resp, body := get(t, srv, listPath+"/rates")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	var out dto.TaxRatesResponse
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.Data, 10)
	byName := map[string]dto.TaxRateResponse{}
	for _, r := range out.Data {
		byName[r.Name] = r
	}
	require.Equal(t, dto.TaxRateResponse{ID: 1, Name: "VAT", TaxRate: "10.00", Active: true}, byName["VAT"])
	require.Equal(t, "0.00", byName["Tax 32"].TaxRate)

	t.Run("it takes no parameters", func(t *testing.T) {
		resp, _ := get(t, srv, listPath+"/rates?from=2026-10-01")
		require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	})
}

func TestFiltersEndpoint(t *testing.T) {
	srv := newServer(t)

	resp, body := get(t, srv, listPath+"/filters")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.JSONEq(t, `{
		"bases": ["collected", "invoiced"],
		"methods": ["cash", "check", "card", "other"],
		"sort_keys": ["name", "taxable_sales", "tax_rate", "tax_collected", "surcharge_tax"],
		"default_sort": {"key": "taxable_sales", "order": "desc"}
	}`, string(body))
}

func TestOnlyGetIsAllowed(t *testing.T) {
	srv := newServer(t)

	req, err := http.NewRequest(http.MethodPost, srv.URL+listPath, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}
