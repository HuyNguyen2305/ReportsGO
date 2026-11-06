//go:build integration

package materialusereport_test

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
	listPath      = "/api/v1/reports/material-use"
)

// november6 is "now": the default range is then 2026-11-01 to 2026-11-30, the screen's range.
var november6 = time.Date(2026, 11, 6, 9, 30, 0, 0, time.UTC)

// newServer starts the real handler stack (middleware, router, handler, service, repository) behind a real HTTP
// listener, on a seeded database.
func newServer(t *testing.T) *httptest.Server {
	t.Helper()

	pool := testdb.SeededPool(t)
	logger := slog.New(slog.DiscardHandler)
	svc := service.NewMaterialUseService(repository.NewMaterialUseRepository(pool), func() time.Time { return november6 })
	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, []string{allowedOrigin}),
		Reports:    []handler.RouteRegistrar{handler.NewMaterialUseHandler(svc, logger)},
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

func list(t *testing.T, srv *httptest.Server, query string) dto.MaterialUseListResponse {
	t.Helper()
	resp := get(t, srv, listPath+query)
	require.Equal(t, http.StatusOK, resp.status, "GET %s: %s", query, resp.body)
	var out dto.MaterialUseListResponse
	require.NoError(t, json.Unmarshal(resp.body, &out))
	return out
}

func names(out dto.MaterialUseListResponse) []string {
	got := make([]string, len(out.Data))
	for i, r := range out.Data {
		got[i] = r.Material
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
			{"material_id": 1, "material": "Demand CS",   "epa": "100-1066", "qty": "4.250", "applications": 3},
			{"material_id": 4, "material": "Glue Boards", "epa": null,       "qty": "4.000", "applications": 1},
			{"material_id": 2, "material": "Temprid SC",  "epa": "432-1363", "qty": "3.750", "applications": 2}
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

func TestTabsOverHTTP(t *testing.T) {
	srv := newServer(t)

	require.Equal(t, []string{"Demand CS", "Glue Boards", "Temprid SC"}, names(list(t, srv, "?tab=jobs")))
	devices := list(t, srv, "?tab=devices")
	require.Equal(t, []string{"Demand CS", "Temprid SC"}, names(devices))
	require.Equal(t, "0.500", devices.Data[0].Qty)
	require.Equal(t, "0.750", devices.Data[1].Qty)
}

func TestFiltersOverHTTP(t *testing.T) {
	srv := newServer(t)

	require.Equal(t, []string{"Temprid SC"}, names(list(t, srv, "?material_id=2")))
	require.Equal(t, []string{"Demand CS", "Temprid SC"}, names(list(t, srv, "?q=1")), "EPA numbers 100-1066 and 432-1363 contain a 1")
	require.Equal(t, []string{"Demand CS", "Gentrol IGR", "Glue Boards", "Temprid SC"}, names(list(t, srv, "?from=2026-01-01&to=2026-12-31")))
	require.Empty(t, list(t, srv, "?client_id=-5").Data)
}

func TestPagingOverHTTPWalksEveryRowOnce(t *testing.T) {
	srv := newServer(t)
	for _, order := range []string{"asc", "desc"} {
		for _, sort := range []string{"material", "epa", "qty", "applications"} {
			full := names(list(t, srv, "?from=2026-01-01&to=2026-12-31&sort="+sort+"&order="+order+"&limit=100"))
			require.Len(t, full, 4)

			var walked []string
			cursor := ""
			for pages := 0; pages < 6; pages++ {
				q := "?from=2026-01-01&to=2026-12-31&sort=" + sort + "&order=" + order + "&limit=1"
				if cursor != "" {
					q += "&cursor=" + url.QueryEscape(cursor)
				}
				page := list(t, srv, q)
				walked = append(walked, names(page)...)
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
		{"unknown tab", "?tab=gadgets", http.StatusUnprocessableEntity, "invalid_input", "tab"},
		{"bad date", "?from=yesterday", http.StatusUnprocessableEntity, "invalid_input", "from"},
		{"to before from", "?from=2026-11-05&to=2026-11-01", http.StatusUnprocessableEntity, "invalid_input", "to"},
		{"client id not a number", "?client_id=abc", http.StatusUnprocessableEntity, "invalid_input", "client_id"},
		{"unsupported parameter", "?customer_id=1", http.StatusUnprocessableEntity, "invalid_input", "customer_id"},
		{"a cursor of another sort", "?sort=qty&cursor=eyJrIjoibWF0ZXJpYWwiLCJkIjpmYWxzZSwidiI6IngiLCJpZCI6MX0", http.StatusUnprocessableEntity, "invalid_input", "cursor"},
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
	var out dto.MaterialUseFiltersResponse
	require.NoError(t, json.Unmarshal(resp.body, &out))
	require.Len(t, out.Materials, 4)
	require.Equal(t, "Demand CS", out.Materials[0].Name)
	require.Len(t, out.Clients, 20)
	require.Equal(t, []string{"all", "jobs", "devices"}, out.Tabs)
	require.Equal(t, []string{"material", "epa", "qty", "applications"}, out.SortKeys)
	require.Equal(t, dto.SortResponse{Key: "material", Order: "asc"}, out.DefaultSort)
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
