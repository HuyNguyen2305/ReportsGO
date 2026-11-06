//go:build integration

package onlinebookingreport_test

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
	listPath      = "/api/v1/reports/online-bookings"
)

// november6 is "now": the default range is then 2026-11-01 to 2026-11-30, the screen's range.
var november6 = time.Date(2026, 11, 6, 9, 30, 0, 0, time.UTC)

// newServer starts the real handler stack (middleware, router, handler, service, repository) behind a real HTTP
// listener, on a seeded database with the extra customers and jobs of testdb.InsertOnlineBookings.
func newServer(t *testing.T) (*httptest.Server, testdb.OnlineBookingFixture) {
	t.Helper()

	pool := testdb.SeededPool(t)
	f := testdb.InsertOnlineBookings(t, pool)
	logger := slog.New(slog.DiscardHandler)
	svc := service.NewOnlineBookingService(repository.NewOnlineBookingRepository(pool), func() time.Time { return november6 })
	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, []string{allowedOrigin}),
		Reports:    []handler.RouteRegistrar{handler.NewOnlineBookingHandler(svc, logger)},
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

func list(t *testing.T, srv *httptest.Server, query string) dto.OnlineBookingListResponse {
	t.Helper()
	resp := get(t, srv, listPath+query)
	require.Equal(t, http.StatusOK, resp.status, "GET %s: %s", query, resp.body)
	var out dto.OnlineBookingListResponse
	require.NoError(t, json.Unmarshal(resp.body, &out))
	return out
}

func ids(out dto.OnlineBookingListResponse) []int64 {
	got := make([]int64, len(out.Data))
	for i, r := range out.Data {
		got[i] = r.ID
	}
	return got
}

func ptr(s string) *string { return &s }

func TestTheDefaultRequestIsTheScreensNovemberView(t *testing.T) {
	srv, f := newServer(t)

	resp := get(t, srv, listPath)

	require.Equal(t, http.StatusOK, resp.status)
	require.Equal(t, "no-store", resp.header.Get("Cache-Control"))
	require.NotEmpty(t, resp.header.Get("X-Request-ID"))
	var out dto.OnlineBookingListResponse
	require.NoError(t, json.Unmarshal(resp.body, &out))

	got := ids(out)
	require.Equal(t, []int64{f.EdgeLast, f.Cancelled, f.TieSecond, f.TieFirst, f.NinaJob}, got[:5],
		"latest appointment first, the later id first on a tie; then the seeded November bookings")
	require.Len(t, got, 7, "plus the seeded online customers' two jobs on November 2")
	require.NotContains(t, got, f.EdgeNext)
	require.NotContains(t, got, f.Deleted)
	require.NotContains(t, got, f.Unscheduled)
	require.NotContains(t, got, f.OfflineJob)
	require.Equal(t, dto.PageResponse{Limit: 50}, out.Page)
	require.Equal(t, dto.DateRangeResponse{From: ptr("2026-11-01"), To: ptr("2026-11-30")}, out.Range)

	tie := out.Data[3]
	require.Equal(t, f.TieFirst, tie.ID)
	require.Equal(t, dto.CustomerRef{ID: f.Olive, Name: "Olive"}, tie.Customer)
	require.Equal(t, "2026-11-10T09:00:00.123456Z", tie.ScheduledAt)
	require.Equal(t, "Bi-Monthly Service", tie.ServiceType.Name)
	require.Equal(t, "olive.ng@example.com", *tie.Email)
	require.Equal(t, "(555) 010-0011", *tie.Phone)
	require.Equal(t, dto.ServiceLookupAddressResponse{Line1: "11 Olive Way", City: "Round Rock", State: "TX", Zip: "73301", County: "Travis"}, tie.Address)
	require.Equal(t, "Gate code 1234", tie.Notes)
	require.Equal(t, "confirmed", tie.Status)
	require.Equal(t, "Google", tie.Source.Name)

	require.Equal(t, "cancelled", out.Data[1].Status, "a cancelled booking is listed with its status")
}

