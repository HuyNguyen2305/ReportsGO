//go:build integration

package creditreport_test

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
	"github.com/jackc/pgx/v5/pgxpool"
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
	allowedOrigin  = "https://app.example.com"
	listPath       = "/api/v1/reports/credits"
	octoberCredits = 7
)

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

type server struct {
	*httptest.Server
	pool *pgxpool.Pool
}

// newServer starts the real handler stack (middleware, router, handler, service, repository) behind a real HTTP
// listener, on a seeded database.
func newServer(t *testing.T) server {
	t.Helper()

	pool := testdb.SeededPool(t)
	logger := slog.New(slog.DiscardHandler)
	svc := service.NewCreditService(repository.NewCreditRepository(pool), func() time.Time { return october21 })
	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, []string{allowedOrigin}),
		Reports:    []handler.RouteRegistrar{handler.NewCreditHandler(svc, logger)},
	})

	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	return server{Server: srv, pool: pool}
}

type response struct {
	status  int
	header  http.Header
	body    []byte
	request string // the request path and query, for failure messages
}

func send(t *testing.T, srv server, method, target string, headers map[string]string) response {
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

func get(t *testing.T, srv server, target string) response {
	t.Helper()
	return send(t, srv, http.MethodGet, target, nil)
}

func (r response) list(t *testing.T) dto.CreditListResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, r.status, "GET %s -> %s", r.request, r.body)
	var out dto.CreditListResponse
	require.NoError(t, json.Unmarshal(r.body, &out))
	return out
}

func (r response) errorBody(t *testing.T) dto.ErrorBody {
	t.Helper()
	var out dto.ErrorResponse
	require.NoError(t, json.Unmarshal(r.body, &out), "body: %s", r.body)
	return out.Error
}

func idsOf(l dto.CreditListResponse) []int64 {
	out := make([]int64, len(l.Data))
	for i, d := range l.Data {
		out[i] = d.ID
	}
	return out
}

func TestDefaultRequest(t *testing.T) {
	srv := newServer(t)
	var wantTotal string
	require.NoError(t, srv.pool.QueryRow(context.Background(), `
		SELECT to_char(sum(amount), 'FM999999990.00') FROM credits
		WHERE credit_date BETWEEN '2026-10-01' AND '2026-10-31'`).Scan(&wantTotal))

	resp := get(t, srv, listPath)
	page := resp.list(t)

	require.Len(t, page.Data, octoberCredits)
	require.Equal(t, 50, page.Page.Limit)
	require.False(t, page.Page.HasMore)
	require.Nil(t, page.Page.NextCursor)
	require.NotNil(t, page.Summary)
	require.EqualValues(t, octoberCredits, page.Summary.Count)
	require.Equal(t, wantTotal, page.Summary.Total, "the total matches an independent query")
	require.Equal(t, "2026-10-01", *page.Range.From)
	require.Equal(t, "2026-10-31", *page.Range.To)
	require.Equal(t, "no-store", resp.header.Get("Cache-Control"))
	require.NotEmpty(t, resp.header.Get("X-Request-ID"))
	require.Equal(t, "2026-10-21", page.Data[0].CreditDate, "newest first")
}

func strPtr(s string) *string { return &s }

func TestCreditFields(t *testing.T) {
	srv := newServer(t)

	page := get(t, srv, listPath+"?q=4242").list(t)

	require.Len(t, page.Data, 1)
	c := page.Data[0]
	require.NotZero(t, c.ID)
	require.Equal(t, "Customer 1", c.Customer.Name)
	require.NotZero(t, c.Customer.ID)
	require.Equal(t, "63733", c.AccountNumber)
	require.Equal(t, dto.CreditMethodResponse{Type: "card", Detail: "Stripe **** 4242 Visa"}, c.Method)
	require.Equal(t, "20.00", c.Amount)
	require.Equal(t, "2026-10-15", c.CreditDate)
	require.Equal(t, strPtr("thon nguyen"), c.CreatedBy)
}

func TestMissingCreatorIsNullAndMissingDetailIsEmpty(t *testing.T) {
	srv := newServer(t)

	var list map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"?method=other").body, &list))
	c := list["data"].([]any)[0].(map[string]any)
	require.Contains(t, c, "created_by")
	require.Nil(t, c["created_by"], "an unknown creator is JSON null")

	page := get(t, srv, listPath+"?method=cash&q=63717").list(t)
	require.Len(t, page.Data, 1)
	require.Equal(t, "", page.Data[0].Method.Detail, "no stored detail is an empty string, not null")
}

