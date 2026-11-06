//go:build integration

package inboundleadreport_test

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
	listPath      = "/api/v1/reports/inbound-leads"
)

// november6 is "now": the default range is then 2026-11-01 to 2026-11-30, the screen's range.
var november6 = time.Date(2026, 11, 6, 9, 30, 0, 0, time.UTC)

// newServer starts the real handler stack (middleware, router, handler, service, repository) behind a real HTTP
// listener, on a seeded database with the extra leads of testdb.InsertInboundLeads.
func newServer(t *testing.T) (*httptest.Server, testdb.InboundLeadFixture) {
	t.Helper()

	pool := testdb.SeededPool(t)
	f := testdb.InsertInboundLeads(t, pool)
	logger := slog.New(slog.DiscardHandler)
	svc := service.NewInboundLeadService(repository.NewInboundLeadRepository(pool), func() time.Time { return november6 })
	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, []string{allowedOrigin}),
		Reports:    []handler.RouteRegistrar{handler.NewInboundLeadHandler(svc, logger)},
	})

	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	return srv, f
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

func list(t *testing.T, srv *httptest.Server, query string) dto.InboundLeadListResponse {
	t.Helper()
	resp := get(t, srv, listPath+query)
	require.Equal(t, http.StatusOK, resp.status, "GET %s: %s", query, resp.body)
	var out dto.InboundLeadListResponse
	require.NoError(t, json.Unmarshal(resp.body, &out))
	return out
}

func ids(out dto.InboundLeadListResponse) []int64 {
	got := make([]int64, len(out.Data))
	for i, r := range out.Data {
		got[i] = r.ID
	}
	return got
}

func TestTheDefaultRequestIsTheScreensNovemberView(t *testing.T) {
	srv, f := newServer(t)

	resp := get(t, srv, listPath)

	require.Equal(t, http.StatusOK, resp.status)
	require.Equal(t, "no-store", resp.header.Get("Cache-Control"))
	require.NotEmpty(t, resp.header.Get("X-Request-ID"))
	var out dto.InboundLeadListResponse
	require.NoError(t, json.Unmarshal(resp.body, &out))

	require.Equal(t, []int64{f.Noah, f.Cold}, ids(out), "the two leads added in November, newest first, the later id first on a tie")
	require.Equal(t, dto.PageResponse{Limit: 50}, out.Page)
	require.Equal(t, dto.DateRangeResponse{From: ptr("2026-11-01"), To: ptr("2026-11-30")}, out.Range)

	noah := out.Data[0]
	require.Equal(t, dto.CustomerRef{ID: f.Noah, Name: "Noah"}, noah.Customer)
	require.NotNil(t, noah.Source)
	require.Equal(t, "Facebook", noah.Source.Name)
	require.Equal(t, "noah.berg@example.com", *noah.Email)
	require.Equal(t, "(555) 010-0003", *noah.Phone)
	require.Equal(t, "Wants a Bi-Monthly Service", noah.Notes)
	require.Equal(t, "2026-11-03T09:15:30.123456Z", noah.DateAdded)
	require.NotNil(t, noah.ActiveJob)
	require.Equal(t, "2026-11-12T09:00:00.000000Z", noah.ActiveJob.ScheduledAt)

	cold := out.Data[1]
	require.Equal(t, dto.CustomerRef{ID: f.Cold, Name: "Cold Call Co"}, cold.Customer)
	require.Nil(t, cold.Source, "the screen's NA")
	require.Nil(t, cold.Email)
	require.Nil(t, cold.Phone)
	require.Equal(t, "", cold.Notes)
	require.Nil(t, cold.ActiveJob)
}

func ptr(s string) *string { return &s }

func TestTheJSONKeepsNullsAndNeverOmitsAField(t *testing.T) {
	srv, _ := newServer(t)

	resp := get(t, srv, listPath+"?from=2026-10-01&to=2026-10-31&sort=date_added&order=asc&limit=1")

	var raw struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp.body, &raw))
	require.Len(t, raw.Data, 1)
	row := raw.Data[0] // the seeded lead 63730
	for _, key := range []string{"id", "customer", "source", "email", "phone", "notes", "date_added", "active_job"} {
		require.Contains(t, row, key)
	}
	require.Nil(t, row["source"])
	require.Nil(t, row["email"])
	require.Nil(t, row["phone"])
	require.Nil(t, row["active_job"])
	require.Equal(t, "", row["notes"])
}

