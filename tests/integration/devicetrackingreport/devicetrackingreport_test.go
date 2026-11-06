//go:build integration

package devicetrackingreport_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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
	listPath      = "/api/v1/reports/device-tracking"
)

// november6 is "now": the default range is then 2026-11-01 to 2026-11-30, the screen's range.
var november6 = time.Date(2026, 11, 6, 9, 30, 0, 0, time.UTC)

// newServer starts the real handler stack (middleware, router, handler, service, repository) behind a real HTTP
// listener, on a seeded database with the extra customers, devices, jobs and checks of testdb.InsertDeviceTracking.
func newServer(t *testing.T) (*httptest.Server, testdb.DeviceTrackingFixture) {
	t.Helper()

	pool := testdb.SeededPool(t)
	f := testdb.InsertDeviceTracking(t, pool)
	logger := slog.New(slog.DiscardHandler)
	svc := service.NewDeviceTrackingService(repository.NewDeviceTrackingRepository(pool), func() time.Time { return november6 })
	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, []string{allowedOrigin}),
		Reports:    []handler.RouteRegistrar{handler.NewDeviceTrackingHandler(svc, logger)},
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

func list(t *testing.T, srv *httptest.Server, query string) dto.DeviceTrackingListResponse {
	t.Helper()
	resp := get(t, srv, listPath+query)
	require.Equal(t, http.StatusOK, resp.status, "GET %s: %s", query, resp.body)
	var out dto.DeviceTrackingListResponse
	require.NoError(t, json.Unmarshal(resp.body, &out))
	return out
}

func ids(out dto.DeviceTrackingListResponse) []int64 {
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
	var out dto.DeviceTrackingListResponse
	require.NoError(t, json.Unmarshal(resp.body, &out))

	got := ids(out)
	require.Equal(t, []int64{f.EdgeLastGate, f.CancelledGate, f.Tie2Beside, f.Tie1NoName, f.Tie1Gate, f.OtherCheck}, got[:6],
		"latest appointment first, the later check id first on a tie; then the seeded checks of November 2")
	require.Len(t, got, 10, "plus the four seeded checks on the November 2 jobs")
	require.NotContains(t, got, f.EdgeNextGate)
	require.NotContains(t, got, f.HiddenDeleted)
	require.NotContains(t, got, f.HiddenUnscheduled)
	require.Equal(t, dto.PageResponse{Limit: 50}, out.Page)
	require.Equal(t, dto.DateRangeResponse{From: ptr("2026-11-01"), To: ptr("2026-11-30")}, out.Range)

	full := out.Data[4]
	require.Equal(t, f.Tie1Gate, full.ID)
	require.Equal(t, dto.CustomerRef{ID: f.Dana, Name: "Dana Devices"}, full.Customer)
	require.Equal(t, "Bi-Monthly Service", full.ServiceType.Name)
	require.Equal(t, "2026-11-10T09:00:00.123456Z", full.Date)
	require.Equal(t, dto.ServiceLookupAddressResponse{Line1: "Terminal 4 Arrivals", Line2: "Gate 7", City: "Queens", State: "NY", Zip: "11430", County: "Queens"}, full.Address)
	require.Equal(t, f.Gate, full.Device.ID)
	require.Equal(t, "Gate Box", full.Device.Name)
	require.Equal(t, "BC-9001", full.Device.Barcode)
	require.Equal(t, &dto.CoordinateResponse{Latitude: 37.5, Longitude: -122.1}, full.Device.Coordinate)
	require.Equal(t, "Area 9", full.Device.Area)
	require.Equal(t, "Bait Box", full.Device.DeviceType)
	require.Equal(t, ptr("2026-11-10T09:10:00.000000Z"), full.CheckIn)
	require.Equal(t, ptr("2026-11-10T09:25:00.500000Z"), full.CheckOut)
	require.Equal(t, "Good", full.Status)
	require.Equal(t, "Inspected", full.Activity)
	require.Equal(t, "Bait taken, refilled", full.Notes)
	require.Equal(t, "Thọ Nguyễn Quang", full.AssignedTo.Name)
}

