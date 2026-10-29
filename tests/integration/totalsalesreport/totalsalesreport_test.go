//go:build integration

package totalsalesreport_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
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

const listPath = "/api/v1/reports/total-sales"

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

// newServer starts the real handler stack (middleware, router, handler, service, repository) behind a real HTTP
// listener, on a seeded database.
func newServer(t *testing.T) *httptest.Server {
	t.Helper()

	pool := testdb.SeededPool(t)
	logger := slog.New(slog.DiscardHandler)
	svc := service.NewTotalSalesService(repository.NewTotalSalesRepository(pool), func() time.Time { return october21 })
	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, nil),
		Reports:    []handler.RouteRegistrar{handler.NewTotalSalesHandler(svc, logger)},
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

func list(t *testing.T, srv *httptest.Server, query string) dto.TotalSaleListResponse {
	t.Helper()
	resp, body := get(t, srv, listPath+query)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	var out dto.TotalSaleListResponse
	require.NoError(t, json.Unmarshal(body, &out))
	return out
}

func TestListReturnsTheFirstPageWithItsSummary(t *testing.T) {
	srv := newServer(t)

	resp, body := get(t, srv, listPath+"?limit=2")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	require.NotEmpty(t, resp.Header.Get("X-Request-ID"))

	var out dto.TotalSaleListResponse
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.Data, 2)
	require.True(t, out.Page.HasMore)
	require.NotNil(t, out.Page.NextCursor)
	require.Equal(t, &dto.TotalSaleSummaryResponse{Count: 12, Amount: "1075.06", WithoutTax: "911.36"}, out.Summary)
	require.Equal(t, dto.DateRangeResponse{From: ptr("2026-10-01"), To: ptr("2026-10-31")}, out.Range)
	require.Equal(t, "85031", out.Data[0].InvoiceNumber)
	require.Equal(t, "2026-10-22", out.Data[0].Date)
	require.Equal(t, "sent", out.Data[0].Status)
	require.Equal(t, "95.00", out.Data[0].Amount)
	require.Equal(t, "85.50", out.Data[0].WithoutTax)
}

func ptr(s string) *string { return &s }

func TestListFollowsTheCursorToTheEndWithoutRepeats(t *testing.T) {
	srv := newServer(t)

	seen := map[int64]bool{}
	target := "?limit=5&sort=customer"
	var summaries int
	for pages := 0; pages < 10; pages++ {
		out := list(t, srv, target)
		if out.Summary != nil {
			summaries++
		}
		for _, row := range out.Data {
			require.False(t, seen[row.ID], "invoice %d came twice", row.ID)
			seen[row.ID] = true
		}
		if !out.Page.HasMore {
			require.Nil(t, out.Page.NextCursor)
			break
		}
		target = "?limit=5&sort=customer&cursor=" + url.QueryEscape(*out.Page.NextCursor)
	}

	require.Len(t, seen, 12)
	require.Equal(t, 1, summaries)
}

func TestListFilters(t *testing.T) {
	srv := newServer(t)

	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"partial", "?status=partial", 1},
		{"paid", "?status=paid", 9},
		{"sent and partial, comma separated", "?status=sent,partial", 3},
		{"search by invoice number", "?q=85004", 1},
		{"search by customer", "?q=saliba", 5},
		{"a day", "?from=2026-10-12&to=2026-10-12", 2},
		{"september has no sales", "?from=2026-09-01&to=2026-09-30", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := list(t, srv, tt.query)
			require.Len(t, out.Data, tt.want)
			require.NotNil(t, out.Data, "data is always an array")
		})
	}
}

func TestListRejectsInvalidRequestsWithFieldDetails(t *testing.T) {
	srv := newServer(t)

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantField  string
	}{
		{"unknown status", "?status=void", http.StatusUnprocessableEntity, "status"},
		{"unsupported parameter", "?lifecycle=deleted", http.StatusUnprocessableEntity, "lifecycle"},
		{"limit out of range", "?limit=500", http.StatusUnprocessableEntity, "limit"},
		{"bad date", "?from=yesterday", http.StatusUnprocessableEntity, "from"},
		{"bad sort", "?sort=total", http.StatusUnprocessableEntity, "sort"},
		{"garbage cursor", "?cursor=zzz", http.StatusUnprocessableEntity, "cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := get(t, srv, listPath+tt.query)

			require.Equal(t, tt.wantStatus, resp.StatusCode)
			require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
			var e dto.ErrorResponse
			require.NoError(t, json.Unmarshal(body, &e))
			require.Equal(t, "invalid_input", e.Error.Code)
			require.Equal(t, tt.wantField, e.Error.Details[0].Field)
		})
	}

	t.Run("a malformed query string is a bad request", func(t *testing.T) {
		resp, _ := get(t, srv, listPath+"?q=%zz")
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

func TestFiltersEndpoint(t *testing.T) {
	srv := newServer(t)

	resp, body := get(t, srv, listPath+"/filters")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.JSONEq(t, `{
		"statuses": ["paid", "partial", "sent"],
		"sort_keys": ["invoice_number", "customer", "date", "status", "amount", "without_tax"],
		"default_sort": {"key": "date", "order": "desc"},
		"limits": {"default": 50, "max": 200}
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
