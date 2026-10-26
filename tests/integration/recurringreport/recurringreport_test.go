//go:build integration

package recurringreport_test

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
	allowedOrigin = "https://app.example.com"
	listPath      = "/api/v1/reports/recurring-invoices"
	allRecurring  = 7
)

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
	svc := service.NewRecurringService(repository.NewRecurringRepository(pool))
	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, []string{allowedOrigin}),
		Reports:    []handler.RouteRegistrar{handler.NewRecurringHandler(svc, logger)},
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

func (r response) list(t *testing.T) dto.RecurringListResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, r.status, "GET %s -> %s", r.request, r.body)
	var out dto.RecurringListResponse
	require.NoError(t, json.Unmarshal(r.body, &out))
	return out
}

func (r response) errorBody(t *testing.T) dto.ErrorBody {
	t.Helper()
	var out dto.ErrorResponse
	require.NoError(t, json.Unmarshal(r.body, &out), "body: %s", r.body)
	return out.Error
}

func idsOf(l dto.RecurringListResponse) []int64 {
	out := make([]int64, len(l.Data))
	for i, d := range l.Data {
		out[i] = d.ID
	}
	return out
}

func labelsOf(l dto.RecurringListResponse) []string {
	out := make([]string, len(l.Data))
	for i, d := range l.Data {
		out[i] = d.Repeats.Label
	}
	return out
}

func TestDefaultRequest(t *testing.T) {
	srv := newServer(t)
	var wantTotal string
	require.NoError(t, srv.pool.QueryRow(context.Background(),
		`SELECT to_char(sum(total_value), 'FM999999990.00') FROM recurring_invoices`).Scan(&wantTotal))

	resp := get(t, srv, listPath)
	page := resp.list(t)

	require.Len(t, page.Data, allRecurring, "there is no default date range")
	require.Equal(t, 50, page.Page.Limit)
	require.False(t, page.Page.HasMore)
	require.Nil(t, page.Page.NextCursor)
	require.NotNil(t, page.Summary)
	require.EqualValues(t, allRecurring, page.Summary.Count)
	require.Equal(t, wantTotal, page.Summary.Total, "the total matches an independent query")
	require.Equal(t, "713.49", page.Summary.Total)
	require.Nil(t, page.Range.From, "no dates were sent, so the range is open")
	require.Nil(t, page.Range.To)
	require.Equal(t, "no-store", resp.header.Get("Cache-Control"))
	require.NotEmpty(t, resp.header.Get("X-Request-ID"))
	require.Equal(t, "2026-10-15", page.Data[0].StartedOn, "newest start first")
}

func strPtr(s string) *string { return &s }
func intPtr(v int) *int       { return &v }

func TestAllPartsOfARecurringInvoiceWithANextInvoice(t *testing.T) {
	srv := newServer(t)

	page := get(t, srv, listPath+"?q=85032").list(t)

	require.Len(t, page.Data, 1)
	r := page.Data[0]
	require.NotZero(t, r.ID)
	require.NotEmpty(t, r.Customer.Name)
	require.Equal(t, "63733", r.AccountNumber)
	require.Equal(t, dto.RepeatsResponse{
		Unit: "week", Interval: 2, Weekday: intPtr(2), Occurrences: intPtr(6), Label: "Every 2 weeks on Tuesday, 6 times",
	}, r.Repeats)
	require.Equal(t, "send_by_email", r.Action)
	require.Equal(t, "failed", r.Status)
	require.Equal(t, "48.50", r.TotalValue)
	require.Equal(t, "2026-10-05", r.StartedOn)
	require.Nil(t, r.EndsOn)
	require.NotNil(t, r.NextInvoice.InvoiceID)
	require.Equal(t, strPtr("85032"), r.NextInvoice.InvoiceNumber)
	require.Equal(t, strPtr("2026-10-19"), r.NextInvoice.Date)
}

func TestOptionalPartsAreJSONNullAndNextInvoiceIsAlwaysAnObject(t *testing.T) {
	srv := newServer(t)

	var list map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"?status=paused").body, &list))
	r := list["data"].([]any)[0].(map[string]any)

	require.Nil(t, r["ends_on"])
	repeats := r["repeats"].(map[string]any)
	require.Contains(t, repeats, "weekday")
	require.Nil(t, repeats["weekday"])
	require.Nil(t, repeats["occurrences"])
	require.Equal(t, "Every month", repeats["label"])
	next, ok := r["next_invoice"].(map[string]any)
	require.True(t, ok, "next_invoice is an object even when nothing is known")
	require.Equal(t, []string{"date", "invoice_id", "invoice_number"}, keysOf(next))
	for _, v := range next {
		require.Nil(t, v)
	}
}

