//go:build integration

package invoicereport_test

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
	listPath      = "/api/v1/reports/invoices"
)

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

// newServer starts the real handler stack (middleware, router, handler, service, repository) behind a real HTTP
// listener, on a seeded database. With withNulls it adds October invoices that have no job, email or payment terms.
func newServer(t *testing.T, withNulls bool) *httptest.Server {
	t.Helper()

	pool := testdb.SeededPool(t)
	if withNulls {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO billing_documents (document_number, customer_id, document_date, total, discount, status)
			SELECT 'N' || g, c.id, '2026-10-10', 10 * g, g, 'draft'
			FROM generate_series(1, 3) g
			JOIN customers c ON c.account_number = '63716'`)
		require.NoError(t, err)
	}

	logger := slog.New(slog.DiscardHandler)
	svc := service.NewInvoiceService(repository.NewInvoiceRepository(pool), func() time.Time { return october21 })
	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, []string{allowedOrigin}),
		Reports:    []handler.RouteRegistrar{handler.NewInvoiceHandler(svc, logger)},
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

func (r response) list(t *testing.T) dto.InvoiceListResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, r.status, "GET %s -> %s", r.request, r.body)
	var out dto.InvoiceListResponse
	require.NoError(t, json.Unmarshal(r.body, &out))
	return out
}

func (r response) errorBody(t *testing.T) dto.ErrorBody {
	t.Helper()
	var out dto.ErrorResponse
	require.NoError(t, json.Unmarshal(r.body, &out), "body: %s", r.body)
	return out.Error
}

func numbers(l dto.InvoiceListResponse) []string {
	out := make([]string, len(l.Data))
	for i, d := range l.Data {
		out[i] = d.InvoiceNumber
	}
	return out
}

func idsOf(l dto.InvoiceListResponse) []int64 {
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

func TestDefaultRequest(t *testing.T) {
	srv := newServer(t, false)

	resp := get(t, srv, listPath)
	page := resp.list(t)

	require.Len(t, page.Data, 21, "the active October invoices")
	require.Equal(t, 50, page.Page.Limit)
	require.False(t, page.Page.HasMore)
	require.Nil(t, page.Page.NextCursor)
	require.Equal(t, "2026-10-01", *page.Range.From)
	require.Equal(t, "2026-10-31", *page.Range.To)

	require.NotNil(t, page.Summary)
	require.EqualValues(t, 21, page.Summary.Count)
	require.Equal(t, "264.00", page.Summary.Outstanding)
	require.Equal(t, "1.23", page.Summary.Discount)

	// The summary must equal the sum of the rows it describes.
	var total model.Money
	for _, d := range page.Data {
		m, err := model.ParseMoney(d.Total)
		require.NoError(t, err)
		total += m
	}
	require.Equal(t, total.String(), page.Summary.Total)

	first := page.Data[0]
	require.Equal(t, "84936", first.InvoiceNumber, "newest first")
	require.Equal(t, "2026-10-26", first.Date)
	require.Equal(t, "5001", first.AccountNumber)
	require.Equal(t, "William Saliba", first.Customer.Name)
	require.Equal(t, "Weekly", first.Frequency)
	require.Equal(t, "65.00", first.Total)
	require.Equal(t, "draft", first.Status)
	require.Equal(t, []string{}, first.Tags)
	require.NotNil(t, first.PaymentTerms)
	require.Equal(t, "Due on receipt", first.PaymentTerms.Name)

	require.Equal(t, "no-store", resp.header.Get("Cache-Control"))
	require.Contains(t, resp.header.Get("Content-Type"), "application/json")
	require.NotEmpty(t, resp.header.Get("X-Request-ID"))
}

func TestInvoiceWithTagsAndNetTerms(t *testing.T) {
	srv := newServer(t, false)

	page := get(t, srv, listPath+"?q=85005").list(t)

	require.Len(t, page.Data, 1)
	inv := page.Data[0]
	require.Equal(t, []string{"a", "b"}, inv.Tags)
	require.Equal(t, "Net 30 Terms", inv.PaymentTerms.Name)
	require.Contains(t, inv.PaymentTerms.Description, "30 days")
	require.Equal(t, "152.26", inv.Total)
	require.NotNil(t, inv.Job)
	require.Equal(t, "Blank Service 123 (S1 S2)", *inv.Job)
}

func TestNullableFieldsAreJSONNull(t *testing.T) {
	srv := newServer(t, true)

	resp := get(t, srv, listPath+"?q=N2")
	var raw struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp.body, &raw))

	require.Len(t, raw.Data, 1)
	inv := raw.Data[0]
	require.Contains(t, inv, "job")
	require.Nil(t, inv["job"])
	require.Contains(t, inv, "billing_email")
	require.Nil(t, inv["billing_email"])
	require.Contains(t, inv, "payment_terms")
	require.Nil(t, inv["payment_terms"])
	require.Equal(t, []any{}, inv["tags"])
}

func TestPagingOverHTTPFollowsNextCursorForEverySortAndOrder(t *testing.T) {
	srv := newServer(t, true)

	for _, key := range model.InvoiceSortKeys {
		for _, order := range []string{"asc", "desc"} {
			t.Run(fmt.Sprintf("%s/%s", key, order), func(t *testing.T) {
				base := fmt.Sprintf("%s?sort=%s&order=%s", listPath, key, order)

				all := get(t, srv, base+"&limit=200").list(t)
				require.Len(t, all.Data, 24)

				var got []int64
				target := base + "&limit=5"
				for pages := 1; ; pages++ {
					require.LessOrEqual(t, pages, 10, "paging must terminate")

					page := get(t, srv, target).list(t)
					got = append(got, idsOf(page)...)

					if pages == 1 {
						require.NotNil(t, page.Summary)
						require.EqualValues(t, 24, page.Summary.Count)
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

				require.Equal(t, idsOf(all), got, "every invoice exactly once, in order")
			})
		}
	}
}

func TestMultiValueParametersAreEquivalentInEveryForm(t *testing.T) {
	srv := newServer(t, false)

	forms := []string{
		"?status=paid,sent&sort=invoice_number",
		"?status=paid&status=sent&sort=invoice_number",
		"?status=sent&status=paid&sort=invoice_number",
		"?status=%20paid%20,%20sent%20&sort=invoice_number",
		"?status=PAID,Sent&sort=invoice_number",
	}

	var reference []string
	for i, form := range forms {
		page := get(t, srv, listPath+form).list(t)
		require.Len(t, page.Data, 12, form)
		require.EqualValues(t, 12, page.Summary.Count, form)
		if i == 0 {
			reference = numbers(page)
			continue
		}
		require.Equal(t, reference, numbers(page), form)
	}
}

func TestFiltersUsingIDsFromTheFiltersEndpoint(t *testing.T) {
	srv := newServer(t, false)

	var f dto.InvoiceFiltersResponse
	resp := get(t, srv, listPath+"/filters")
	require.Equal(t, http.StatusOK, resp.status)
	require.NoError(t, json.Unmarshal(resp.body, &f))

	idOf := func(opts []dto.OptionResponse, name string) int64 {
		for _, o := range opts {
			if o.Name == name {
				return o.ID
			}
		}
		t.Fatalf("option %q not offered", name)
		return 0
	}
	termID := func(name string) int64 {
		for _, o := range f.PaymentTerms {
			if o.Name == name {
				return o.ID
			}
		}
		t.Fatalf("payment term %q not offered", name)
		return 0
	}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"service Bi-Monthly", fmt.Sprintf("?service_id=%d", idOf(f.ServiceTypes, "Bi-Monthly Service")),
			[]string{"84936", "84942", "85007", "84937", "85000", "84985", "84979", "84975", "85031", "85011", "85010", "85009", "85004", "85032"}},
		{"service Blank or Initial", fmt.Sprintf("?service_id=%d,%d", idOf(f.ServiceTypes, "Blank Service 123"), idOf(f.ServiceTypes, "Initial Service")),
			[]string{"85030", "85028", "85005", "84984", "84983", "84982", "85033"}},
		{"tag a", fmt.Sprintf("?tag_id=%d", idOf(f.Tags, "a")), []string{"85030", "85028", "85005"}},
		{"tags b or c, any-of without duplicates", fmt.Sprintf("?tag_id=%d&tag_id=%d", idOf(f.Tags, "b"), idOf(f.Tags, "c")),
			[]string{"85030", "85028", "85005"}},
		{"payment terms Net 30", fmt.Sprintf("?payment_term_id=%d", termID("Net 30 Terms")), []string{"85005"}},
		{"unknown id matches nothing, and is not an error", "?service_id=999999", nil},
		{"archived tab", "?lifecycle=archived", []string{"85034"}},
		{"deleted tab", "?lifecycle=DELETED", []string{"85035"}},
		{"signature signed", "?signature_status=signed", []string{"85032"}},
		{"single day, inclusive", "?from=2026-10-12&to=2026-10-12", []string{"84937", "85009", "85000"}},
		{"open-ended from", "?from=2026-10-22", []string{"84936", "85031"}},
		{"September", "?from=2026-09-01&to=2026-09-30", []string{"84890"}},

		{"search customer, any case", "?q=SaLiBa", []string{"84936", "84942", "85007", "84937", "85000", "84985", "84979", "84975", "85031"}},
		{"search email, encoded @", "?q=d%40cm.co", []string{"85030", "85028", "85005"}},
		{"search with a plus is a space", "?q=William+Saliba", []string{"84936", "84942", "85007", "84937", "85000", "84985", "84979", "84975", "85031"}},
		{"search with an encoded space", "?q=William%20Saliba", []string{"84936", "84942", "85007", "84937", "85000", "84985", "84979", "84975", "85031"}},
		{"percent is literal", "?q=%25", nil},
		{"underscore is literal", "?q=8503_", nil},
		{"accented text", "?q=%C3%A9", nil},
		{"sql injection text", "?q=%27%3B+DROP+TABLE+invoices%3B+--", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := get(t, srv, listPath+tt.query+sep(tt.query)+"limit=200").list(t)

			require.ElementsMatch(t, tt.want, numbers(page))
			require.NotNil(t, page.Summary)
			require.EqualValues(t, len(tt.want), page.Summary.Count, "the summary describes exactly the listed set")
		})
	}

	// the hostile search did no harm
	require.Equal(t, http.StatusOK, get(t, srv, "/readyz").status)
	require.Len(t, get(t, srv, listPath).list(t).Data, 21)
}

func sep(query string) string {
	if query == "" {
		return "?"
	}
	return "&"
}

func TestSummaryFollowsFilters(t *testing.T) {
	srv := newServer(t, false)

	sent := get(t, srv, listPath+"?status=sent").list(t)
	require.EqualValues(t, 3, sent.Summary.Count)
	require.Equal(t, "294.00", sent.Summary.Total)
	require.Equal(t, "264.00", sent.Summary.Outstanding)

	paid := get(t, srv, listPath+"?status=paid").list(t)
	require.Equal(t, "0.00", paid.Summary.Outstanding, "paid invoices owe nothing")

	one := get(t, srv, listPath+"?q=85032").list(t)
	require.Equal(t, "100.00", one.Summary.Total)
	require.Equal(t, "70.00", one.Summary.Outstanding, "100.00 less the 30.00 payment")
}

func TestPageSizeAndHasMore(t *testing.T) {
	srv := newServer(t, false)

	full := get(t, srv, listPath+"?limit=21").list(t)
	require.Len(t, full.Data, 21)
	require.False(t, full.Page.HasMore)

	partial := get(t, srv, listPath+"?limit=20").list(t)
	require.Len(t, partial.Data, 20)
	require.True(t, partial.Page.HasMore)
	require.NotNil(t, partial.Page.NextCursor)
	require.Equal(t, 20, partial.Page.Limit)
	require.EqualValues(t, 21, partial.Summary.Count, "the summary covers the whole set, not the page")
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
		{"limit not a number", "?limit=lots", 422, "invalid_input", []string{"limit"}},
		{"impossible date", "?from=2026-02-30", 422, "invalid_input", []string{"from"}},
		{"to before from", "?from=2026-10-10&to=2026-10-09", 422, "invalid_input", []string{"to"}},
		{"unknown sort", "?sort=bogus", 422, "invalid_input", []string{"sort"}},
		{"unknown order", "?order=sideways", 422, "invalid_input", []string{"order"}},
		{"unknown lifecycle", "?lifecycle=everything", 422, "invalid_input", []string{"lifecycle"}},
		{"unknown status", "?status=overdue", 422, "invalid_input", []string{"status"}},
		{"unknown signature status", "?signature_status=maybe", 422, "invalid_input", []string{"signature_status"}},
		{"id not a number", "?service_id=abc", 422, "invalid_input", []string{"service_id"}},
		{"id zero", "?tag_id=0", 422, "invalid_input", []string{"tag_id"}},
		{"NUL in search", "?q=a%00b", 422, "invalid_input", []string{"q"}},
		{"invalid UTF-8 in search", "?q=%FF%FE", 422, "invalid_input", []string{"q"}},
		{"search too long", "?q=" + string(make([]byte, 0)) + repeatByte('a', 101), 422, "invalid_input", []string{"q"}},
		{"garbage cursor", "?cursor=garbage", 422, "invalid_input", []string{"cursor"}},
		{"unknown parameter", "?statuss=paid", 422, "invalid_input", []string{"statuss"}},
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

func repeatByte(b byte, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return string(out)
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
	require.Equal(t, []string{"count", "discount", "outstanding", "total"}, keysOf(list["summary"].(map[string]any)))

	invoice := list["data"].([]any)[0].(map[string]any)
	require.Equal(t, []string{
		"account_number", "billing_email", "customer", "date", "discount", "frequency", "id", "invoice_number",
		"job", "payment_terms", "signature_status", "status", "tags", "total",
	}, keysOf(invoice))
	require.Equal(t, []string{"id", "name"}, keysOf(invoice["customer"].(map[string]any)))
	require.Equal(t, []string{"description", "name"}, keysOf(invoice["payment_terms"].(map[string]any)))

	// Money is always a string, ids and counts are numbers.
	require.IsType(t, "", invoice["total"])
	require.IsType(t, "", invoice["discount"])
	require.IsType(t, "", list["summary"].(map[string]any)["outstanding"])
	require.IsType(t, float64(0), invoice["id"])
	require.IsType(t, float64(0), list["summary"].(map[string]any)["count"])

	var filters map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"/filters").body, &filters))
	require.Equal(t, []string{
		"default_sort", "lifecycles", "limits", "payment_terms", "service_types", "signature_statuses",
		"sort_keys", "statuses", "tags",
	}, keysOf(filters))

	var errBody map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"?limit=0").body, &errBody))
	require.Equal(t, []string{"error"}, keysOf(errBody))
	require.Equal(t, []string{"code", "details", "message", "request_id"}, keysOf(errBody["error"].(map[string]any)))
}

func TestFiltersEndpointReflectsTheDatabase(t *testing.T) {
	srv := newServer(t, false)

	var f dto.InvoiceFiltersResponse
	resp := get(t, srv, listPath+"/filters")
	require.Equal(t, http.StatusOK, resp.status)
	require.Equal(t, "no-store", resp.header.Get("Cache-Control"))
	require.NoError(t, json.Unmarshal(resp.body, &f))

	require.Len(t, f.ServiceTypes, 5)
	require.Len(t, f.Tags, 3)
	require.Len(t, f.PaymentTerms, 2)
	require.Equal(t, "Due on receipt", f.PaymentTerms[0].Name)
	require.Equal(t, []string{"draft", "sent", "paid", "void"}, f.Statuses)
	require.Equal(t, []string{"active", "archived", "deleted"}, f.Lifecycles)
	require.Len(t, f.SortKeys, 11)
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
			key := string(model.InvoiceSortKeys[i%len(model.InvoiceSortKeys)])
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
		var page dto.InvoiceListResponse
		require.NoError(t, json.Unmarshal(r.body, &page))
		require.Len(t, page.Data, 21, "worker %d (sort %s)", i, r.sortKey)
		require.Equal(t, "264.00", page.Summary.Outstanding)
		require.Equal(t, sorted(numbersOfSeed()), sorted(numbers(page)))
	}
}

func numbersOfSeed() []string {
	return []string{
		"84936", "85030", "85011", "85010", "84942", "85028", "85007", "84937", "85009", "85000", "85005",
		"85004", "84985", "84979", "84984", "84983", "84982", "84975", "85031", "85032", "85033",
	}
}