func TestAnEmptyRangeIsAnEmptyList(t *testing.T) {
	srv, _ := newServer(t)

	resp := get(t, srv, listPath+"?from=2030-01-01&to=2030-01-31")

	require.Equal(t, http.StatusOK, resp.status)
	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 50, "has_more": false, "next_cursor": null},
		"range": {"from": "2030-01-01", "to": "2030-01-31"}
	}`, string(resp.body), "the screen's 'There is no data to display' case")
}

func TestNameFormatOverHTTP(t *testing.T) {
	srv, _ := newServer(t)
	names := func(query string) []string {
		out := list(t, srv, query)
		got := make([]string, len(out.Data))
		for i, r := range out.Data {
			got[i] = r.Customer.Name
		}
		return got
	}

	require.Equal(t, []string{"Noah", "Cold Call Co"}, names("?name_format=first_name"))
	require.Equal(t, []string{"Berg", "Cold Call Co"}, names("?name_format=last_name"))
	require.Equal(t, []string{"Noah Berg", "Cold Call Co"}, names("?name_format=full_name"))
	require.Equal(t, names("?name_format=first_name"), names(""), "the default is the first name, as on the screen")
}

func TestFiltersOverHTTP(t *testing.T) {
	srv, f := newServer(t)
	year := "?from=2026-01-01&to=2026-12-31"

	require.Equal(t, []int64{f.Cold, f.QA}, ids(list(t, srv, year+"&source_id=none")), "NA")
	require.Equal(t, []int64{f.Noah, f.Mia}, ids(list(t, srv, year+"&source_id=1,2")), "Facebook and Google, by id")
	require.Equal(t, []int64{f.Noah, f.Cold, f.QA}, ids(list(t, srv, year+"&source_id=1&source_id=NONE")))
	require.Equal(t, []int64{f.Mia}, ids(list(t, srv, year+"&q=quote")))
	require.Equal(t, []int64{f.Mia}, ids(list(t, srv, year+"&q=mia.torres")))
	require.Equal(t, []int64{f.Mia, f.QA}, ids(list(t, srv, "?from=2026-10-01&to=2026-10-31")))
	require.Equal(t, []int64{f.QA, f.Mia}, ids(list(t, srv, "?from=2026-10-01&to=2026-10-31&sort=date_added&order=asc")))
}

func TestPagingOverHTTPWalksEveryRowOnce(t *testing.T) {
	srv, _ := newServer(t)
	for _, order := range []string{"asc", "desc"} {
		for _, sort := range []string{"customer", "source", "email", "phone", "notes", "date_added"} {
			base := "?from=2026-01-01&to=2026-12-31&sort=" + sort + "&order=" + order
			full := ids(list(t, srv, base+"&limit=100"))
			require.Len(t, full, 4)

			var walked []int64
			cursor := ""
			for pages := 0; pages < 8; pages++ {
				q := base + "&limit=1"
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
	srv, _ := newServer(t)

	tests := []struct {
		name   string
		query  string
		status int
		code   string
		field  string
	}{
		{"unknown name format", "?name_format=nickname", http.StatusUnprocessableEntity, "invalid_input", "name_format"},
		{"bad date", "?from=yesterday", http.StatusUnprocessableEntity, "invalid_input", "from"},
		{"to before from", "?from=2026-11-05&to=2026-11-01", http.StatusUnprocessableEntity, "invalid_input", "to"},
		{"source id not a number", "?source_id=abc", http.StatusUnprocessableEntity, "invalid_input", "source_id"},
		{"there is no year parameter", "?year=2026", http.StatusUnprocessableEntity, "invalid_input", "year"},
		{"time added is the same column", "?sort=time_added", http.StatusUnprocessableEntity, "invalid_input", "sort"},
		{"a cursor of another sort", "?sort=email&cursor=eyJrIjoibm90ZXMiLCJkIjpmYWxzZSwidiI6IngiLCJpZCI6MX0", http.StatusUnprocessableEntity, "invalid_input", "cursor"},
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
	srv, _ := newServer(t)

	resp := get(t, srv, listPath+"/filters")

	require.Equal(t, http.StatusOK, resp.status)
	var out dto.InboundLeadFiltersResponse
	require.NoError(t, json.Unmarshal(resp.body, &out))
	require.Len(t, out.Sources, 3)
	require.Equal(t, "Facebook", out.Sources[0].Name)
	require.Equal(t, []string{"first_name", "last_name", "full_name"}, out.NameFormats)
	require.Equal(t, []string{"customer", "source", "email", "phone", "notes", "date_added"}, out.SortKeys)
	require.Equal(t, dto.SortResponse{Key: "date_added", Order: "desc"}, out.DefaultSort)
}

func TestThereIsNoChartEndpoint(t *testing.T) {
	srv, _ := newServer(t)

	resp := get(t, srv, listPath+"/summary")

	require.Equal(t, http.StatusNotFound, resp.status)
}

func TestOnlyGetIsAllowed(t *testing.T) {
	srv, _ := newServer(t)
	req, err := http.NewRequest(http.MethodPost, srv.URL+listPath, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}
