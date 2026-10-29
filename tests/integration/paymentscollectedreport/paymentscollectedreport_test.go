//go:build integration

package paymentscollectedreport_test

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

const listPath = "/api/v1/reports/payments-collected"

var october21 = time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)

// newServer starts the real handler stack (middleware, router, handler, service, repository) behind a real HTTP
// listener, on a seeded database.
func newServer(t *testing.T) *httptest.Server {
	t.Helper()

	pool := testdb.SeededPool(t)
	logger := slog.New(slog.DiscardHandler)
	svc := service.NewPaymentsCollectedService(repository.NewPaymentsCollectedRepository(pool), func() time.Time { return october21 })
	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, nil),
		Reports:    []handler.RouteRegistrar{handler.NewPaymentsCollectedHandler(svc, logger)},
	})
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, srv *httptest.Server, target string) (*http.Response, []byte) {
	t.Helper()
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(srv.URL + target)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, body
}

func list(t *testing.T, srv *httptest.Server, query string) dto.PaymentCollectedListResponse {
	t.Helper()
	resp, body := get(t, srv, listPath+query)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	var out dto.PaymentCollectedListResponse
	require.NoError(t, json.Unmarshal(body, &out))
	return out
}

func ptr(s string) *string { return &s }

func TestListReturnsTheFirstPageWithItsSummary(t *testing.T) {
	srv := newServer(t)

	resp, body := get(t, srv, listPath+"?limit=2")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	require.NotEmpty(t, resp.Header.Get("X-Request-ID"))

	var out dto.PaymentCollectedListResponse
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.Data, 2)
	require.True(t, out.Page.HasMore)
	require.NotNil(t, out.Page.NextCursor)
	require.Equal(t, &dto.PaymentCollectedSummaryResponse{Count: 10, Amount: "811.06", WithoutTax: "693.28"}, out.Summary)
	require.Equal(t, dto.DateRangeResponse{From: ptr("2026-10-01"), To: ptr("2026-10-31")}, out.Range)

	first := out.Data[0]
	require.Equal(t, "invoice", first.PaymentFor.Type)
	require.Equal(t, "85032", first.PaymentFor.Label)
	require.NotNil(t, first.PaymentFor.InvoiceID)
	require.Equal(t, "check", first.Method.Type)
	require.Equal(t, "FL", *first.ServiceState)
	require.NotNil(t, first.Staff)
	require.Equal(t, "2026-10-21", first.Date)
	require.Equal(t, "30.00", first.Amount)
	require.Equal(t, "27.12", first.WithoutTax)
}

func TestListShowsCardPaymentsWithoutStaff(t *testing.T) {
	srv := newServer(t)

	out := list(t, srv, "?method=card&q=85004")

	require.Len(t, out.Data, 1)
	require.Nil(t, out.Data[0].Staff)
	require.Equal(t, "card", out.Data[0].Method.Type)
	require.Equal(t, "3222", *out.Data[0].Method.CardLast4)
	require.Equal(t, "108.63", out.Data[0].WithoutTax)
}

func TestListCreditsView(t *testing.T) {
	srv := newServer(t)

	out := list(t, srv, "?payment_for=credits&sort=amount&order=desc")

	require.Len(t, out.Data, 2)
	require.Equal(t, "credit", out.Data[0].PaymentFor.Type)
	require.Nil(t, out.Data[0].PaymentFor.InvoiceID)
	require.Equal(t, "Credit", out.Data[0].PaymentFor.Label)
	require.Equal(t, "200.00", out.Data[0].Amount)
	require.Equal(t, "200.00", out.Data[0].WithoutTax)
	require.Nil(t, out.Data[0].ServiceState)
	require.Equal(t, &dto.PaymentCollectedSummaryResponse{Count: 2, Amount: "300.00", WithoutTax: "300.00"}, out.Summary)
}

func TestListFollowsTheCursorToTheEndWithoutRepeats(t *testing.T) {
	srv := newServer(t)

	seen := map[int64]bool{}
	target := "?limit=3&sort=staff"
	var summaries int
	for pages := 0; pages < 10; pages++ {
		out := list(t, srv, target)
		if out.Summary != nil {
			summaries++
		}
		for _, row := range out.Data {
			require.False(t, seen[row.ID], "payment %d came twice", row.ID)
			seen[row.ID] = true
		}
		if !out.Page.HasMore {
			require.Nil(t, out.Page.NextCursor)
			break
		}
		target = "?limit=3&sort=staff&cursor=" + url.QueryEscape(*out.Page.NextCursor)
	}

	require.Len(t, seen, 10)
	require.Equal(t, 1, summaries)
}

func TestListFilters(t *testing.T) {
	srv := newServer(t)

	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"card", "?method=card", 4},
		{"cash or check, comma separated", "?method=cash,check", 6},
		{"search by invoice number", "?q=85004", 1},
		{"search by customer", "?q=saliba", 4},
		{"a day", "?from=2026-10-15&to=2026-10-15", 4},
		{"august has none", "?from=2026-08-01&to=2026-08-31", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := list(t, srv, tt.query)
			require.Len(t, out.Data, tt.want)
			require.NotNil(t, out.Data, "data is always an array")
		})
	}
}

func TestListRejectsInvalidRequestsWithFieldDetails(t *testing.T) {
	srv := newServer(t)

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantField  string
	}{
		{"unknown method", "?method=wire", http.StatusUnprocessableEntity, "method"},
		{"unknown payment_for", "?payment_for=all", http.StatusUnprocessableEntity, "payment_for"},
		{"unsupported parameter", "?status=paid", http.StatusUnprocessableEntity, "status"},
		{"limit out of range", "?limit=500", http.StatusUnprocessableEntity, "limit"},
		{"bad date", "?from=yesterday", http.StatusUnprocessableEntity, "from"},
		{"bad sort", "?sort=status", http.StatusUnprocessableEntity, "sort"},
		{"bad staff id", "?staff_id=abc", http.StatusUnprocessableEntity, "staff_id"},
		{"garbage cursor", "?cursor=zzz", http.StatusUnprocessableEntity, "cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := get(t, srv, listPath+tt.query)

			require.Equal(t, tt.wantStatus, resp.StatusCode)
			require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
			var e dto.ErrorResponse
			require.NoError(t, json.Unmarshal(body, &e))
			require.Equal(t, "invalid_input", e.Error.Code)
			require.Equal(t, tt.wantField, e.Error.Details[0].Field)
		})
	}

	t.Run("a malformed query string is a bad request", func(t *testing.T) {
		resp, _ := get(t, srv, listPath+"?q=%zz")
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

func TestFiltersEndpointListsTheStaffFromTheDatabase(t *testing.T) {
	srv := newServer(t)

	resp, body := get(t, srv, listPath+"/filters")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	var out dto.PaymentCollectedFiltersResponse
	require.NoError(t, json.Unmarshal(body, &out))
	require.Equal(t, []string{"invoices", "credits"}, out.PaymentFor)
	require.Equal(t, []string{"cash", "check", "card", "other"}, out.Methods)
	require.Equal(t, []string{"payment_for", "customer", "method", "service_state", "staff", "date", "amount", "without_tax"}, out.SortKeys)
	require.Equal(t, dto.SortResponse{Key: "date", Order: "desc"}, out.DefaultSort)
	require.Equal(t, dto.PageLimitsResponse{Default: 50, Max: 200}, out.Limits)

	names := make([]string, len(out.Staff))
	for i, s := range out.Staff {
		require.Positive(t, s.ID)
		names[i] = s.Name
	}
	require.ElementsMatch(t, []string{"Thọ Nguyễn Quang", "thon nguyen"}, names)
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
