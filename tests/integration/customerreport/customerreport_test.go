//go:build integration

package customerreport_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/dto"
	"reportsgo/internal/handler"
	"reportsgo/internal/middleware"
	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/internal/service"
	"reportsgo/tests/integration/testdb"
)

func init() { gin.SetMode(gin.TestMode) }

const (
	allowedOrigin = "https://app.example.com"
	listPath      = "/api/v1/reports/new-customers"
)

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

// newServer starts the real handler stack (middleware, router, handler, service, repository) behind a real HTTP
// listener, on a seeded database.
func newServer(t *testing.T) *httptest.Server {
	t.Helper()

	pool := testdb.SeededPool(t)
	logger := slog.New(slog.DiscardHandler)
	svc := service.NewCustomerService(repository.NewCustomerRepository(pool), func() time.Time { return october21 })
	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, []string{allowedOrigin}),
		Reports:    []handler.RouteRegistrar{handler.NewCustomerHandler(svc, logger)},
	})

	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	return srv
}

type response struct {
	status  int
	header  http.Header
	body    []byte
	request string // the request path and query, for failure messages
}

func send(t *testing.T, srv *httptest.Server, method, target string, headers map[string]string) response {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+target, nil)
	require.NoError(t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return response{status: resp.StatusCode, header: resp.Header, body: body, request: target}
}

func get(t *testing.T, srv *httptest.Server, target string) response {
	t.Helper()
	return send(t, srv, http.MethodGet, target, nil)
}

func (r response) list(t *testing.T) dto.CustomerListResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, r.status, "GET %s -> %s", r.request, r.body)
	var out dto.CustomerListResponse
	require.NoError(t, json.Unmarshal(r.body, &out))
	return out
}

func (r response) errorBody(t *testing.T) dto.ErrorBody {
	t.Helper()
	var out dto.ErrorResponse
	require.NoError(t, json.Unmarshal(r.body, &out), "body: %s", r.body)
	return out.Error
}

func idsOf(l dto.CustomerListResponse) []int64 {
	out := make([]int64, len(l.Data))
	for i, d := range l.Data {
		out[i] = d.ID
	}
	return out
}

// keysOf returns the sorted keys of a JSON object.
func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestDefaultRequest(t *testing.T) {
	srv := newServer(t)

	resp := get(t, srv, listPath+"?limit=200")
	page := resp.list(t)

	require.Len(t, page.Data, 14, "the October customers")
	require.False(t, page.Page.HasMore)
	require.Nil(t, page.Page.NextCursor)
	require.NotNil(t, page.Summary)
	require.EqualValues(t, 14, page.Summary.Count)
	require.Equal(t, "2026-10-01", *page.Range.From)
	require.Equal(t, "2026-10-31", *page.Range.To)
	require.Equal(t, "no-store", resp.header.Get("Cache-Control"))
	require.NotEmpty(t, resp.header.Get("X-Request-ID"))

	require.Equal(t, 50, get(t, srv, listPath).list(t).Page.Limit)
	for i := 1; i < len(page.Data); i++ {
		require.GreaterOrEqual(t, page.Data[i-1].CreatedAt, page.Data[i].CreatedAt, "newest first")
	}
}

func TestCustomerFields(t *testing.T) {
	srv := newServer(t)

	page := get(t, srv, listPath+"?limit=1").list(t)
	require.Len(t, page.Data, 1)
	c := page.Data[0]
	require.NotZero(t, c.ID)
	require.Equal(t, c.ID, c.Customer.ID)
	require.Equal(t, "Ada Lovelace", c.Customer.Name)
	require.Equal(t, "70001", c.AccountNumber)
	require.NotNil(t, c.Email)
	require.Equal(t, "ada@example.com", *c.Email)
	require.NotNil(t, c.Phone)
	require.Equal(t, "555-0101", *c.Phone, "the first of two phones")
	require.NotNil(t, c.Source)
	require.Equal(t, "Google", c.Source.Name)
	require.NotZero(t, c.Source.ID)
	require.Equal(t, "2026-10-22T08:15:30.123456Z", c.CreatedAt)
}

