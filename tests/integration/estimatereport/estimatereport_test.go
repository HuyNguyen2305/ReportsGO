//go:build integration

package estimatereport_test

import (
	"context"
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
	listPath      = "/api/v1/reports/estimates"
)

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

// newServer starts the real handler stack (middleware, router, handler, service, repository) behind a real HTTP
// listener, on a seeded database. With withNulls it adds October estimates that have no email, creator or phone.
func newServer(t *testing.T, withNulls bool) *httptest.Server {
	t.Helper()

	pool := testdb.SeededPool(t)
	if withNulls {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO billing_documents (parent_type, document_number, customer_id, document_date, total)
			SELECT 'estimate', 'EN' || g, c.id, '2026-10-10', 10 * g
			FROM generate_series(1, 3) g
			JOIN customers c ON c.account_number = '63716'`)
		require.NoError(t, err)
	}

	logger := slog.New(slog.DiscardHandler)
	svc := service.NewEstimateService(repository.NewEstimateRepository(pool), func() time.Time { return october21 })
	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, []string{allowedOrigin}),
		Reports:    []handler.RouteRegistrar{handler.NewEstimateHandler(svc, logger)},
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

func (r response) list(t *testing.T) dto.EstimateListResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, r.status, "GET %s -> %s", r.request, r.body)
	var out dto.EstimateListResponse
	require.NoError(t, json.Unmarshal(r.body, &out))
	return out
}

func (r response) errorBody(t *testing.T) dto.ErrorBody {
	t.Helper()
	var out dto.ErrorResponse
	require.NoError(t, json.Unmarshal(r.body, &out), "body: %s", r.body)
	return out.Error
}

func numbers(l dto.EstimateListResponse) []string {
	out := make([]string, len(l.Data))
	for i, d := range l.Data {
		out[i] = d.EstimateNumber
	}
	return out
}

func idsOf(l dto.EstimateListResponse) []int64 {
	out := make([]int64, len(l.Data))
	for i, d := range l.Data {
		out[i] = d.ID
	}
	return out
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// the 19 active October estimates in the seed: 17 drafts, one accepted, one declined.
var seedNumbers = []string{
	"123187", "123184", "123186", "123185", "123183", "123175", "123174", "123173", "123172", "123168",
	"123170", "123169", "123156", "123159", "123158", "123157", "123155", "123188", "123189",
}

func TestDefaultRequest(t *testing.T) {
	srv := newServer(t, false)

	resp := get(t, srv, listPath)
	page := resp.list(t)

	require.Len(t, page.Data, 19, "the active October estimates")
	require.Equal(t, 50, page.Page.Limit)
	require.False(t, page.Page.HasMore)
	require.Nil(t, page.Page.NextCursor)
	require.NotNil(t, page.Summary)
	require.EqualValues(t, 19, page.Summary.Count)
	require.Equal(t, "698.12", page.Summary.Total)
	require.NotNil(t, page.Range.From)
	require.Equal(t, "2026-10-01", *page.Range.From)
	require.Equal(t, "2026-10-31", *page.Range.To)
	require.Equal(t, sorted(seedNumbers), sorted(numbers(page)))
	require.Equal(t, "no-store", resp.header.Get("Cache-Control"))
	require.NotEmpty(t, resp.header.Get("X-Request-ID"))

	first := page.Data[0]
	require.Equal(t, "2026-10-21", first.Date, "newest first")
}

func TestEstimateFields(t *testing.T) {
	srv := newServer(t, false)

	page := get(t, srv, listPath+"?q=123183").list(t)
	require.Len(t, page.Data, 1)
	e := page.Data[0]
	require.Equal(t, "123183", e.EstimateNumber)
	require.Equal(t, "63714", e.AccountNumber)
	require.Equal(t, "#F Last Name", e.Customer.Name)
	require.NotZero(t, e.Customer.ID)
	require.Equal(t, "012222222", *e.Phone)
	require.Equal(t, "d@cm.co", *e.BillingEmail)
	require.Equal(t, "Thọ Nguyễn Quang", *e.CreatedBy)
	require.Equal(t, "2026-10-19", e.Date)
	require.Equal(t, "24.40", e.Total)
	require.Equal(t, "draft", e.Status)
	require.Equal(t, "draft", e.SignatureStatus)

	accepted := get(t, srv, listPath+"?status=accepted").list(t)
	require.Len(t, accepted.Data, 1)
	require.Equal(t, "150.00", accepted.Data[0].Total)
	require.Equal(t, "signed", accepted.Data[0].SignatureStatus)
	require.Nil(t, accepted.Data[0].Phone, "that customer has no phone")
}

func TestNullableFieldsAreJSONNull(t *testing.T) {
	srv := newServer(t, true)

	var list map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"?q=EN2").body, &list))
	e := list["data"].([]any)[0].(map[string]any)

	for _, k := range []string{"phone", "billing_email", "created_by"} {
		require.Contains(t, e, k)
		require.Nil(t, e[k], k)
	}
}

func TestPagingOverHTTPFollowsNextCursorForEverySortAndOrder(t *testing.T) {
	srv := newServer(t, true)

	for _, key := range model.EstimateSortKeys {
		for _, order := range []string{"asc", "desc"} {
			t.Run(fmt.Sprintf("%s/%s", key, order), func(t *testing.T) {
				base := fmt.Sprintf("%s?sort=%s&order=%s", listPath, key, order)

				all := get(t, srv, base+"&limit=200").list(t)
				require.Len(t, all.Data, 22)

				var got []int64
				target := base + "&limit=5"
				for pages := 1; ; pages++ {
					require.LessOrEqual(t, pages, 10, "paging must terminate")

					page := get(t, srv, target).list(t)
					got = append(got, idsOf(page)...)

					if pages == 1 {
						require.NotNil(t, page.Summary)
						require.EqualValues(t, 22, page.Summary.Count)
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

				require.Equal(t, idsOf(all), got, "every estimate exactly once, in order")
			})
		}
	}
}

func TestMultiValueParametersAreEquivalentInEveryForm(t *testing.T) {
	srv := newServer(t, false)

	forms := []string{
		"?status=accepted,declined&sort=estimate_number",
		"?status=accepted&status=declined&sort=estimate_number",
		"?status=declined&status=accepted&sort=estimate_number",
		"?status=%20accepted%20,%20declined%20&sort=estimate_number",
		"?status=ACCEPTED,Declined&sort=estimate_number",
	}

	var reference []string
	for i, form := range forms {
		page := get(t, srv, listPath+form).list(t)
		require.Len(t, page.Data, 2, form)
		require.EqualValues(t, 2, page.Summary.Count, form)
		if i == 0 {
			reference = numbers(page)
			continue
		}
		require.Equal(t, reference, numbers(page), form)
	}
}

func TestFilters(t *testing.T) {
	srv := newServer(t, false)

	tests := []struct {
		name      string
		query     string
		wantCount int
		wantTotal string
	}{
		{"accepted", "?status=accepted", 1, "150.00"},
		{"declined signature", "?signature_status=declined", 1, "75.50"},
		{"signed", "?signature_status=signed", 1, "150.00"},
		{"drafts", "?status=draft", 17, "472.62"},
		{"archived tab", "?lifecycle=archived", 1, "40.00"},
		{"deleted tab", "?lifecycle=deleted", 1, "10.00"},
		{"one day", "?from=2026-10-21&to=2026-10-21", 2, "225.50"},
		{"open start", "?to=2026-10-08", 5, "179.82"},
		{"september is empty", "?from=2026-09-01&to=2026-09-30", 0, "0.00"},
		{"search by customer name", "?q=alaba", 3, "99.22"},
		{"search by account number", "?q=5002", 3, "99.22"},
		{"search by email, encoded", "?q=alaba123%40yopmail", 3, "99.22"},
		{"search with plus as space", "?q=Customer+1", 6, "272.00"},
		{"percent is literal", "?q=%25", 0, "0.00"},
		{"underscore is literal", "?q=1_3", 0, "0.00"},
		{"accented text matches nothing", "?q=%C3%A9", 0, "0.00"},
		{"injection text matches nothing", "?q=%27%3B+DROP+TABLE+billing_documents%3B--", 0, "0.00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := get(t, srv, listPath+tt.query).list(t)

			require.Len(t, page.Data, tt.wantCount)
			require.NotNil(t, page.Summary)
			require.EqualValues(t, tt.wantCount, page.Summary.Count)
			require.Equal(t, tt.wantTotal, page.Summary.Total)
		})
	}
}

func TestPageSizeAndHasMore(t *testing.T) {
	srv := newServer(t, false)

	page := get(t, srv, listPath+"?limit=19").list(t)
	require.Len(t, page.Data, 19)
	require.False(t, page.Page.HasMore, "an exactly full last page has no successor")
	require.Nil(t, page.Page.NextCursor)

	page = get(t, srv, listPath+"?limit=18").list(t)
	require.Len(t, page.Data, 18)
	require.True(t, page.Page.HasMore)
	require.NotNil(t, page.Page.NextCursor)
	require.EqualValues(t, 19, page.Summary.Count, "the summary covers all pages")
}

func TestErrorContract(t *testing.T) {
	srv := newServer(t, false)

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
		{"invoice-only sort discount", "?sort=discount", 422, "invalid_input", []string{"sort"}},
		{"invoice-only sort job", "?sort=job", 422, "invalid_input", []string{"sort"}},
		{"unknown order", "?order=sideways", 422, "invalid_input", []string{"order"}},
		{"unknown lifecycle", "?lifecycle=everything", 422, "invalid_input", []string{"lifecycle"}},
		{"unknown status", "?status=overdue", 422, "invalid_input", []string{"status"}},
		{"invoice-only status paid", "?status=paid", 422, "invalid_input", []string{"status"}},
		{"invoice-only status void", "?status=void", 422, "invalid_input", []string{"status"}},
		{"unknown signature status", "?signature_status=maybe", 422, "invalid_input", []string{"signature_status"}},
		{"NUL in search", "?q=a%00b", 422, "invalid_input", []string{"q"}},
		{"invalid UTF-8 in search", "?q=%FF%FE", 422, "invalid_input", []string{"q"}},
		{"search too long", "?q=" + strings.Repeat("a", 101), 422, "invalid_input", []string{"q"}},
		{"garbage cursor", "?cursor=garbage", 422, "invalid_input", []string{"cursor"}},
		{"unknown parameter", "?statuss=draft", 422, "invalid_input", []string{"statuss"}},
		{"invoice-only parameter service_id", "?service_id=1", 422, "invalid_input", []string{"service_id"}},
		{"invoice-only parameter tag_id", "?tag_id=1", 422, "invalid_input", []string{"tag_id"}},
		{"invoice-only parameter payment_term_id", "?payment_term_id=1", 422, "invalid_input", []string{"payment_term_id"}},
		{"duplicated parameter", "?limit=1&limit=2", 422, "invalid_input", []string{"limit"}},
		{"several field problems are reported together", "?limit=0&sort=bogus&status=x", 422, "invalid_input", []string{"limit", "sort", "status"}},
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

func TestInvoiceOnlyValuesAreNamedWithTheEstimateChoices(t *testing.T) {
	srv := newServer(t, false)

	body := get(t, srv, listPath+"?status=paid&sort=discount").errorBody(t)

	messages := map[string]string{}
	for _, d := range body.Details {
		messages[d.Field] = d.Message
	}
	require.Equal(t, "must be one of: draft, sent, accepted, declined", messages["status"])
	require.Contains(t, messages["sort"], "estimate_number")
	require.NotContains(t, messages["sort"], "discount")
}

func TestCursorFromAnotherSortIsRejected(t *testing.T) {
	srv := newServer(t, false)

	page := get(t, srv, listPath+"?limit=2").list(t)
	require.NotNil(t, page.Page.NextCursor)
	cursor := url.QueryEscape(*page.Page.NextCursor)

	for _, other := range []string{"&sort=total&order=desc", "&order=asc"} {
		resp := get(t, srv, listPath+"?limit=2"+other+"&cursor="+cursor)

		require.Equal(t, http.StatusUnprocessableEntity, resp.status, other)
		body := resp.errorBody(t)
		require.Equal(t, "cursor", body.Details[0].Field)
		require.Contains(t, body.Details[0].Message, "does not match the requested sort")
	}
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

func TestJSONContract(t *testing.T) {
	srv := newServer(t, false)

	var list map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"?limit=1").body, &list))

	require.Equal(t, []string{"data", "page", "range", "summary"}, keysOf(list))
	require.Equal(t, []string{"has_more", "limit", "next_cursor"}, keysOf(list["page"].(map[string]any)))
	require.Equal(t, []string{"from", "to"}, keysOf(list["range"].(map[string]any)))
	require.Equal(t, []string{"count", "total"}, keysOf(list["summary"].(map[string]any)))

	estimate := list["data"].([]any)[0].(map[string]any)
	require.Equal(t, []string{
		"account_number", "billing_email", "created_by", "customer", "date", "estimate_number", "id", "phone",
		"signature_status", "status", "total",
	}, keysOf(estimate))
	require.Equal(t, []string{"id", "name"}, keysOf(estimate["customer"].(map[string]any)))

	// Money is always a string, ids and counts are numbers.
	require.IsType(t, "", estimate["total"])
	require.IsType(t, "", list["summary"].(map[string]any)["total"])
	require.IsType(t, float64(0), estimate["id"])
	require.IsType(t, float64(0), list["summary"].(map[string]any)["count"])

	var filters map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"/filters").body, &filters))
	require.Equal(t, []string{
		"default_sort", "lifecycles", "limits", "signature_statuses", "sort_keys", "statuses",
	}, keysOf(filters))

	var errBody map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"?limit=0").body, &errBody))
	require.Equal(t, []string{"error"}, keysOf(errBody))
	require.Equal(t, []string{"code", "details", "message", "request_id"}, keysOf(errBody["error"].(map[string]any)))
}

func TestFiltersEndpointMatchesWhatTheListAccepts(t *testing.T) {
	srv := newServer(t, false)

	var f dto.EstimateFiltersResponse
	resp := get(t, srv, listPath+"/filters")
	require.Equal(t, http.StatusOK, resp.status)
	require.Equal(t, "no-store", resp.header.Get("Cache-Control"))
	require.NoError(t, json.Unmarshal(resp.body, &f))

	require.Equal(t, []string{"draft", "sent", "accepted", "declined"}, f.Statuses)
	require.Equal(t, []string{"draft", "pending", "signed", "declined"}, f.SignatureStatuses)
	require.Equal(t, []string{"active", "archived", "deleted"}, f.Lifecycles)
	require.Len(t, f.SortKeys, 10)
	require.Equal(t, dto.SortResponse{Key: "date", Order: "desc"}, f.DefaultSort)
	require.Equal(t, dto.PageLimitsResponse{Default: 50, Max: 200}, f.Limits)

	// Every advertised sort key, status and lifecycle is accepted by the list endpoint.
	for _, key := range f.SortKeys {
		require.Equal(t, http.StatusOK, get(t, srv, listPath+"?sort="+key).status, key)
	}
	for _, s := range f.Statuses {
		require.Equal(t, http.StatusOK, get(t, srv, listPath+"?status="+s).status, s)
	}
	for _, s := range f.SignatureStatuses {
		require.Equal(t, http.StatusOK, get(t, srv, listPath+"?signature_status="+s).status, s)
	}
	for _, l := range f.Lifecycles {
		require.Equal(t, http.StatusOK, get(t, srv, listPath+"?lifecycle="+l).status, l)
	}
}

func TestCORSAndRouting(t *testing.T) {
	srv := newServer(t, false)

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
		require.Equal(t, "GET, HEAD, OPTIONS", resp.header.Get("Access-Control-Allow-Methods"))
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
	srv := newServer(t, false)

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
			key := string(model.EstimateSortKeys[i%len(model.EstimateSortKeys)])
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
		var page dto.EstimateListResponse
		require.NoError(t, json.Unmarshal(r.body, &page))
		require.Len(t, page.Data, 19, "worker %d (sort %s)", i, r.sortKey)
		require.Equal(t, "698.12", page.Summary.Total)
		require.Equal(t, sorted(seedNumbers), sorted(numbers(page)))
	}
}

func TestAnEmptyResultIsAnEmptyArrayOnTheWire(t *testing.T) {
	srv := newServer(t, false)

	resp := get(t, srv, listPath+"?from=2026-09-01&to=2026-09-30")

	require.Equal(t, http.StatusOK, resp.status)
	require.Contains(t, string(resp.body), `"data":[]`, "never null, so clients can iterate without a check")
	require.Contains(t, string(resp.body), `"summary":{"count":0,"total":"0.00"}`)
}
