//go:build integration

package systememailreport_test

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

const (
	allowedOrigin = "https://app.example.com"
	listPath      = "/api/v1/reports/system-email-history"
)

// november6 is "now": the default range is then 2026-11-01 to 2026-11-30, the screen's range.
var november6 = time.Date(2026, 11, 6, 9, 30, 0, 0, time.UTC)

// newServer starts the real handler stack (middleware, router, handler, service, repository) behind a real HTTP
// listener, on a seeded database.
func newServer(t *testing.T) *httptest.Server {
	t.Helper()

	pool := testdb.SeededPool(t)
	logger := slog.New(slog.DiscardHandler)
	svc := service.NewSystemEmailService(repository.NewSystemEmailRepository(pool), func() time.Time { return november6 })
	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, []string{allowedOrigin}),
		Reports:    []handler.RouteRegistrar{handler.NewSystemEmailHandler(svc, logger)},
	})

	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	return srv
}

type response struct {
	status int
	header http.Header
	body   []byte
}

func get(t *testing.T, srv *httptest.Server, target string) response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+target, nil)
	require.NoError(t, err)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return response{status: resp.StatusCode, header: resp.Header, body: body}
}

func list(t *testing.T, srv *httptest.Server, query string) dto.SystemEmailListResponse {
	t.Helper()
	resp := get(t, srv, listPath+query)
	require.Equal(t, http.StatusOK, resp.status, "GET %s: %s", query, resp.body)
	var out dto.SystemEmailListResponse
	require.NoError(t, json.Unmarshal(resp.body, &out))
	return out
}

func ids(out dto.SystemEmailListResponse) []int64 {
	got := make([]int64, len(out.Data))
	for i, r := range out.Data {
		got[i] = r.ID
	}
	return got
}

func TestTheDefaultRequestIsTheScreensNovemberView(t *testing.T) {
	srv := newServer(t)

	resp := get(t, srv, listPath)

	require.Equal(t, http.StatusOK, resp.status)
	require.Equal(t, "no-store", resp.header.Get("Cache-Control"))
	require.NotEmpty(t, resp.header.Get("X-Request-ID"))
	require.JSONEq(t, `{
		"data": [
			{"id": 10, "customer": {"id": 8, "name": "PerfTest"}, "email": "no-reply@example.com",
			 "email_type": "estimate", "status": "confirmed", "time_stamp": "2026-11-04T08:00:00.000000Z"},
			{"id": 9, "customer": {"id": 2, "name": "Customer"}, "email": "billing.alt@example.com",
			 "email_type": "payment_receipt", "status": "confirmed", "time_stamp": "2026-11-03T12:00:00.000000Z"},
			{"id": 8, "customer": {"id": 20, "name": "#"}, "email": "d@cm.co",
			 "email_type": "welcome", "status": "unconfirmed", "time_stamp": "2026-11-02T03:42:00.000000Z"},
			{"id": 7, "customer": {"id": 5, "name": "#"}, "email": "no-reply@example.com",
			 "email_type": "appointment_confirmation", "status": "confirmed", "time_stamp": "2026-11-02T01:30:00.000000Z"}
		],
		"page": {"limit": 50, "has_more": false, "next_cursor": null},
		"range": {"from": "2026-11-01", "to": "2026-11-30"}
	}`, string(resp.body))
}