func TestTheJSONKeepsNullsAndNeverOmitsAField(t *testing.T) {
	srv, f := newServer(t)

	resp := get(t, srv, listPath+"?client_filter=x")
	require.Equal(t, http.StatusUnprocessableEntity, resp.status)

	resp = get(t, srv, listPath+"?source_id=none&limit=100")
	var raw struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp.body, &raw))
	var nina map[string]any
	for _, row := range raw.Data {
		if int64(row["id"].(float64)) == f.NinaJob {
			nina = row
		}
	}
	require.NotNil(t, nina)
	for _, key := range []string{"id", "customer", "scheduled_at", "service_type", "email", "phone", "address", "notes", "status", "source"} {
		require.Contains(t, nina, key)
	}
	require.Nil(t, nina["email"])
	require.Nil(t, nina["phone"])
	require.Nil(t, nina["source"])
	require.Equal(t, "", nina["notes"])
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
	srv, f := newServer(t)
	names := func(query string) map[int64]string {
		out := list(t, srv, query)
		got := map[int64]string{}
		for _, r := range out.Data {
			got[r.Customer.ID] = r.Customer.Name
		}
		return got
	}

	first, last, full := names("?name_format=first_name"), names("?name_format=last_name"), names("?name_format=full_name")
	require.Equal(t, "Olive", first[f.Olive])
	require.Equal(t, "Nina Nosource", first[f.Nina], "no first name: the display name")
	require.Equal(t, "Ng", last[f.Olive])
	require.Equal(t, "Olive Ng", full[f.Olive])
	require.Equal(t, first, names(""), "the default is the first name, as on the screen")
}

func TestFiltersOverHTTP(t *testing.T) {
	srv, f := newServer(t)
	year := "?from=2026-01-01&to=2026-12-31"

	oliveOnly := list(t, srv, year+"&q=olive.ng")
	for _, r := range oliveOnly.Data {
		require.Equal(t, f.Olive, r.Customer.ID)
	}
	require.Len(t, oliveOnly.Data, 5)

	none := list(t, srv, year+"&source_id=none")
	for _, r := range none.Data {
		require.Nil(t, r.Source, "none: only the customers without a source")
	}
	require.Contains(t, ids(none), f.NinaJob)

	require.Empty(t, list(t, srv, year+"&source_id=999999").Data)
	require.NotContains(t, ids(list(t, srv, year+"&q=ollie")), f.OfflineJob, "the offline customer never appears")
	require.Equal(t, []int64{f.NinaJob}, ids(list(t, srv, year+"&q=nina")))
}

func TestPagingOverHTTPWalksEveryRowOnce(t *testing.T) {
	srv, _ := newServer(t)
	for _, order := range []string{"asc", "desc"} {
		for _, sort := range []string{"customer", "scheduled_at", "service", "email", "phone", "address", "notes", "status"} {
			base := "?from=2026-01-01&to=2026-12-31&sort=" + sort + "&order=" + order
			full := ids(list(t, srv, base+"&limit=100"))
			require.GreaterOrEqual(t, len(full), 8)

			var walked []int64
			cursor := ""
			for pages := 0; pages < 40; pages++ {
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
		{"there is no status filter", "?status=confirmed", http.StatusUnprocessableEntity, "invalid_input", "status"},
		{"date and time are one column", "?sort=time", http.StatusUnprocessableEntity, "invalid_input", "sort"},
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
	var out dto.OnlineBookingFiltersResponse
	require.NoError(t, json.Unmarshal(resp.body, &out))
	require.Len(t, out.Sources, 3)
	require.Equal(t, "Facebook", out.Sources[0].Name)
	require.Equal(t, []string{"first_name", "last_name", "full_name"}, out.NameFormats)
	require.Equal(t, []string{"customer", "scheduled_at", "service", "email", "phone", "address", "notes", "status"}, out.SortKeys)
	require.Equal(t, dto.SortResponse{Key: "scheduled_at", Order: "desc"}, out.DefaultSort)
}

func TestThereIsNoChartEndpoint(t *testing.T) {
	srv, _ := newServer(t)

	require.Equal(t, http.StatusNotFound, get(t, srv, listPath+"/summary").status)
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
