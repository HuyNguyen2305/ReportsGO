//go:build integration

package noactivejobsreport_test

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

const (
	customersPath = "/api/v1/reports/customers-without-active-jobs"
	locationsPath = "/api/v1/reports/locations-without-active-jobs"
)

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

// newServer starts the real handler stack (middleware, router, handler, service, repository) behind a real HTTP
// listener, on a seeded database.
func newServer(t *testing.T) *httptest.Server {
	t.Helper()

	pool := testdb.SeededPool(t)
	logger := slog.New(slog.DiscardHandler)
	svc := service.NewNoActiveJobsService(repository.NewNoActiveJobsRepository(pool), func() time.Time { return october21 })
	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, nil),
		Reports:    []handler.RouteRegistrar{handler.NewNoActiveJobsHandler(svc, logger)},
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

func TestCustomersReport(t *testing.T) {
	srv := newServer(t)

	resp, body := get(t, srv, customersPath+"?limit=2")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	require.NotEmpty(t, resp.Header.Get("X-Request-ID"))
	var out dto.NoActiveJobsCustomersListResponse
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.Data, 2)
	require.True(t, out.Page.HasMore)
	require.NotNil(t, out.Page.NextCursor)
	require.Equal(t, &dto.NoActiveJobsSummaryResponse{Count: 12}, out.Summary)
	require.Equal(t, dto.NoActiveJobsCustomerResponse{
		ID: out.Data[0].ID, Status: "inactive", AccountNumber: "63716", Customer: "# D",
	}, out.Data[0])
	require.Nil(t, out.Data[0].LastService, "no job")

	t.Run("the next page has no summary", func(t *testing.T) {
		_, body := get(t, srv, customersPath+"?limit=2&cursor="+*out.Page.NextCursor)
		var next dto.NoActiveJobsCustomersListResponse
		require.NoError(t, json.Unmarshal(body, &next))
		require.Len(t, next.Data, 2)
		require.Nil(t, next.Summary)
	})

	t.Run("a customer's last service", func(t *testing.T) {
		_, body := get(t, srv, customersPath+"?q=5001")
		var one dto.NoActiveJobsCustomersListResponse
		require.NoError(t, json.Unmarshal(body, &one))
		require.Len(t, one.Data, 1)
		require.Equal(t, &dto.LastServiceResponse{ServiceType: "Bi-Monthly Service", JobStatus: "completed", Date: "2026-10-15"}, one.Data[0].LastService)
	})
}

func TestLocationsReport(t *testing.T) {
	srv := newServer(t)

	resp, body := get(t, srv, locationsPath+"?status=lead")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out dto.NoActiveJobsLocationsListResponse
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.Data, 1)
	require.Equal(t, int64(1), out.Summary.Count)
	row := out.Data[0]
	require.Equal(t, "lead", row.Status)
	require.Equal(t, "63730", row.AccountNumber)
	require.Equal(t, dto.ServiceAddressResponse{Line1: "1 Main St", Line2: "", City: "Austin", Zip: "73301"}, row.Address)
	require.NotZero(t, row.CustomerID)
}

func TestReportsRejectInvalidRequestsWithFieldDetails(t *testing.T) {
	srv := newServer(t)

	tests := []struct {
		name, path, query, field, msg string
	}{
		{"unknown status", customersPath, "?status=vip", "status", "must be one of: active, inactive, lead, archived"},
		{"customers have no city sort", customersPath, "?sort=city", "sort",
			"must be one of: status, account_number, customer, last_service_type, job_status, last_service_date"},
		{"locations have no total sort", locationsPath, "?sort=total", "sort",
			"must be one of: status, account_number, customer, address, city, zip, last_service_type, job_status, last_service_date"},
		{"unknown parameter", customersPath, "?from=2026-10-01", "from", "is not a supported parameter"},
		{"bad cursor", locationsPath, "?cursor=x", "cursor", "is not a valid cursor"},
		{"a cursor from the other sort", customersPath, "?sort=status&cursor=eyJrIjoiY3VzdG9tZXIiLCJkIjpmYWxzZSwidiI6ImEiLCJpZCI6MX0", "cursor", "does not match the requested sort"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := get(t, srv, tt.path+tt.query)

			require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
			require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
			var e dto.ErrorResponse
			require.NoError(t, json.Unmarshal(body, &e))
			require.Equal(t, "invalid_input", e.Error.Code)
			require.Equal(t, dto.ErrorDetail{Field: tt.field, Message: tt.msg}, e.Error.Details[0])
		})
	}

	t.Run("a malformed query string is a bad request", func(t *testing.T) {
		resp, _ := get(t, srv, customersPath+"?q=%zz")
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

func TestFiltersEndpoints(t *testing.T) {
	srv := newServer(t)

	_, customers := get(t, srv, customersPath+"/filters")
	require.JSONEq(t, `{
		"statuses": ["active", "inactive", "lead", "archived"],
		"sort_keys": ["status", "account_number", "customer", "last_service_type", "job_status", "last_service_date"],
		"default_sort": {"key": "customer", "order": "asc"},
		"limits": {"default": 50, "max": 200}
	}`, string(customers))

	resp, locations := get(t, srv, locationsPath+"/filters")
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	require.JSONEq(t, `{
		"statuses": ["active", "inactive", "lead", "archived"],
		"sort_keys": ["status", "account_number", "customer", "address", "city", "zip", "last_service_type", "job_status", "last_service_date"],
		"default_sort": {"key": "customer", "order": "asc"},
		"limits": {"default": 50, "max": 200}
	}`, string(locations))
}

func TestOnlyGetIsAllowed(t *testing.T) {
	srv := newServer(t)

	for _, path := range []string{customersPath, locationsPath} {
		req, err := http.NewRequest(http.MethodPost, srv.URL+path, nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()

		require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	}
}