func TestMissingEmailPhoneAndSourceAreJSONNull(t *testing.T) {
	srv := newServer(t)

	var list map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"?q=63701").body, &list))
	c := list["data"].([]any)[0].(map[string]any)

	for _, field := range []string{"email", "phone", "source"} {
		require.Contains(t, c, field)
		require.Nil(t, c[field], field)
	}
}

func TestPagingOverHTTPFollowsNextCursorForEverySortAndOrder(t *testing.T) {
	srv := newServer(t)

	for _, key := range model.CustomerSortKeys {
		for _, order := range []string{"asc", "desc"} {
			t.Run(fmt.Sprintf("%s/%s", key, order), func(t *testing.T) {
				base := fmt.Sprintf("%s?sort=%s&order=%s", listPath, key, order)

				all := get(t, srv, base+"&limit=200").list(t)
				require.Len(t, all.Data, 14)

				var got []int64
				target := base + "&limit=5"
				for pages := 1; ; pages++ {
					require.LessOrEqual(t, pages, 10, "paging must terminate")

					page := get(t, srv, target).list(t)
					got = append(got, idsOf(page)...)

					if pages == 1 {
						require.NotNil(t, page.Summary)
						require.EqualValues(t, 14, page.Summary.Count)
					} else {
						require.Nil(t, page.Summary, "only the first page carries the summary")
					}

					if page.Page.NextCursor == nil {
						require.False(t, page.Page.HasMore)
						break
					}
					require.True(t, page.Page.HasMore)
					target = base + "&limit=5&cursor=" + url.QueryEscape(*page.Page.NextCursor)
				}

				require.Equal(t, idsOf(all), got, "every customer exactly once, in order")
			})
		}
	}
}

func TestMultiValueParametersAreEquivalentInEveryForm(t *testing.T) {
	srv := newServer(t)

	var f dto.CustomerFiltersResponse
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"/filters").body, &f))
	ids := map[string]int64{}
	for _, s := range f.LeadSources {
		ids[s.Name] = s.ID
	}
	fb, g := ids["Facebook"], ids["Google"]

	forms := []string{
		fmt.Sprintf("?lead_source_id=%d,%d&sort=created_at", fb, g),
		fmt.Sprintf("?lead_source_id=%d&lead_source_id=%d&sort=created_at", fb, g),
		fmt.Sprintf("?lead_source_id=%d&lead_source_id=%d&sort=created_at", g, fb),
		fmt.Sprintf("?lead_source_id=%%20%d%%20,%%20%d%%20&sort=created_at", fb, g),
	}

	var reference []int64
	for i, form := range forms {
		page := get(t, srv, listPath+form).list(t)
		require.Len(t, page.Data, 4, form)
		require.EqualValues(t, 4, page.Summary.Count, form)
		if i == 0 {
			reference = idsOf(page)
			continue
		}
		require.Equal(t, reference, idsOf(page), form)
	}
}

func TestFilters(t *testing.T) {
	srv := newServer(t)

	var f dto.CustomerFiltersResponse
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"/filters").body, &f))
	sourceID := map[string]string{}
	for _, s := range f.LeadSources {
		sourceID[s.Name] = fmt.Sprint(s.ID)
	}

	tests := []struct {
		name      string
		query     string
		wantCount int
	}{
		{"one source", "?lead_source_id=" + sourceID["Facebook"], 3},
		{"google in october", "?lead_source_id=" + sourceID["Google"], 1},
		{"two sources, comma separated", "?lead_source_id=" + sourceID["Referral"] + "," + sourceID["Google"], 2},
		{"one day", "?from=2026-10-22&to=2026-10-22", 1},
		{"the first and last instant of a day", "?from=2026-10-10&to=2026-10-10", 2},
		{"september", "?from=2026-09-01&to=2026-09-30", 4},
		{"november 1", "?from=2026-11-01&to=2026-11-01", 1},
		{"everything", "?from=2000-01-01&to=2100-01-01", 19},
		{"open start", "?to=2026-10-05", 9},
		{"search by name with plus as space", "?q=Ada+Lovelace", 1},
		{"search by account number", "?q=7000", 6},
		{"search by email", "?q=example.com", 4},
		{"search by phone", "?q=0606", 1},
		{"percent is literal", "?q=50%25", 1},
		{"a lone percent is not a wildcard", "?q=a%25b", 0},
		{"underscore is literal", "?q=a_b", 1},
		{"accented text matches nothing", "?q=%C3%A9", 0},
		{"injection text matches nothing", "?q=%27%3B+DROP+TABLE+customers%3B--", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := get(t, srv, listPath+tt.query+"&limit=200").list(t)

			require.Len(t, page.Data, tt.wantCount)
			require.NotNil(t, page.Summary)
			require.EqualValues(t, tt.wantCount, page.Summary.Count)
		})
	}
}

