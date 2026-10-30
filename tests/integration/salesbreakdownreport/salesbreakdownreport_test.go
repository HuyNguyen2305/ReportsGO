//go:build integration

package salesbreakdownreport_test

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

const listPath = "/api/v1/reports/sales-breakdown"

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

// newServer starts the real handler stack (middleware, router, handler, service, repository) behind a real HTTP
// listener, on a seeded database.
func newServer(t *testing.T) *httptest.Server {
	t.Helper()

	pool := testdb.SeededPool(t)
	logger := slog.New(slog.DiscardHandler)
	svc := service.NewSalesBreakdownService(repository.NewSalesBreakdownRepository(pool), func() time.Time { return october21 })
	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, nil),
		Reports:    []handler.RouteRegistrar{handler.NewSalesBreakdownHandler(svc, logger)},
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

func list(t *testing.T, srv *httptest.Server, query string) dto.SalesBreakdownListResponse {
	t.Helper()
	resp, body := get(t, srv, listPath+query)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	var out dto.SalesBreakdownListResponse
	require.NoError(t, json.Unmarshal(body, &out))
	return out
}

func TestForecastByClient(t *testing.T) {
	srv := newServer(t)

	resp, body := get(t, srv, listPath+"?type=forecast&by=client")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	require.NotEmpty(t, resp.Header.Get("X-Request-ID"))
	var out dto.SalesBreakdownListResponse
	require.NoError(t, json.Unmarshal(body, &out))
	require.Equal(t, 2026, out.Year)
	require.Equal(t, "forecast", out.Type)
	require.Equal(t, "client", out.By)
	require.Nil(t, out.Unassigned)
	require.Equal(t, "1831.26", out.Summary.Total)
	require.Len(t, out.Summary.Months, 12)
	for _, r := range out.Data {
		require.Len(t, r.Months, 12)
		require.NotNil(t, r.AccountNumber)
	}
}

func TestRevenueBySourceWithItsUnassignedLine(t *testing.T) {
	srv := newServer(t)

	out := list(t, srv, "?type=revenue&by=source")

	require.Len(t, out.Data, 1)
	require.Equal(t, "Facebook", out.Data[0].Name)
	require.Nil(t, out.Data[0].AccountNumber)
	require.Equal(t, &dto.SalesBreakdownUnassignedResponse{
		Label:  "Revenue w/o a source",
		Months: []string{"0.00", "0.00", "0.00", "0.00", "0.00", "0.00", "0.00", "0.00", "10.00", "658.80", "0.00", "0.00"},
		Total:  "668.80",
	}, out.Unassigned)
	require.Equal(t, "821.06", out.Summary.Total)

	t.Run("a group filter drops it", func(t *testing.T) {
		filtered := list(t, srv, "?type=revenue&by=source&q=face")
		require.Nil(t, filtered.Unassigned)
		require.Equal(t, "152.26", filtered.Summary.Total)
	})
}

func TestPagingAndMethods(t *testing.T) {
	srv := newServer(t)

	first := list(t, srv, "?type=revenue&by=client&sort=oct&order=desc&limit=2&method=cash,check,card,other")
	require.Len(t, first.Data, 2)
	require.True(t, first.Page.HasMore)
	require.Equal(t, "AAA1 ccc1", first.Data[0].Name)

	second := list(t, srv, "?type=revenue&by=client&sort=oct&order=desc&limit=2&method=cash,check,card,other&cursor="+*first.Page.NextCursor)
	require.Len(t, second.Data, 1)
	require.Nil(t, second.Summary)
}

func TestRejectsInvalidRequestsWithFieldDetails(t *testing.T) {
	srv := newServer(t)

	tests := []struct {
		name, query, field, msg string
	}{
		{"type is required", "?by=client", "type", "is required"},
		{"by is required", "?type=revenue", "by", "is required"},
		{"unknown type", "?type=budget&by=client", "type", "must be one of: forecast, revenue"},
		{"unknown by", "?type=revenue&by=region", "by", "must be one of: client, service, item, staff, source, tag"},
		{"bad year", "?type=revenue&by=client&year=26", "year", "must be a year between 2000 and 2100"},
		{"measure on the forecast", "?type=forecast&by=client&measure=collected", "measure", "can only be used with type revenue"},
		{"method on the forecast", "?type=forecast&by=client&method=card", "method", "can only be used with type revenue"},
		{"unknown measure", "?type=revenue&by=client&measure=invoiced", "measure", "must be one of: collected, collected_without_tax"},
		{"account number outside clients", "?type=revenue&by=staff&sort=account_number", "sort",
			"must be one of: name, jan, feb, mar, apr, may, jun, jul, aug, sep, oct, nov, dec, total"},
		{"bad group", "?type=revenue&by=staff&group=x", "group", "must be positive whole numbers"},
		{"unknown parameter", "?type=revenue&by=client&from=2026-01-01", "from", "is not a supported parameter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := get(t, srv, listPath+tt.query)

			require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
			require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
			var e dto.ErrorResponse
			require.NoError(t, json.Unmarshal(body, &e))
			require.Equal(t, "invalid_input", e.Error.Code)
			require.Equal(t, dto.ErrorDetail{Field: tt.field, Message: tt.msg}, e.Error.Details[0])
		})
	}

	t.Run("a malformed query string is a bad request", func(t *testing.T) {
		resp, _ := get(t, srv, listPath+"?type=%zz")
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

func TestFiltersEndpoint(t *testing.T) {
	srv := newServer(t)

	resp, body := get(t, srv, listPath+"/filters")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	var out dto.SalesBreakdownFiltersResponse
	require.NoError(t, json.Unmarshal(body, &out))
	require.Equal(t, []string{"forecast", "revenue"}, out.Types)
	require.Equal(t, []string{"client", "service", "item", "staff", "source", "tag"}, out.Bys)
	require.Equal(t, []string{"collected", "collected_without_tax"}, out.Measures)
	require.Len(t, out.Groups.Tag, 4)
	require.Len(t, out.Groups.Staff, 2)
	require.NotEmpty(t, out.Groups.Item)
	require.Equal(t, dto.YearsResponse{Min: 2000, Max: 2100}, out.Years)
	require.Equal(t, dto.SortResponse{Key: "name", Order: "asc"}, out.DefaultSort)
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