func TestAnEmptyRangeIsAnEmptyList(t *testing.T) {
	srv := newServer(t)

	resp := get(t, srv, listPath+"?from=2030-01-01&to=2030-01-31")

	require.Equal(t, http.StatusOK, resp.status)
	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 50, "has_more": false, "next_cursor": null},
		"range": {"from": "2030-01-01", "to": "2030-01-31"}
	}`, string(resp.body), "the screen's 'There is no data to display' case")
}

func TestNameFormatOverHTTP(t *testing.T) {
	srv := newServer(t)
	names := func(query string) []string {
		out := list(t, srv, query)
		got := make([]string, len(out.Data))
		for i, r := range out.Data {
			got[i] = r.Customer.Name
		}
		return got
	}

	require.Equal(t, []string{"PerfTest", "Customer", "#", "#"}, names("?name_format=first_name"))
	require.Equal(t, []string{"PerfTest", "1", "1", "D"}, names("?name_format=last_name"))
	require.Equal(t, []string{"PerfTest", "Customer 1", "#1", "# D"}, names("?name_format=full_name"))
	require.Equal(t, names("?name_format=first_name"), names(""), "the default is the first name, as on the screen")
}

func TestFiltersOverHTTP(t *testing.T) {
	srv := newServer(t)

	require.Equal(t, []int64{10, 9, 7}, ids(list(t, srv, "?status=confirmed")))
	require.Equal(t, []int64{8}, ids(list(t, srv, "?status=unconfirmed")))
	require.Empty(t, list(t, srv, "?status=cancelled").Data)
	require.Equal(t, []int64{9}, ids(list(t, srv, "?q=billing.alt")))
	require.Equal(t, []int64{9}, ids(list(t, srv, "?client_id=2")))
	require.Equal(t, []int64{6, 1}, ids(list(t, srv, "?from=2026-01-01&to=2026-12-31&status=cancelled")))
	require.Equal(t, []int64{2, 3, 4}, ids(list(t, srv, "?from=2026-10-01&to=2026-10-31&q=63714&sort=time_stamp&order=asc")),
		"the two emails at one instant are ordered by id")
}

func TestPagingOverHTTPWalksEveryRowOnce(t *testing.T) {
	srv := newServer(t)
	for _, order := range []string{"asc", "desc"} {
		for _, sort := range []string{"time_stamp", "customer", "email", "email_type", "status"} {
			base := "?from=2026-01-01&to=2026-12-31&sort=" + sort + "&order=" + order
			full := ids(list(t, srv, base+"&limit=100"))
			require.Len(t, full, 10)

			var walked []int64
			cursor := ""
			for pages := 0; pages < 12; pages++ {
				q := base + "&limit=3"
				if cursor != "" {
					q += "&cursor=" + url.QueryEscape(cursor)
				}
				page := list(t, srv, q)
				walked = append(walked, ids(page)...)
				if !page.Page.HasMore {
					require.Nil(t, page.Page.NextCursor)
					break
				}
				require.NotNil(t, page.Page.NextCursor)
				cursor = *page.Page.NextCursor
			}
			require.Equal(t, full, walked, "sort=%s order=%s", sort, order)
		}
	}
}

func TestInvalidRequestsGetFieldErrors(t *testing.T) {
	srv := newServer(t)

	tests := []struct {
		name   string
		query  string
		status int
		code   string
		field  string
	}{
		{"unknown status", "?status=sent", http.StatusUnprocessableEntity, "invalid_input", "status"},
		{"unknown name format", "?name_format=nickname", http.StatusUnprocessableEntity, "invalid_input", "name_format"},
		{"bad date", "?from=yesterday", http.StatusUnprocessableEntity, "invalid_input", "from"},
		{"to before from", "?from=2026-11-05&to=2026-11-01", http.StatusUnprocessableEntity, "invalid_input", "to"},
		{"client id not a number", "?client_id=abc", http.StatusUnprocessableEntity, "invalid_input", "client_id"},
		{"unsupported parameter", "?customer_id=1", http.StatusUnprocessableEntity, "invalid_input", "customer_id"},
		{"a cursor of another sort", "?sort=email&cursor=eyJrIjoic3RhdHVzIiwiZCI6ZmFsc2UsInYiOiJ4IiwiaWQiOjF9", http.StatusUnprocessableEntity, "invalid_input", "cursor"},
		{"malformed query string", "?q=%zz", http.StatusBadRequest, "invalid_request", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := get(t, srv, listPath+tt.query)

			require.Equal(t, tt.status, resp.status, string(resp.body))
			var body dto.ErrorResponse
			require.NoError(t, json.Unmarshal(resp.body, &body))
			require.Equal(t, tt.code, body.Error.Code)
			if tt.field != "" {
				require.Len(t, body.Error.Details, 1)
				require.Equal(t, tt.field, body.Error.Details[0].Field)
			}
		})
	}
}

func TestFiltersEndpoint(t *testing.T) {
	srv := newServer(t)

	resp := get(t, srv, listPath+"/filters")

	require.Equal(t, http.StatusOK, resp.status)
	var out dto.SystemEmailFiltersResponse
	require.NoError(t, json.Unmarshal(resp.body, &out))
	require.Equal(t, []string{"unconfirmed", "confirmed", "cancelled"}, out.Statuses)
	require.Len(t, out.EmailTypes, 7)
	require.Len(t, out.Clients, 20)
	require.Equal(t, []string{"first_name", "last_name", "full_name"}, out.NameFormats)
	require.Equal(t, []string{"time_stamp", "customer", "email", "email_type", "status"}, out.SortKeys)
	require.Equal(t, dto.SortResponse{Key: "time_stamp", Order: "desc"}, out.DefaultSort)
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