func TestPageSizeAndHasMore(t *testing.T) {
	srv := newServer(t)

	page := get(t, srv, listPath+"?limit=14").list(t)
	require.Len(t, page.Data, 14)
	require.False(t, page.Page.HasMore, "an exactly full last page has no successor")
	require.Nil(t, page.Page.NextCursor)

	page = get(t, srv, listPath+"?limit=13").list(t)
	require.Len(t, page.Data, 13)
	require.True(t, page.Page.HasMore)
	require.NotNil(t, page.Page.NextCursor)
	require.EqualValues(t, 14, page.Summary.Count, "the count covers all pages")
}

func TestErrorContract(t *testing.T) {
	srv := newServer(t)

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantCode   string
		wantFields []string
	}{
		{"limit zero", "?limit=0", 422, "invalid_input", []string{"limit"}},
		{"limit too large", "?limit=201", 422, "invalid_input", []string{"limit"}},
		{"impossible date", "?from=2026-02-30", 422, "invalid_input", []string{"from"}},
		{"to before from", "?from=2026-10-10&to=2026-10-09", 422, "invalid_input", []string{"to"}},
		{"unknown sort", "?sort=bogus", 422, "invalid_input", []string{"sort"}},
		{"another report's sort", "?sort=invoice_value", 422, "invalid_input", []string{"sort"}},
		{"unknown order", "?order=sideways", 422, "invalid_input", []string{"order"}},
		{"lead source id not a number", "?lead_source_id=abc", 422, "invalid_input", []string{"lead_source_id"}},
		{"lead source id zero", "?lead_source_id=0", 422, "invalid_input", []string{"lead_source_id"}},
		{"NUL in search", "?q=a%00b", 422, "invalid_input", []string{"q"}},
		{"invalid UTF-8 in search", "?q=%FF%FE", 422, "invalid_input", []string{"q"}},
		{"search too long", "?q=" + strings.Repeat("a", 101), 422, "invalid_input", []string{"q"}},
		{"garbage cursor", "?cursor=garbage", 422, "invalid_input", []string{"cursor"}},
		{"unknown parameter", "?lead_source=1", 422, "invalid_input", []string{"lead_source"}},
		{"customers have no status", "?status=active", 422, "invalid_input", []string{"status"}},
		{"customers have no tabs", "?lifecycle=active", 422, "invalid_input", []string{"lifecycle"}},
		{"job parameter service_type_id", "?service_type_id=1", 422, "invalid_input", []string{"service_type_id"}},
		{"duplicated parameter", "?limit=1&limit=2", 422, "invalid_input", []string{"limit"}},
		{"several field problems are reported together", "?limit=0&sort=bogus&lead_source_id=x", 422, "invalid_input", []string{"limit", "sort", "lead_source_id"}},
		// Unsupported parameters are rejected before any value is validated, so they are reported alone.
		{"an unsupported parameter is reported first", "?limit=0&sort=bogus&nope=1", 422, "invalid_input", []string{"nope"}},
		{"bad percent-encoding", "?q=%zz", 400, "invalid_request", nil},
		{"semicolon separator", "?q=a;b", 400, "invalid_request", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := send(t, srv, http.MethodGet, listPath+tt.query, map[string]string{"X-Request-ID": "trace-123"})

			require.Equal(t, tt.wantStatus, resp.status, "body: %s", resp.body)
			body := resp.errorBody(t)
			require.Equal(t, tt.wantCode, body.Code)
			require.Equal(t, "trace-123", body.RequestID)
			require.Equal(t, "trace-123", resp.header.Get("X-Request-ID"))
			require.Equal(t, "no-store", resp.header.Get("Cache-Control"))
			require.Contains(t, resp.header.Get("Content-Type"), "application/json")

			var fields []string
			for _, d := range body.Details {
				fields = append(fields, d.Field)
			}
			require.ElementsMatch(t, tt.wantFields, fields)
		})
	}
}