func TestPagingOverHTTPFollowsNextCursorForEverySortAndOrder(t *testing.T) {
	srv := newServer(t)

	for _, key := range model.CreditSortKeys {
		for _, order := range []string{"asc", "desc"} {
			for _, limit := range []int{1, 3} {
				t.Run(fmt.Sprintf("%s/%s/limit=%d", key, order, limit), func(t *testing.T) {
					base := fmt.Sprintf("%s?sort=%s&order=%s", listPath, key, order)

					all := get(t, srv, base+"&limit=200").list(t)
					require.Len(t, all.Data, octoberCredits)

					var got []int64
					target := fmt.Sprintf("%s&limit=%d", base, limit)
					for pages := 1; ; pages++ {
						require.LessOrEqual(t, pages, 20, "paging must terminate")

						page := get(t, srv, target).list(t)
						got = append(got, idsOf(page)...)

						if pages == 1 {
							require.NotNil(t, page.Summary)
							require.EqualValues(t, octoberCredits, page.Summary.Count)
							require.Equal(t, all.Summary.Total, page.Summary.Total)
						} else {
							require.Nil(t, page.Summary, "only the first page carries the summary")
						}

						if page.Page.NextCursor == nil {
							require.False(t, page.Page.HasMore)
							break
						}
						require.True(t, page.Page.HasMore)
						target = fmt.Sprintf("%s&limit=%d&cursor=%s", base, limit, url.QueryEscape(*page.Page.NextCursor))
					}

					require.Equal(t, idsOf(all), got, "every credit exactly once, in order")
				})
			}
		}
	}
}

func TestMethodParameterIsEquivalentInEveryForm(t *testing.T) {
	srv := newServer(t)

	forms := []string{
		"?method=check,other",
		"?method=check&method=other",
		"?method=other&method=check",
		"?method=%20check%20,%20other%20",
		"?method=CHECK,Other",
	}

	var reference []int64
	for i, form := range forms {
		page := get(t, srv, listPath+form+"&sort=credit_date&order=asc").list(t)
		require.Len(t, page.Data, 3, form)
		require.EqualValues(t, 3, page.Summary.Count, form)
		if i == 0 {
			reference = idsOf(page)
			continue
		}
		require.Equal(t, reference, idsOf(page), form)
	}
}

func TestFilters(t *testing.T) {
	srv := newServer(t)

	tests := []struct {
		name      string
		query     string
		wantCount int
	}{
		{"cash", "?method=cash", 2},
		{"check", "?method=check", 2},
		{"card", "?method=card", 2},
		{"other", "?method=other", 1},
		{"one day", "?from=2026-10-15&to=2026-10-15", 5},
		{"the last day is included", "?from=2026-10-21&to=2026-10-21", 1},
		{"open start", "?to=2026-10-12", 2},
		{"open end", "?from=2026-10-16", 1},
		{"september", "?from=2026-09-01&to=2026-09-30", 1},
		{"search by customer name with plus as space", "?q=Customer+1", 2},
		{"search by account number", "?q=63706", 2},
		{"search by method detail", "?q=stripe", 2},
		{"search by method detail is case-insensitive", "?q=WIRE", 1},
		{"search for asterisks in card details", "?q=%2A%2A%2A%2A+3222", 1},
		{"percent is literal", "?q=%25", 0},
		{"underscore is literal", "?q=Che_k", 0},
		{"accented text matches nothing", "?q=%C3%A9", 0},
		{"injection text matches nothing", "?q=%27%3B+DROP+TABLE+credits%3B--", 0},
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
	srv := newServer(t)

	page := get(t, srv, listPath+"?limit=7").list(t)
	require.Len(t, page.Data, 7)
	require.False(t, page.Page.HasMore, "an exactly full last page has no successor")
	require.Nil(t, page.Page.NextCursor)

	page = get(t, srv, listPath+"?limit=6").list(t)
	require.Len(t, page.Data, 6)
	require.True(t, page.Page.HasMore)
	require.NotNil(t, page.Page.NextCursor)
	require.EqualValues(t, 7, page.Summary.Count, "the count covers all pages")
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
		{"the date column is called credit_date", "?sort=date", 422, "invalid_input", []string{"sort"}},
		{"another report's sort", "?sort=payment_date", 422, "invalid_input", []string{"sort"}},
		{"credits have no status to sort by", "?sort=status", 422, "invalid_input", []string{"sort"}},
		{"unknown order", "?order=sideways", 422, "invalid_input", []string{"order"}},
		{"unknown method", "?method=wire", 422, "invalid_input", []string{"method"}},
		{"a payment status is not a method", "?method=paid", 422, "invalid_input", []string{"method"}},
		{"NUL in search", "?q=a%00b", 422, "invalid_input", []string{"q"}},
		{"invalid UTF-8 in search", "?q=%FF%FE", 422, "invalid_input", []string{"q"}},
		{"search too long", "?q=" + strings.Repeat("a", 101), 422, "invalid_input", []string{"q"}},
		{"garbage cursor", "?cursor=garbage", 422, "invalid_input", []string{"cursor"}},
		{"unknown parameter", "?methodd=cash", 422, "invalid_input", []string{"methodd"}},
		{"credits have no tabs", "?lifecycle=active", 422, "invalid_input", []string{"lifecycle"}},
		{"credits have no status", "?status=paid", 422, "invalid_input", []string{"status"}},
		{"estimate parameter signature_status", "?signature_status=signed", 422, "invalid_input", []string{"signature_status"}},
		{"invoice parameter service_id", "?service_id=1", 422, "invalid_input", []string{"service_id"}},
		{"document parameter document_type_id", "?document_type_id=1", 422, "invalid_input", []string{"document_type_id"}},
		{"duplicated parameter", "?limit=1&limit=2", 422, "invalid_input", []string{"limit"}},
		{"several field problems are reported together", "?limit=0&sort=bogus&method=y", 422, "invalid_input", []string{"limit", "sort", "method"}},
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

func TestErrorMessagesNameTheCreditChoices(t *testing.T) {
	srv := newServer(t)

	body := get(t, srv, listPath+"?method=wire&sort=status").errorBody(t)

	messages := map[string]string{}
	for _, d := range body.Details {
		messages[d.Field] = d.Message
	}
	require.Equal(t, "must be one of: cash, check, card, other", messages["method"])
	require.Contains(t, messages["sort"], "credit_date")
	require.NotContains(t, messages["sort"], "status")
}

func TestCursorFromAnotherSortIsRejected(t *testing.T) {
	srv := newServer(t)

	page := get(t, srv, listPath+"?limit=2").list(t)
	require.NotNil(t, page.Page.NextCursor)
	cursor := url.QueryEscape(*page.Page.NextCursor)

	for _, other := range []string{"&sort=amount&order=desc", "&order=asc"} {
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
	srv := newServer(t)

	var list map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"?limit=1").body, &list))

	require.Equal(t, []string{"data", "page", "range", "summary"}, keysOf(list))
	require.Equal(t, []string{"has_more", "limit", "next_cursor"}, keysOf(list["page"].(map[string]any)))
	require.Equal(t, []string{"from", "to"}, keysOf(list["range"].(map[string]any)))
	require.Equal(t, []string{"count", "total"}, keysOf(list["summary"].(map[string]any)))

	credit := list["data"].([]any)[0].(map[string]any)
	require.Equal(t, []string{
		"account_number", "amount", "created_by", "credit_date", "customer", "id", "method",
	}, keysOf(credit))
	require.Equal(t, []string{"id", "name"}, keysOf(credit["customer"].(map[string]any)))
	require.Equal(t, []string{"detail", "type"}, keysOf(credit["method"].(map[string]any)))

	// money is a string, ids and counts are numbers.
	require.IsType(t, "", credit["amount"])
	require.IsType(t, "", list["summary"].(map[string]any)["total"])
	require.IsType(t, float64(0), credit["id"])
	require.IsType(t, float64(0), list["summary"].(map[string]any)["count"])

	var filters map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"/filters").body, &filters))
	require.Equal(t, []string{"default_sort", "limits", "methods", "sort_keys"}, keysOf(filters))

	var errBody map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"?limit=0").body, &errBody))
	require.Equal(t, []string{"error"}, keysOf(errBody))
	require.Equal(t, []string{"code", "details", "message", "request_id"}, keysOf(errBody["error"].(map[string]any)))
}