func TestADueDateWithoutAnInvoiceAndAnEndedOne(t *testing.T) {
	srv := newServer(t)

	due := get(t, srv, listPath+"?status=active&action=create_only").list(t)
	require.Len(t, due.Data, 1)
	require.Nil(t, due.Data[0].NextInvoice.InvoiceID)
	require.Nil(t, due.Data[0].NextInvoice.InvoiceNumber)
	require.Equal(t, strPtr("2026-10-18"), due.Data[0].NextInvoice.Date)
	require.Equal(t, "Every 3 days, 10 times", due.Data[0].Repeats.Label)

	ended := get(t, srv, listPath+"?status=ended").list(t)
	require.Len(t, ended.Data, 1)
	require.Equal(t, strPtr("2026-10-01"), ended.Data[0].EndsOn)
	require.Equal(t, "Every year", ended.Data[0].Repeats.Label)
}

func TestEverySeededRepeatRuleHasItsLabel(t *testing.T) {
	srv := newServer(t)

	page := get(t, srv, listPath+"?sort=repeats&order=asc").list(t)

	require.Equal(t, []string{
		"Every 3 days, 10 times",
		"Every week on Thursday, 3 times",
		"Every week on Thursday",
		"Every 2 weeks on Tuesday, 6 times",
		"Every month",
		"Every 2 months",
		"Every year",
	}, labelsOf(page), "ordered from the shortest repeat to the longest")
}