func TestErrorMessagesNameTheCustomerChoices(t *testing.T) {
	srv := newServer(t)

	body := get(t, srv, listPath+"?sort=invoice_value").errorBody(t)

	require.Len(t, body.Details, 1)
	require.Equal(t, "sort", body.Details[0].Field)
	for _, key := range model.CustomerSortKeys {
		require.Contains(t, body.Details[0].Message, string(key))
	}
	require.NotContains(t, body.Details[0].Message, "invoice_value")
}

func TestCursorFromAnotherSortIsRejected(t *testing.T) {
	srv := newServer(t)

	page := get(t, srv, listPath+"?limit=2").list(t)
	require.NotNil(t, page.Page.NextCursor)
	cursor := url.QueryEscape(*page.Page.NextCursor)

	for _, other := range []string{"&sort=email&order=desc", "&order=asc"} {
		resp := get(t, srv, listPath+"?limit=2"+other+"&cursor="+cursor)

		require.Equal(t, http.StatusUnprocessableEntity, resp.status, other)
		body := resp.errorBody(t)
		require.Equal(t, "cursor", body.Details[0].Field)
		require.Contains(t, body.Details[0].Message, "does not match the requested sort")
	}
}

func TestJSONContract(t *testing.T) {
	srv := newServer(t)

	var list map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"?limit=1").body, &list))

	require.Equal(t, []string{"data", "page", "range", "summary"}, keysOf(list))
	require.Equal(t, []string{"has_more", "limit", "next_cursor"}, keysOf(list["page"].(map[string]any)))
	require.Equal(t, []string{"from", "to"}, keysOf(list["range"].(map[string]any)))
	require.Equal(t, []string{"count"}, keysOf(list["summary"].(map[string]any)))

	customer := list["data"].([]any)[0].(map[string]any)
	require.Equal(t, []string{
		"account_number", "created_at", "customer", "email", "id", "phone", "source",
	}, keysOf(customer))
	require.Equal(t, []string{"id", "name"}, keysOf(customer["customer"].(map[string]any)))
	require.Equal(t, []string{"id", "name"}, keysOf(customer["source"].(map[string]any)))

	// ids and counts are numbers, timestamps are strings.
	require.IsType(t, "", customer["created_at"])
	require.IsType(t, float64(0), customer["id"])
	require.IsType(t, float64(0), list["summary"].(map[string]any)["count"])

	var filters map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"/filters").body, &filters))
	require.Equal(t, []string{"default_sort", "lead_sources", "limits", "sort_keys"}, keysOf(filters))

	var errBody map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"?limit=0").body, &errBody))
	require.Equal(t, []string{"error"}, keysOf(errBody))
	require.Equal(t, []string{"code", "details", "message", "request_id"}, keysOf(errBody["error"].(map[string]any)))
}

func TestAnEmptyResultIsAnEmptyArrayOnTheWire(t *testing.T) {
	srv := newServer(t)

	resp := get(t, srv, listPath+"?from=2025-01-01&to=2025-01-31")

	require.Equal(t, http.StatusOK, resp.status)
	require.Contains(t, string(resp.body), `"data":[]`, "never null, so clients can iterate without a check")
	require.Contains(t, string(resp.body), `"summary":{"count":0}`)
}

