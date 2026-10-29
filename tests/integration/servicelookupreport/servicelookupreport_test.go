//go:build integration

package servicelookupreport_test

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

const listPath = "/api/v1/reports/service-lookup"

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

// newServer starts the real handler stack (middleware, router, handler, service, repository) behind a real HTTP
// listener, on a seeded database.
func newServer(t *testing.T) *httptest.Server {
	t.Helper()

	pool := testdb.SeededPool(t)
	logger := slog.New(slog.DiscardHandler)
	svc := service.NewServiceLookupService(repository.NewServiceLookupRepository(pool), func() time.Time { return october21 })
	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, nil),
		Reports:    []handler.RouteRegistrar{handler.NewServiceLookupHandler(svc, logger)},
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

func list(t *testing.T, srv *httptest.Server, query string) dto.ServiceLookupListResponse {
	t.Helper()
	resp, body := get(t, srv, listPath+query)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	var out dto.ServiceLookupListResponse
	require.NoError(t, json.Unmarshal(body, &out))
	return out
}

func TestListReturnsTheActiveTabWithItsSummary(t *testing.T) {
	srv := newServer(t)

	resp, body := get(t, srv, listPath+"?limit=200")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	require.NotEmpty(t, resp.Header.Get("X-Request-ID"))
	var out dto.ServiceLookupListResponse
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.Data, 31)
	require.Equal(t, dto.DateRangeResponse{From: ptr("2026-10-01"), To: ptr("2026-10-31")}, out.Range)
	require.Equal(t, int64(31), out.Summary.Count)
	require.Equal(t, dto.ServiceLookupTabCountsResponse{Active: 31, WorkPool: 3, Deleted: 1}, out.Summary.Tabs)

	row := out.Data[0]
	require.NotNil(t, row.ScheduledAt)
	require.Equal(t, "2026-10-01T04:30:00.000000Z", *row.ScheduledAt, "the earliest October job comes first")
	require.Equal(t, "AAA1 ccc1", row.Customer.Name)
	require.Equal(t, "Blank Service 123", row.ServiceType.Name)
	require.Equal(t, "confirmed", row.Status)
	require.Equal(t, "Miami-Dade County", row.Address.County)
	require.Equal(t, "0.00", row.InvoiceValue)
	require.NotNil(t, row.Tags)
}

func ptr(s string) *string { return &s }

func TestListPagingAndFilters(t *testing.T) {
	srv := newServer(t)

	first := list(t, srv, "?limit=10&sort=customer&q=william")
	require.Len(t, first.Data, 10)
	require.True(t, first.Page.HasMore)
	require.NotNil(t, first.Summary)

	second := list(t, srv, "?limit=10&sort=customer&q=william&cursor="+*first.Page.NextCursor)
	require.NotEmpty(t, second.Data)
	require.Nil(t, second.Summary)

	pool := list(t, srv, "?tab=work_pool")
	require.Len(t, pool.Data, 3)
	require.Equal(t, dto.DateRangeResponse{}, pool.Range, "the work pool has an open range")
	require.Equal(t, "285.00", pool.Summary.InvoiceValue)

	deleted := list(t, srv, "?tab=deleted&from=2026-10-22&to=2026-10-22&status=cancelled")
	require.Len(t, deleted.Data, 1)

	tagged := list(t, srv, "?tag=1&tag=2&status=confirmed,unconfirmed")
	require.NotEmpty(t, tagged.Data)
}

func TestListRejectsInvalidRequestsWithFieldDetails(t *testing.T) {
	srv := newServer(t)

	tests := []struct {
		name, query, field, msg string
	}{
		{"unknown tab", "?tab=archived", "tab", "must be one of: active, work_pool, deleted"},
		{"unknown status", "?status=done", "status", "must be one of: unconfirmed, confirmed, completed, cancelled"},
		{"bad date", "?from=yesterday", "from", "must be a date in YYYY-MM-DD format"},
		{"bad tag", "?tag=x", "tag", "must be positive whole numbers"},
		{"bad sort", "?sort=total", "sort",
			"must be one of: customer, service_type, scheduled_at, address, city, zip, county, invoice_value, without_tax, status, assigned_to"},
		{"unknown parameter", "?state=TX", "state", "is not a supported parameter"},
		{"bad cursor", "?cursor=x", "cursor", "is not a valid cursor"},
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
		resp, _ := get(t, srv, listPath+"?tab=%zz")
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

func TestFiltersEndpoint(t *testing.T) {
	srv := newServer(t)

	resp, body := get(t, srv, listPath+"/filters")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	var out dto.ServiceLookupFiltersResponse
	require.NoError(t, json.Unmarshal(body, &out))
	require.Equal(t, []string{"active", "work_pool", "deleted"}, out.Tabs)
	require.Equal(t, []string{"unconfirmed", "confirmed", "completed", "cancelled"}, out.Statuses)
	require.NotEmpty(t, out.ServiceTypes)
	require.Len(t, out.Staff, 2)
	require.Equal(t, []dto.OptionResponse{{ID: 2, Name: "Commercial"}, {ID: 4, Name: "Follow Up"}, {ID: 3, Name: "Residential"}, {ID: 1, Name: "VIP"}}, out.Tags)
	require.Equal(t, dto.SortResponse{Key: "scheduled_at", Order: "asc"}, out.DefaultSort)
	require.Equal(t, dto.PageLimitsResponse{Default: service.DefaultPageSize, Max: service.MaxPageSize}, out.Limits)
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
