//go:build integration

package documentreport_test

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
	listPath      = "/api/v1/reports/documents"
)

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

// newServer starts the real handler stack (middleware, router, handler, service, repository) behind a real HTTP
// listener, on a seeded database. With withExtras it adds three October documents created at the same instant
// and without a creator.
func newServer(t *testing.T, withExtras bool) *httptest.Server {
	t.Helper()

	pool := testdb.SeededPool(t)
	if withExtras {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO documents (document_number, document_type_id, customer_id, created_by, created_at)
			SELECT 'X' || g, dt.id, c.id, NULL, '2026-10-12 08:00:00+00'
			FROM generate_series(1, 3) g
			JOIN document_types dt ON dt.name = '1-Time Treatment (PestPros of MI)'
			JOIN customers c ON c.account_number = '63716'`)
		require.NoError(t, err)
	}

	logger := slog.New(slog.DiscardHandler)
	svc := service.NewDocumentService(repository.NewDocumentRepository(pool), func() time.Time { return october21 })
	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, []string{allowedOrigin}),
		Reports:    []handler.RouteRegistrar{handler.NewDocumentHandler(svc, logger)},
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

func (r response) list(t *testing.T) dto.DocumentListResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, r.status, "GET %s -> %s", r.request, r.body)
	var out dto.DocumentListResponse
	require.NoError(t, json.Unmarshal(r.body, &out))
	return out
}

func (r response) errorBody(t *testing.T) dto.ErrorBody {
	t.Helper()
	var out dto.ErrorResponse
	require.NoError(t, json.Unmarshal(r.body, &out), "body: %s", r.body)
	return out.Error
}

func numbers(l dto.DocumentListResponse) []string {
	out := make([]string, len(l.Data))
	for i, d := range l.Data {
		out[i] = d.DocumentNumber
	}
	return out
}

func idsOf(l dto.DocumentListResponse) []int64 {
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

// the 18 active October documents in the seed: 15 drafts, one sent, two signed (5900 is deleted).
var seedNumbers = []string{
	"5979", "5978", "5977", "5972", "5957", "5936", "5931", "5924", "5923", "5910", "5859", "5805", "5751",
	"5746", "5743", "5980", "5981", "5982",
}

func TestDefaultRequest(t *testing.T) {
	srv := newServer(t, false)

	resp := get(t, srv, listPath)
	page := resp.list(t)

	require.Len(t, page.Data, 18, "the active October documents")
	require.Equal(t, 50, page.Page.Limit)
	require.False(t, page.Page.HasMore)
	require.Nil(t, page.Page.NextCursor)
	require.NotNil(t, page.Summary)
	require.EqualValues(t, 18, page.Summary.Count)
	require.NotNil(t, page.Range.From)
	require.Equal(t, "2026-10-01", *page.Range.From)
	require.Equal(t, "2026-10-31", *page.Range.To)
	require.Equal(t, sorted(seedNumbers), sorted(numbers(page)))
	require.Equal(t, "no-store", resp.header.Get("Cache-Control"))
	require.NotEmpty(t, resp.header.Get("X-Request-ID"))

	first := page.Data[0]
	require.Equal(t, "5982", first.DocumentNumber, "newest first")
	require.Equal(t, "2026-10-22T08:15:30.123456Z", first.CreatedAt)
}

func TestDocumentFields(t *testing.T) {
	srv := newServer(t, false)

	page := get(t, srv, listPath+"?q=5979").list(t)
	require.Len(t, page.Data, 1)
	d := page.Data[0]
	require.Equal(t, "5979", d.DocumentNumber)
	require.Equal(t, "1-Time Treatment (PestPros of MI)", d.DocumentType.Name)
	require.NotZero(t, d.DocumentType.ID)
	require.Equal(t, "Customer 1", d.Customer.Name)
	require.NotZero(t, d.Customer.ID)
	require.Equal(t, "63733", d.AccountNumber)
	require.Equal(t, "thon nguyen", *d.CreatedBy)
	require.Equal(t, "2026-10-20T11:40:10.000000Z", d.CreatedAt, "whole seconds still carry six digits")
	require.Equal(t, "draft", d.Status)

	signed := get(t, srv, listPath+"?status=signed&sort=document_number").list(t)
	require.Equal(t, []string{"5981", "5982"}, numbers(signed))
}

func TestNullableFieldsAreJSONNull(t *testing.T) {
	srv := newServer(t, true)

	var list map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"?q=X2").body, &list))
	d := list["data"].([]any)[0].(map[string]any)

	require.Contains(t, d, "created_by")
	require.Nil(t, d["created_by"])
}

func TestPagingOverHTTPFollowsNextCursorForEverySortAndOrder(t *testing.T) {
	srv := newServer(t, true)

	for _, key := range model.DocumentSortKeys {
		for _, order := range []string{"asc", "desc"} {
			t.Run(fmt.Sprintf("%s/%s", key, order), func(t *testing.T) {
				base := fmt.Sprintf("%s?sort=%s&order=%s", listPath, key, order)

				all := get(t, srv, base+"&limit=200").list(t)
				require.Len(t, all.Data, 21)

				var got []int64
				target := base + "&limit=5"
				for pages := 1; ; pages++ {
					require.LessOrEqual(t, pages, 10, "paging must terminate")

					page := get(t, srv, target).list(t)
					got = append(got, idsOf(page)...)

					if pages == 1 {
						require.NotNil(t, page.Summary)
						require.EqualValues(t, 21, page.Summary.Count)
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

				require.Equal(t, idsOf(all), got, "every document exactly once, in order")
			})
		}
	}
}

func TestMultiValueParametersAreEquivalentInEveryForm(t *testing.T) {
	srv := newServer(t, false)

	forms := []string{
		"?status=sent,signed&sort=document_number",
		"?status=sent&status=signed&sort=document_number",
		"?status=signed&status=sent&sort=document_number",
		"?status=%20sent%20,%20signed%20&sort=document_number",
		"?status=SENT,Signed&sort=document_number",
	}

	var reference []string
	for i, form := range forms {
		page := get(t, srv, listPath+form).list(t)
		require.Len(t, page.Data, 3, form)
		require.EqualValues(t, 3, page.Summary.Count, form)
		if i == 0 {
			reference = numbers(page)
			continue
		}
		require.Equal(t, reference, numbers(page), form)
	}
}

func TestFilters(t *testing.T) {
	srv := newServer(t, false)

	var f dto.DocumentFiltersResponse
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"/filters").body, &f))
	typeID := map[string]int64{}
	for _, ty := range f.DocumentTypes {
		typeID[ty.Name] = ty.ID
	}
	warranty := fmt.Sprint(typeID["1 Year Termite Warranty (Spidermen)"])
	subTermite := fmt.Sprint(typeID["1 Year SubTermite Agreement (LibertyPS)"])

	tests := []struct {
		name      string
		query     string
		wantCount int
	}{
		{"sent", "?status=sent", 1},
		{"signed", "?status=signed", 2},
		{"one type", "?document_type_id=" + warranty, 2},
		{"two types, comma separated", "?document_type_id=" + warranty + "," + subTermite, 8},
		{"two types, repeated", "?document_type_id=" + warranty + "&document_type_id=" + subTermite, 8},
		{"deleted tab", "?lifecycle=deleted", 1},
		{"one day", "?from=2026-10-21&to=2026-10-21", 2},
		{"the last day is included", "?from=2026-10-22&to=2026-10-22", 1},
		{"open start", "?to=2026-10-05", 3},
		{"open end", "?from=2026-10-21", 3},
		{"september is empty", "?from=2026-09-01&to=2026-09-30", 0},
		{"search by number", "?q=598", 3},
		{"search by account number", "?q=63733", 4},
		{"search by customer name with plus as space", "?q=Customer+1", 4},
		{"percent is literal", "?q=%25", 0},
		{"underscore is literal", "?q=59_9", 0},
		{"accented text matches nothing", "?q=%C3%A9", 0},
		{"injection text matches nothing", "?q=%27%3B+DROP+TABLE+documents%3B--", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := get(t, srv, listPath+tt.query).list(t)

			require.Len(t, page.Data, tt.wantCount)
			require.NotNil(t, page.Summary)
			require.EqualValues(t, tt.wantCount, page.Summary.Count)
		})
	}
}

func TestPageSizeAndHasMore(t *testing.T) {
	srv := newServer(t, false)

	page := get(t, srv, listPath+"?limit=18").list(t)
	require.Len(t, page.Data, 18)
	require.False(t, page.Page.HasMore, "an exactly full last page has no successor")
	require.Nil(t, page.Page.NextCursor)

	page = get(t, srv, listPath+"?limit=17").list(t)
	require.Len(t, page.Data, 17)
	require.True(t, page.Page.HasMore)
	require.NotNil(t, page.Page.NextCursor)
	require.EqualValues(t, 18, page.Summary.Count, "the count covers all pages")
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
		{"the date column is called created_at", "?sort=date", 422, "invalid_input", []string{"sort"}},
		{"estimate-only sort", "?sort=phone", 422, "invalid_input", []string{"sort"}},
		{"unknown order", "?order=sideways", 422, "invalid_input", []string{"order"}},
		{"archived tab does not exist", "?lifecycle=archived", 422, "invalid_input", []string{"lifecycle"}},
		{"unknown status", "?status=overdue", 422, "invalid_input", []string{"status"}},
		{"invoice status paid", "?status=paid", 422, "invalid_input", []string{"status"}},
		{"estimate status accepted", "?status=accepted", 422, "invalid_input", []string{"status"}},
		{"type id not a number", "?document_type_id=abc", 422, "invalid_input", []string{"document_type_id"}},
		{"type id zero", "?document_type_id=0", 422, "invalid_input", []string{"document_type_id"}},
		{"NUL in search", "?q=a%00b", 422, "invalid_input", []string{"q"}},
		{"invalid UTF-8 in search", "?q=%FF%FE", 422, "invalid_input", []string{"q"}},
		{"search too long", "?q=" + strings.Repeat("a", 101), 422, "invalid_input", []string{"q"}},
		{"garbage cursor", "?cursor=garbage", 422, "invalid_input", []string{"cursor"}},
		{"unknown parameter", "?statuss=draft", 422, "invalid_input", []string{"statuss"}},
		{"estimate parameter signature_status", "?signature_status=signed", 422, "invalid_input", []string{"signature_status"}},
		{"invoice parameter service_id", "?service_id=1", 422, "invalid_input", []string{"service_id"}},
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

func TestErrorMessagesNameTheDocumentChoices(t *testing.T) {
	srv := newServer(t, false)

	body := get(t, srv, listPath+"?status=paid&lifecycle=archived&sort=phone").errorBody(t)

	messages := map[string]string{}
	for _, d := range body.Details {
		messages[d.Field] = d.Message
	}
	require.Equal(t, "must be one of: draft, sent, signed", messages["status"])
	require.Equal(t, "must be one of: active, deleted", messages["lifecycle"])
	require.Contains(t, messages["sort"], "created_at")
	require.NotContains(t, messages["sort"], "phone")
}

func TestCursorFromAnotherSortIsRejected(t *testing.T) {
	srv := newServer(t, false)

	page := get(t, srv, listPath+"?limit=2").list(t)
	require.NotNil(t, page.Page.NextCursor)
	cursor := url.QueryEscape(*page.Page.NextCursor)

	for _, other := range []string{"&sort=status&order=desc", "&order=asc"} {
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
	require.Equal(t, []string{"count"}, keysOf(list["summary"].(map[string]any)))

	document := list["data"].([]any)[0].(map[string]any)
	require.Equal(t, []string{
		"account_number", "created_at", "created_by", "customer", "document_number", "document_type", "id", "status",
	}, keysOf(document))
	require.Equal(t, []string{"id", "name"}, keysOf(document["customer"].(map[string]any)))
	require.Equal(t, []string{"id", "name"}, keysOf(document["document_type"].(map[string]any)))

	// ids and counts are numbers, timestamps are strings.
	require.IsType(t, "", document["created_at"])
	require.IsType(t, float64(0), document["id"])
	require.IsType(t, float64(0), list["summary"].(map[string]any)["count"])

	var filters map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"/filters").body, &filters))
	require.Equal(t, []string{
		"default_sort", "document_types", "lifecycles", "limits", "sort_keys", "statuses",
	}, keysOf(filters))

	var errBody map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"?limit=0").body, &errBody))
	require.Equal(t, []string{"error"}, keysOf(errBody))
	require.Equal(t, []string{"code", "details", "message", "request_id"}, keysOf(errBody["error"].(map[string]any)))
}

func TestAnEmptyResultIsAnEmptyArrayOnTheWire(t *testing.T) {
	srv := newServer(t, false)

	resp := get(t, srv, listPath+"?from=2026-09-01&to=2026-09-30")

	require.Equal(t, http.StatusOK, resp.status)
	require.Contains(t, string(resp.body), `"data":[]`, "never null, so clients can iterate without a check")
	require.Contains(t, string(resp.body), `"summary":{"count":0}`)
}

func TestFiltersEndpointMatchesWhatTheListAccepts(t *testing.T) {
	srv := newServer(t, false)

	var f dto.DocumentFiltersResponse
	resp := get(t, srv, listPath+"/filters")
	require.Equal(t, http.StatusOK, resp.status)
	require.Equal(t, "no-store", resp.header.Get("Cache-Control"))
	require.NoError(t, json.Unmarshal(resp.body, &f))

	require.Len(t, f.DocumentTypes, 3)
	for _, ty := range f.DocumentTypes {
		require.NotZero(t, ty.ID)
		require.NotEmpty(t, ty.Name)
	}
	require.Equal(t, []string{"draft", "sent", "signed"}, f.Statuses)
	require.Equal(t, []string{"active", "deleted"}, f.Lifecycles)
	require.Len(t, f.SortKeys, 7)
	require.Equal(t, dto.SortResponse{Key: "created_at", Order: "desc"}, f.DefaultSort)
	require.Equal(t, dto.PageLimitsResponse{Default: 50, Max: 200}, f.Limits)

	// Everything the endpoint advertises is accepted by the list endpoint.
	for _, key := range f.SortKeys {
		require.Equal(t, http.StatusOK, get(t, srv, listPath+"?sort="+key).status, key)
	}
	for _, s := range f.Statuses {
		require.Equal(t, http.StatusOK, get(t, srv, listPath+"?status="+s).status, s)
	}
	for _, l := range f.Lifecycles {
		require.Equal(t, http.StatusOK, get(t, srv, listPath+"?lifecycle="+l).status, l)
	}
	for _, ty := range f.DocumentTypes {
		require.Equal(t, http.StatusOK, get(t, srv, fmt.Sprintf("%s?document_type_id=%d", listPath, ty.ID)).status)
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
			key := string(model.DocumentSortKeys[i%len(model.DocumentSortKeys)])
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
		var page dto.DocumentListResponse
		require.NoError(t, json.Unmarshal(r.body, &page))
		require.Len(t, page.Data, 18, "worker %d (sort %s)", i, r.sortKey)
		require.EqualValues(t, 18, page.Summary.Count)
		require.Equal(t, sorted(seedNumbers), sorted(numbers(page)))
	}
}
