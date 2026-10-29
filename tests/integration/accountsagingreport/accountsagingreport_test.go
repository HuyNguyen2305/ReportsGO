//go:build integration

package accountsagingreport_test

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

const listPath = "/api/v1/reports/accounts-aging"

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

// december1 is a day after every seeded date, for the tests that read balances weeks after October.
var december1 = time.Date(2026, 12, 1, 9, 30, 0, 0, time.UTC)

func clock(now []time.Time) time.Time {
	if len(now) > 0 {
		return now[0]
	}
	return october21
}

// newServer starts the real handler stack (middleware, router, handler, service, repository) behind a real HTTP
// listener, on a seeded database.
func newServer(t *testing.T, now ...time.Time) *httptest.Server {
	t.Helper()

	pool := testdb.SeededPool(t)
	logger := slog.New(slog.DiscardHandler)
	svc := service.NewAccountsAgingService(repository.NewAccountsAgingRepository(pool), func() time.Time { return clock(now) })
	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, nil),
		Reports:    []handler.RouteRegistrar{handler.NewAccountsAgingHandler(svc, logger)},
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

func list(t *testing.T, srv *httptest.Server, query string) dto.AgingListResponse {
	t.Helper()
	resp, body := get(t, srv, listPath+query)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	var out dto.AgingListResponse
	require.NoError(t, json.Unmarshal(body, &out))
	return out
}

func TestListReturnsTheCurrentReportWithItsSummary(t *testing.T) {
	srv := newServer(t)

	resp, body := get(t, srv, listPath)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	require.NotEmpty(t, resp.Header.Get("X-Request-ID"))

	var out dto.AgingListResponse
	require.NoError(t, json.Unmarshal(body, &out))
	require.Equal(t, "2026-10-21", out.AsOf)
	require.Len(t, out.Data, 2)
	require.False(t, out.Page.HasMore)
	require.Nil(t, out.Page.NextCursor)
	require.Equal(t, &dto.AgingSummaryResponse{
		Count: 2, D0to30: "169.00", D31to60: "0.00", D61to90: "0.00", D90Plus: "0.00", Total: "169.00", AvailableCredit: "300.00",
	}, out.Summary)

	row := out.Data[0]
	require.Equal(t, "AAA1 ccc1", row.Customer)
	require.NotNil(t, row.LocationID)
	require.NotNil(t, row.Location)
	require.Equal(t, "70.00", row.D0to30)
	require.Equal(t, "70.00", row.Total)
	require.Equal(t, "300.00", row.AvailableCredit)
	require.Equal(t, &dto.AgingCardResponse{Processor: "stripe", Last4: "3222", Expires: "12/2032"}, row.CreditCard)
	require.Nil(t, out.Data[1].CreditCard)
}

func TestListPointInTimeAndPaging(t *testing.T) {
	srv := newServer(t, december1)

	first := list(t, srv, "?as_of=2026-11-30&limit=2&sort=total&order=desc")
	require.Equal(t, "2026-11-30", first.AsOf)
	require.Len(t, first.Data, 2)
	require.True(t, first.Page.HasMore)
	require.NotNil(t, first.Page.NextCursor)
	require.NotNil(t, first.Summary)
	require.Equal(t, "99.00", first.Data[0].Total)

	second := list(t, srv, "?as_of=2026-11-30&limit=2&sort=total&order=desc&cursor="+*first.Page.NextCursor)
	require.Len(t, second.Data, 1)
	require.Equal(t, "20.00", second.Data[0].Total)
	require.Nil(t, second.Summary)
}

func TestListFilters(t *testing.T) {
	srv := newServer(t, december1)

	require.Len(t, list(t, srv, "?as_of=2026-11-30&days=30").Data, 3)
	require.Empty(t, list(t, srv, "?as_of=2026-11-30&days=60").Data)
	require.Len(t, list(t, srv, "?as_of=2026-10-21&q=saka").Data, 1)
	require.Empty(t, list(t, srv, "?service_type=999999").Data)
}

func TestListRejectsInvalidRequestsWithFieldDetails(t *testing.T) {
	srv := newServer(t)

	tests := []struct {
		name, query, field, msg string
	}{
		{"future as_of", "?as_of=2026-10-22", "as_of", "must not be in the future"},
		{"bad as_of", "?as_of=yesterday", "as_of", "must be a date in YYYY-MM-DD format"},
		{"days", "?days=45", "days", "must be one of: 30, 60, 90"},
		{"sort", "?sort=name", "sort", "must be one of: customer, total, d0_30, d31_60, d61_90, d90_plus, available_credit"},
		{"unknown parameter", "?from=2026-10-01", "from", "is not a supported parameter"},
		{"bad service type", "?service_type=abc", "service_type", "must be positive whole numbers"},
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
		resp, _ := get(t, srv, listPath+"?days=%zz")
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

func TestFiltersEndpoint(t *testing.T) {
	srv := newServer(t)

	resp, body := get(t, srv, listPath+"/filters")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	var out dto.AgingFiltersResponse
	require.NoError(t, json.Unmarshal(body, &out))
	require.Equal(t, []int{30, 60, 90}, out.Days)
	require.NotEmpty(t, out.ServiceTypes)
	require.Equal(t, []string{"customer", "total", "d0_30", "d31_60", "d61_90", "d90_plus", "available_credit"}, out.SortKeys)
	require.Equal(t, dto.SortResponse{Key: "customer", Order: "asc"}, out.DefaultSort)
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