func TestAnEmptyResultIsAnEmptyArrayOnTheWire(t *testing.T) {
	srv := newServer(t)

	resp := get(t, srv, listPath+"?from=2027-01-01&to=2027-01-31")

	require.Equal(t, http.StatusOK, resp.status)
	require.Contains(t, string(resp.body), `"data":[]`, "never null, so clients can iterate without a check")
	require.Contains(t, string(resp.body), `"summary":{"count":0,"total":"0.00"}`)
}

func TestFiltersEndpointMatchesWhatTheListAccepts(t *testing.T) {
	srv := newServer(t)

	var f dto.CreditFiltersResponse
	resp := get(t, srv, listPath+"/filters")
	require.Equal(t, http.StatusOK, resp.status)
	require.Equal(t, "no-store", resp.header.Get("Cache-Control"))
	require.NoError(t, json.Unmarshal(resp.body, &f))

	require.Equal(t, []string{"cash", "check", "card", "other"}, f.Methods)
	require.Equal(t, []string{"customer", "account_number", "method", "amount", "credit_date", "created_by"}, f.SortKeys)
	require.Equal(t, dto.SortResponse{Key: "credit_date", Order: "desc"}, f.DefaultSort)
	require.Equal(t, dto.PageLimitsResponse{Default: 50, Max: 200}, f.Limits)

	// Everything the endpoint advertises is accepted by the list endpoint.
	for _, key := range f.SortKeys {
		require.Equal(t, http.StatusOK, get(t, srv, listPath+"?sort="+key).status, key)
	}
	for _, m := range f.Methods {
		require.Equal(t, http.StatusOK, get(t, srv, listPath+"?method="+m).status, m)
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
			key := string(model.CreditSortKeys[i%len(model.CreditSortKeys)])
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

	var reference []int64
	for i, r := range results {
		require.Equal(t, http.StatusOK, r.status, "worker %d (sort %s)", i, r.sortKey)
		var page dto.CreditListResponse
		require.NoError(t, json.Unmarshal(r.body, &page))
		require.Len(t, page.Data, octoberCredits, "worker %d (sort %s)", i, r.sortKey)
		require.EqualValues(t, octoberCredits, page.Summary.Count)

		ids := idsOf(page)
		sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
		if reference == nil {
			reference = ids
		}
		require.Equal(t, reference, ids, "every sort returns the same set of credits")
	}
}