func TestTheJSONKeepsNullsAndNeverOmitsAField(t *testing.T) {
	srv, f := newServer(t)

	resp := get(t, srv, listPath+"?client_id="+itoa(f.Dana)+"&limit=100")

	var raw struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp.body, &raw))
	var bare map[string]any
	for _, row := range raw.Data {
		if int64(row["id"].(float64)) == f.Tie1NoName {
			bare = row
		}
	}
	require.NotNil(t, bare)
	for _, key := range []string{"id", "customer", "service_type", "date", "address", "device", "check_in", "status", "activity", "check_out", "notes", "assigned_to"} {
		require.Contains(t, bare, key)
	}
	require.Nil(t, bare["check_in"])
	require.Nil(t, bare["check_out"])
	require.Nil(t, bare["assigned_to"])
	device := bare["device"].(map[string]any)
	require.Nil(t, device["coordinate"], "a device without a coordinate")
	require.Equal(t, "", device["name"])
	require.Equal(t, "40", bare["status"])
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

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

func TestFiltersOverHTTP(t *testing.T) {
	srv, f := newServer(t)
	year := "?from=2026-01-01&to=2026-12-31"

	require.Equal(t, []int64{f.OtherCheck}, ids(list(t, srv, year+"&client_id="+itoa(f.Other))))
	require.Len(t, list(t, srv, year+"&client_id="+itoa(f.Dana)).Data, 6)
	require.Equal(t, []int64{f.Tie2Beside}, ids(list(t, srv, year+"&q=bc-9003")))
	require.Equal(t, []int64{f.Tie2Beside, f.OtherCheck}, ids(list(t, srv, year+"&client_id="+itoa(f.Dana)+","+itoa(f.Other)+"&q=thon+nguyen")),
		"search reaches the assignee's name")
	require.Empty(t, list(t, srv, year+"&client_id=999999").Data)
	require.Equal(t, []int64{f.Tie1Gate}, ids(list(t, srv, year+"&client_id="+itoa(f.Dana)+"&q=bait+taken")),
		"the seeded check of 63706 has the same note, so the search is scoped to one client")
	require.Len(t, list(t, srv, year+"&q=bait+taken").Data, 2, "without the client filter both are found")
}

func TestPagingOverHTTPWalksEveryRowOnce(t *testing.T) {
	srv, _ := newServer(t)
	keys := []string{
		"customer", "service", "date", "address_1", "address_2", "city", "state", "zip", "device_name", "barcode", "coordinate",
		"area", "device_type", "check_in", "status", "activity", "check_out", "notes", "assigned_to",
	}
	for _, order := range []string{"asc", "desc"} {
		for _, sort := range keys {
			base := "?from=2026-01-01&to=2026-12-31&sort=" + sort + "&order=" + order
			full := ids(list(t, srv, base+"&limit=100"))
			require.Len(t, full, 15, "seven fixture checks and eight seeded ones")

			var walked []int64
			cursor := ""
			for pages := 0; pages < 20; pages++ {
				q := base + "&limit=4"
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
		{"bad date", "?from=yesterday", http.StatusUnprocessableEntity, "invalid_input", "from"},
		{"to before from", "?from=2026-11-05&to=2026-11-01", http.StatusUnprocessableEntity, "invalid_input", "to"},
		{"client id not a number", "?client_id=abc", http.StatusUnprocessableEntity, "invalid_input", "client_id"},
		{"unknown sort", "?sort=address", http.StatusUnprocessableEntity, "invalid_input", "sort"},
		{"no name format on this screen", "?name_format=full_name", http.StatusUnprocessableEntity, "invalid_input", "name_format"},
		{"a cursor of another sort", "?sort=city&cursor=eyJrIjoibm90ZXMiLCJkIjpmYWxzZSwidiI6IngiLCJpZCI6MX0", http.StatusUnprocessableEntity, "invalid_input", "cursor"},
		{"a check in cursor must be a number", "?sort=check_in&cursor=eyJrIjoiY2hlY2tfaW4iLCJkIjpmYWxzZSwidiI6ImFiYyIsImlkIjoxfQ", http.StatusUnprocessableEntity, "invalid_input", "cursor"},
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
	var out dto.DeviceTrackingFiltersResponse
	require.NoError(t, json.Unmarshal(resp.body, &out))
	require.Len(t, out.Clients, 22)
	require.Len(t, out.SortKeys, 19)
	require.Equal(t, dto.SortResponse{Key: "date", Order: "desc"}, out.DefaultSort)
}

func TestThereIsNoSummaryEndpoint(t *testing.T) {
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