func TestPagingOverHTTPFollowsNextCursorForEverySortAndOrder(t *testing.T) {
	srv := newServer(t)

	for _, key := range model.RecurringSortKeys {
		for _, order := range []string{"asc", "desc"} {
			for _, limit := range []int{1, 3} {
				t.Run(fmt.Sprintf("%s/%s/limit=%d", key, order, limit), func(t *testing.T) {
					base := fmt.Sprintf("%s?sort=%s&order=%s", listPath, key, order)

					all := get(t, srv, base+"&limit=200").list(t)
					require.Len(t, all.Data, allRecurring)

					var got []int64
					target := fmt.Sprintf("%s&limit=%d", base, limit)
					for pages := 1; ; pages++ {
						require.LessOrEqual(t, pages, 20, "paging must terminate")

						page := get(t, srv, target).list(t)
						got = append(got, idsOf(page)...)

						if pages == 1 {
							require.NotNil(t, page.Summary)
							require.EqualValues(t, allRecurring, page.Summary.Count)
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

					require.Equal(t, idsOf(all), got, "every recurring invoice exactly once, in order")
				})
			}
		}
	}
}

func TestMultiValueParametersAreEquivalentInEveryForm(t *testing.T) {
	srv := newServer(t)

	for _, param := range []struct {
		name  string
		a, b  string
		wantN int
	}{
		{"status", "paused", "failed", 2},
		{"action", "create_only", "send_by_email", 7},
	} {
		t.Run(param.name, func(t *testing.T) {
			forms := []string{
				"?" + param.name + "=" + param.a + "," + param.b,
				"?" + param.name + "=" + param.a + "&" + param.name + "=" + param.b,
				"?" + param.name + "=" + param.b + "&" + param.name + "=" + param.a,
				"?" + param.name + "=%20" + param.a + "%20,%20" + param.b + "%20",
				"?" + param.name + "=" + strings.ToUpper(param.a) + "," + strings.ToUpper(param.b),
			}
			var reference []int64
			for i, form := range forms {
				page := get(t, srv, listPath+form+"&sort=started_on&order=asc").list(t)
				require.Len(t, page.Data, param.wantN, form)
				require.EqualValues(t, param.wantN, page.Summary.Count, form)
				if i == 0 {
					reference = idsOf(page)
					continue
				}
				require.Equal(t, reference, idsOf(page), form)
			}
		})
	}
}

func TestFilters(t *testing.T) {
	srv := newServer(t)

	tests := []struct {
		name      string
		query     string
		wantCount int
		wantTotal string
	}{
		{"active", "?status=active", 4, "244.99"},
		{"paused", "?status=paused", 1, "120.00"},
		{"failed", "?status=failed", 1, "48.50"},
		{"ended", "?status=ended", 1, "300.00"},
		{"create only", "?action=create_only", 2, "135.00"},
		{"send by email", "?action=send_by_email", 5, "578.49"},
		{"active and send by email", "?status=active&action=send_by_email", 3, "229.99"},
		{"one start date", "?from=2026-09-14&to=2026-09-14", 2, "130.00"},
		{"the last day is included", "?from=2026-10-15&to=2026-10-15", 1, "15.00"},
		{"open start", "?to=2026-08-31", 3, "519.99"},
		{"open end", "?from=2026-09-14", 4, "193.50"},
		{"empty range", "?from=2030-01-01&to=2030-01-31", 0, "0.00"},
		{"search by next invoice number", "?q=85032", 1, "48.50"},
		{"search by account number", "?q=63733", 2, "148.49"},
		{"search with a plus-encoded space", "?q=85032+", 1, "48.50"},
		{"percent is literal", "?q=%25", 0, "0.00"},
		{"underscore is literal", "?q=850_2", 0, "0.00"},
		{"at sign matches nothing", "?q=%40", 0, "0.00"},
		{"accented text matches nothing", "?q=%C3%A9", 0, "0.00"},
		{"injection text matches nothing", "?q=%27%3B+DROP+TABLE+recurring_invoices%3B--", 0, "0.00"},
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

func TestTheRangeEchoesTheDatesThatWereSent(t *testing.T) {
	srv := newServer(t)

	both := get(t, srv, listPath+"?from=2026-09-01&to=2026-10-31").list(t)
	require.Equal(t, strPtr("2026-09-01"), both.Range.From)
	require.Equal(t, strPtr("2026-10-31"), both.Range.To)

	onlyFrom := get(t, srv, listPath+"?from=2026-09-01").list(t)
	require.Equal(t, strPtr("2026-09-01"), onlyFrom.Range.From)
	require.Nil(t, onlyFrom.Range.To)
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
		{"the date column is called started_on", "?sort=date", 422, "invalid_input", []string{"sort"}},
		{"another report's sort", "?sort=credit_date", 422, "invalid_input", []string{"sort"}},
		{"unknown order", "?order=sideways", 422, "invalid_input", []string{"order"}},
		{"unknown status", "?status=overdue", 422, "invalid_input", []string{"status"}},
		{"a payment status is not a status", "?status=paid", 422, "invalid_input", []string{"status"}},
		{"unknown action", "?action=print", 422, "invalid_input", []string{"action"}},
		{"a payment method is not an action", "?action=card", 422, "invalid_input", []string{"action"}},
		{"NUL in search", "?q=a%00b", 422, "invalid_input", []string{"q"}},
		{"invalid UTF-8 in search", "?q=%FF%FE", 422, "invalid_input", []string{"q"}},
		{"search too long", "?q=" + strings.Repeat("a", 101), 422, "invalid_input", []string{"q"}},
		{"garbage cursor", "?cursor=garbage", 422, "invalid_input", []string{"cursor"}},
		{"unknown parameter", "?statuss=active", 422, "invalid_input", []string{"statuss"}},
		{"no tabs: lifecycle", "?lifecycle=active", 422, "invalid_input", []string{"lifecycle"}},
		{"payment parameter method", "?method=card", 422, "invalid_input", []string{"method"}},
		{"estimate parameter signature_status", "?signature_status=signed", 422, "invalid_input", []string{"signature_status"}},
		{"invoice parameter service_id", "?service_id=1", 422, "invalid_input", []string{"service_id"}},
		{"document parameter document_type_id", "?document_type_id=1", 422, "invalid_input", []string{"document_type_id"}},
		{"duplicated parameter", "?limit=1&limit=2", 422, "invalid_input", []string{"limit"}},
		{"several field problems are reported together", "?limit=0&sort=bogus&status=x&action=y", 422, "invalid_input", []string{"limit", "sort", "status", "action"}},
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

func TestErrorMessagesNameTheRecurringChoices(t *testing.T) {
	srv := newServer(t)

	body := get(t, srv, listPath+"?status=paid&action=card&sort=credit_date").errorBody(t)

	messages := map[string]string{}
	for _, d := range body.Details {
		messages[d.Field] = d.Message
	}
	require.Equal(t, "must be one of: active, paused, failed, ended", messages["status"])
	require.Equal(t, "must be one of: send_by_email, create_only", messages["action"])
	require.Contains(t, messages["sort"], "started_on")
	require.NotContains(t, messages["sort"], "credit_date")
}

func TestCursorFromAnotherSortIsRejected(t *testing.T) {
	srv := newServer(t)

	page := get(t, srv, listPath+"?limit=2").list(t)
	require.NotNil(t, page.Page.NextCursor)
	cursor := url.QueryEscape(*page.Page.NextCursor)

	for _, other := range []string{"&sort=ends_on&order=desc", "&order=asc"} {
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

	row := list["data"].([]any)[0].(map[string]any)
	require.Equal(t, []string{
		"account_number", "action", "customer", "ends_on", "id", "next_invoice", "repeats", "started_on", "status", "total_value",
	}, keysOf(row))
	require.Equal(t, []string{"id", "name"}, keysOf(row["customer"].(map[string]any)))
	require.Equal(t, []string{"interval", "label", "occurrences", "unit", "weekday"}, keysOf(row["repeats"].(map[string]any)))
	require.Equal(t, []string{"date", "invoice_id", "invoice_number"}, keysOf(row["next_invoice"].(map[string]any)))

	// money is a string, ids and counts are numbers.
	require.IsType(t, "", row["total_value"])
	require.IsType(t, "", list["summary"].(map[string]any)["total"])
	require.IsType(t, float64(0), row["id"])
	require.IsType(t, float64(0), list["summary"].(map[string]any)["count"])

	var filters map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"/filters").body, &filters))
	require.Equal(t, []string{"actions", "default_sort", "limits", "sort_keys", "statuses"}, keysOf(filters))

	var errBody map[string]any
	require.NoError(t, json.Unmarshal(get(t, srv, listPath+"?limit=0").body, &errBody))
	require.Equal(t, []string{"error"}, keysOf(errBody))
	require.Equal(t, []string{"code", "details", "message", "request_id"}, keysOf(errBody["error"].(map[string]any)))
}

func TestAnEmptyResultIsAnEmptyArrayOnTheWire(t *testing.T) {
	srv := newServer(t)

	resp := get(t, srv, listPath+"?from=2030-01-01&to=2030-01-31")

	require.Equal(t, http.StatusOK, resp.status)
	require.Contains(t, string(resp.body), `"data":[]`, "never null, so clients can iterate without a check")
	require.Contains(t, string(resp.body), `"summary":{"count":0,"total":"0.00"}`)
}

func TestFiltersEndpointMatchesWhatTheListAccepts(t *testing.T) {
	srv := newServer(t)

	var f dto.RecurringFiltersResponse
	resp := get(t, srv, listPath+"/filters")
	require.Equal(t, http.StatusOK, resp.status)
	require.Equal(t, "no-store", resp.header.Get("Cache-Control"))
	require.NoError(t, json.Unmarshal(resp.body, &f))

	require.Equal(t, []string{"active", "paused", "failed", "ended"}, f.Statuses)
	require.Equal(t, []string{"send_by_email", "create_only"}, f.Actions)
	require.Len(t, f.SortKeys, 9)
	require.Equal(t, dto.SortResponse{Key: "started_on", Order: "desc"}, f.DefaultSort)
	require.Equal(t, dto.PageLimitsResponse{Default: 50, Max: 200}, f.Limits)

	// Everything the endpoint advertises is accepted by the list endpoint.
	for _, key := range f.SortKeys {
		require.Equal(t, http.StatusOK, get(t, srv, listPath+"?sort="+key).status, key)
	}
	for _, s := range f.Statuses {
		require.Equal(t, http.StatusOK, get(t, srv, listPath+"?status="+s).status, s)
	}
	for _, a := range f.Actions {
		require.Equal(t, http.StatusOK, get(t, srv, listPath+"?action="+a).status, a)
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
			key := string(model.RecurringSortKeys[i%len(model.RecurringSortKeys)])
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
		var page dto.RecurringListResponse
		require.NoError(t, json.Unmarshal(r.body, &page))
		require.Len(t, page.Data, allRecurring, "worker %d (sort %s)", i, r.sortKey)
		require.EqualValues(t, allRecurring, page.Summary.Count)

		ids := idsOf(page)
		sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
		if reference == nil {
			reference = ids
		}
		require.Equal(t, reference, ids, "every sort returns the same set of recurring invoices")
	}
}