func TestFiltersEndpointMatchesWhatTheListAccepts(t *testing.T) {
	srv := newServer(t)

	var f dto.CustomerFiltersResponse
	resp := get(t, srv, listPath+"/filters")
	require.Equal(t, http.StatusOK, resp.status)
	require.Equal(t, "no-store", resp.header.Get("Cache-Control"))
	require.NoError(t, json.Unmarshal(resp.body, &f))

	require.Len(t, f.LeadSources, 3)
	for _, o := range f.LeadSources {
		require.NotZero(t, o.ID)
		require.NotEmpty(t, o.Name)
	}
	require.Equal(t, []string{"customer", "account_number", "email", "phone", "source", "created_at"}, f.SortKeys)
	require.Equal(t, dto.SortResponse{Key: "created_at", Order: "desc"}, f.DefaultSort)
	require.Equal(t, dto.PageLimitsResponse{Default: 50, Max: 200}, f.Limits)

	// Everything the endpoint advertises is accepted by the list endpoint.
	for _, key := range f.SortKeys {
		require.Equal(t, http.StatusOK, get(t, srv, listPath+"?sort="+key).status, key)
	}
	for _, o := range f.LeadSources {
		require.Equal(t, http.StatusOK, get(t, srv, fmt.Sprintf("%s?lead_source_id=%d", listPath, o.ID)).status)
	}
}

func TestCORSAndRouting(t *testing.T) {
	srv := newServer(t)

	t.Run("allowed origin", func(t *testing.T) {
		resp := send(t, srv, http.MethodGet, listPath, map[string]string{"Origin": allowedOrigin})
		require.Equal(t, http.StatusOK, resp.status)
		require.Equal(t, allowedOrigin, resp.header.Get("Access-Control-Allow-Origin"))
		require.Equal(t, "X-Request-ID", resp.header.Get("Access-Control-Expose-Headers"))
	})

	t.Run("other origin gets no CORS headers", func(t *testing.T) {
		resp := send(t, srv, http.MethodGet, listPath, map[string]string{"Origin": "https://evil.example"})
		require.Equal(t, http.StatusOK, resp.status)
		require.Empty(t, resp.header.Get("Access-Control-Allow-Origin"))
	})

	t.Run("preflight", func(t *testing.T) {
		resp := send(t, srv, http.MethodOptions, listPath, map[string]string{
			"Origin": allowedOrigin, "Access-Control-Request-Method": "GET",
		})
		require.Equal(t, http.StatusNoContent, resp.status)
		require.Equal(t, "GET, OPTIONS", resp.header.Get("Access-Control-Allow-Methods"))
	})

	t.Run("report is read-only", func(t *testing.T) {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			resp := send(t, srv, method, listPath, nil)
			require.Equal(t, http.StatusMethodNotAllowed, resp.status, method)
			require.Equal(t, "method_not_allowed", resp.errorBody(t).Code)
		}
	})

	t.Run("unknown subpath", func(t *testing.T) {
		resp := get(t, srv, listPath+"/nope")
		require.Equal(t, http.StatusNotFound, resp.status)
		require.Equal(t, "not_found", resp.errorBody(t).Code)
	})
}

func TestConcurrentRequestsWithDifferentSortsAgree(t *testing.T) {
	srv := newServer(t)

	type result struct {
		status  int
		body    []byte
		sortKey string
	}
	const workers = 20
	results := make([]result, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := string(model.CustomerSortKeys[i%len(model.CustomerSortKeys)])
			req, _ := http.NewRequest(http.MethodGet, srv.URL+listPath+"?sort="+key+"&limit=200", nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				results[i] = result{status: -1, sortKey: key}
				return
			}
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)
			results[i] = result{status: resp.StatusCode, body: body, sortKey: key}
		}()
	}
	wg.Wait()

	for i, r := range results {
		require.Equal(t, http.StatusOK, r.status, "worker %d (sort %s)", i, r.sortKey)
		var page dto.CustomerListResponse
		require.NoError(t, json.Unmarshal(r.body, &page))
		require.Len(t, page.Data, 14, "worker %d (sort %s)", i, r.sortKey)
		require.EqualValues(t, 14, page.Summary.Count)
	}
}
