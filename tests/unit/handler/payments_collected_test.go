package handler_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/dto"
	"reportsgo/internal/handler"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
	handlermocks "reportsgo/tests/mocks/handler"
)

const paymentsCollectedPath = "/api/v1/reports/payments-collected"

func paymentsCollectedEngine(svc handler.PaymentsCollectedService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewPaymentsCollectedHandler(svc, logger)},
	})
}

func emptyPaymentsCollectedResult() *service.ListPaymentsCollectedResult {
	return &service.ListPaymentsCollectedResult{Items: []model.PaymentCollectedRow{}, Limit: 50}
}

// paymentsCollectedParamsFor performs GET /payments-collected?query against a mocked service and returns the
// params it saw.
func paymentsCollectedParamsFor(t *testing.T, query string) service.ListPaymentsCollectedParams {
	t.Helper()

	svc := handlermocks.NewMockPaymentsCollectedService(t)
	var got service.ListPaymentsCollectedParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListPaymentsCollectedParams) { got = p }).
		Return(emptyPaymentsCollectedResult(), nil).Once()

	rec := do(paymentsCollectedEngine(svc), http.MethodGet, paymentsCollectedPath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

func paymentsCollectedNeverCalled(t *testing.T) *handlermocks.MockPaymentsCollectedService {
	return handlermocks.NewMockPaymentsCollectedService(t)
}

func TestPaymentsCollectedListResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockPaymentsCollectedService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListPaymentsCollectedResult{
		Items: []model.PaymentCollectedRow{
			{
				ID: 12, CustomerID: 7, CustomerName: "AAA1 ccc1",
				InvoiceID: int64Ptr(41), InvoiceNumber: strPtr("85009"),
				Method: model.MethodCash, ServiceState: strPtr("TX"),
				StaffID: int64Ptr(1), StaffName: strPtr("Thọ Nguyễn Quang"),
				Date: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), Amount: 14856, WithoutTax: 13376,
			},
			{
				ID: 13, CustomerID: 7, CustomerName: "AAA1 ccc1",
				InvoiceID: int64Ptr(42), InvoiceNumber: strPtr("85004"),
				Method: model.MethodCard, CardBrand: strPtr("Credit"), CardLast4: strPtr("3222"),
				Date: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), Amount: 13863, WithoutTax: 10863,
			},
			{
				ID: 14, CustomerID: 8, CustomerName: "Customer 4",
				Method: model.MethodCheck,
				Date:   time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), Amount: 10000, WithoutTax: 10000,
			},
		},
		Limit:      50,
		HasMore:    true,
		NextCursor: "opaque-cursor",
		Summary:    &model.PaymentCollectedSummary{Count: 27, Amount: 189670, WithoutTax: 169522},
		From:       time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(paymentsCollectedEngine(svc), http.MethodGet, paymentsCollectedPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{"id": 12, "payment_for": {"type": "invoice", "invoice_id": 41, "label": "85009"},
			 "customer": {"id": 7, "name": "AAA1 ccc1"},
			 "method": {"type": "cash", "card_brand": null, "card_last4": null},
			 "service_state": "TX", "staff": {"id": 1, "name": "Thọ Nguyễn Quang"},
			 "date": "2026-10-15", "amount": "148.56", "without_tax": "133.76"},
			{"id": 13, "payment_for": {"type": "invoice", "invoice_id": 42, "label": "85004"},
			 "customer": {"id": 7, "name": "AAA1 ccc1"},
			 "method": {"type": "card", "card_brand": "Credit", "card_last4": "3222"},
			 "service_state": null, "staff": null,
			 "date": "2026-10-15", "amount": "138.63", "without_tax": "108.63"},
			{"id": 14, "payment_for": {"type": "credit", "invoice_id": null, "label": "Credit"},
			 "customer": {"id": 8, "name": "Customer 4"},
			 "method": {"type": "check", "card_brand": null, "card_last4": null},
			 "service_state": null, "staff": null,
			 "date": "2026-10-14", "amount": "100.00", "without_tax": "100.00"}
		],
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"summary": {"count": 27, "amount": "1896.70", "without_tax": "1695.22"},
		"range": {"from": "2026-10-01", "to": null}
	}`, rec.Body.String())
}

func TestPaymentsCollectedListResponseForAnEmptyLaterPage(t *testing.T) {
	svc := handlermocks.NewMockPaymentsCollectedService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListPaymentsCollectedResult{
		Items: []model.PaymentCollectedRow{}, Limit: 5,
		From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(paymentsCollectedEngine(svc), http.MethodGet, paymentsCollectedPath+"?cursor=x", "")

	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 5, "has_more": false, "next_cursor": null},
		"summary": null,
		"range": {"from": "2026-10-01", "to": "2026-10-31"}
	}`, rec.Body.String())
}

func TestPaymentsCollectedListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListPaymentsCollectedParams
	}{
		{"no query", "", service.ListPaymentsCollectedParams{}},
		{"every single-value parameter", "?from=2026-10-01&to=2026-10-31&payment_for=credits&q=85004&sort=amount&order=desc&limit=25&cursor=abc",
			service.ListPaymentsCollectedParams{From: "2026-10-01", To: "2026-10-31", PaymentFor: "credits", Search: "85004",
				Sort: "amount", Order: "desc", Limit: "25", Cursor: "abc"}},
		{"repeated key", "?method=cash&method=check", service.ListPaymentsCollectedParams{Method: []string{"cash", "check"}}},
		{"comma separated", "?method=cash,check", service.ListPaymentsCollectedParams{Method: []string{"cash", "check"}}},
		{"ids repeated and comma separated", "?customer_id=1,2&customer_id=3&staff_id=4,5",
			service.ListPaymentsCollectedParams{CustomerIDs: []string{"1", "2", "3"}, StaffIDs: []string{"4", "5"}}},
		{"whitespace and blanks are dropped", "?method=%20cash%20,,card,&method=", service.ListPaymentsCollectedParams{Method: []string{"cash", "card"}}},
		{"only blanks leaves the lists empty", "?method=&customer_id=,&staff_id=", service.ListPaymentsCollectedParams{}},
		{"empty single values stay empty strings", "?from=&payment_for=&limit=", service.ListPaymentsCollectedParams{}},
		{"sql text is just text", "?q=%27%3B+DROP+TABLE+payments%3B--", service.ListPaymentsCollectedParams{Search: "'; DROP TABLE payments;--"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, paymentsCollectedParamsFor(t, tt.query))
		})
	}
}

func TestPaymentsCollectedListRejectsUnsupportedAndDuplicatedParameters(t *testing.T) {
	unsupported := func(name string) []dto.ErrorDetail {
		return []dto.ErrorDetail{{Field: name, Message: "is not a supported parameter"}}
	}
	tests := []struct {
		name  string
		query string
		want  []dto.ErrorDetail
	}{
		{"typo in a parameter name", "?methods=cash", unsupported("methods")},
		{"status is not offered", "?status=paid", unsupported("status")},
		{"lifecycle is not offered", "?lifecycle=deleted", unsupported("lifecycle")},
		{"invoice parameter service_id", "?service_id=1", unsupported("service_id")},
		{"cache buster", "?_=1699999999", unsupported("_")},
		{"payment_for twice", "?payment_for=invoices&payment_for=credits", []dto.ErrorDetail{{Field: "payment_for", Message: "must be given only once"}}},
		{"limit twice", "?limit=1&limit=2", []dto.ErrorDetail{{Field: "limit", Message: "must be given only once"}}},
		{"problems are listed in a stable order", "?zzz=1&aaa=2&limit=1&limit=2", []dto.ErrorDetail{
			{Field: "aaa", Message: "is not a supported parameter"},
			{Field: "limit", Message: "must be given only once"},
			{Field: "zzz", Message: "is not a supported parameter"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(paymentsCollectedEngine(paymentsCollectedNeverCalled(t)), http.MethodGet, paymentsCollectedPath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, tt.want, body.Details)
		})
	}

	t.Run("a very long unknown name is truncated in the error", func(t *testing.T) {
		rec := do(paymentsCollectedEngine(paymentsCollectedNeverCalled(t)), http.MethodGet, paymentsCollectedPath+"?"+strings.Repeat("x", 5000)+"=1", "")

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Equal(t, strings.Repeat("x", 64)+"...", decodeError(t, rec).Details[0].Field)
	})
}

func TestPaymentsCollectedListRejectsMalformedQueryStrings(t *testing.T) {
	for _, query := range []string{"?q=%zz", "?q=%", "?q=a;b=c"} {
		t.Run(query, func(t *testing.T) {
			rec := do(paymentsCollectedEngine(paymentsCollectedNeverCalled(t)), http.MethodGet, paymentsCollectedPath+query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestPaymentsCollectedListMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation error", model.InvalidInput(model.FieldError{Field: "limit", Message: "must be a whole number between 1 and 200"}), 422, "invalid_input"},
		{"wrapped validation error", fmt.Errorf("list: %w", model.InvalidInput(model.FieldError{Field: "q", Message: "x"})), 422, "invalid_input"},
		{"storage error", errors.New("pq: password authentication failed"), 500, "internal_error"},
		{"deadline exceeded", fmt.Errorf("list payments collected: %w", context.DeadlineExceeded), 504, "timeout"},
		{"client went away", fmt.Errorf("list payments collected: %w", context.Canceled), 499, "client_closed_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := handlermocks.NewMockPaymentsCollectedService(t)
			svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, tt.err).Once()

			rec := do(paymentsCollectedEngine(svc), http.MethodGet, paymentsCollectedPath, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
			require.NotContains(t, rec.Body.String(), "password", "internal error text must not leak")
		})
	}

	t.Run("validation details reach the client", func(t *testing.T) {
		svc := handlermocks.NewMockPaymentsCollectedService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil,
			model.InvalidInput(model.FieldError{Field: "payment_for", Message: "must be one of: invoices, credits"})).Once()

		rec := do(paymentsCollectedEngine(svc), http.MethodGet, paymentsCollectedPath+"?payment_for=all", "")

		require.Equal(t, []dto.ErrorDetail{{Field: "payment_for", Message: "must be one of: invoices, credits"}}, decodeError(t, rec).Details)
	})
}

func TestPaymentsCollectedListPassesTheRequestContextToTheService(t *testing.T) {
	type key struct{}
	svc := handlermocks.NewMockPaymentsCollectedService(t)
	svc.EXPECT().List(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(key{}) == "marker" }), mock.Anything).
		Return(emptyPaymentsCollectedResult(), nil).Once()

	req := httptest.NewRequest(http.MethodGet, paymentsCollectedPath, nil).
		WithContext(context.WithValue(context.Background(), key{}, "marker"))
	rec := httptest.NewRecorder()
	paymentsCollectedEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestPaymentsCollectedResponsesAreNeverCached(t *testing.T) {
	setRequestID := func(c *gin.Context) { c.Header("X-Request-ID", "rid-7"); c.Next() }

	t.Run("success", func(t *testing.T) {
		svc := handlermocks.NewMockPaymentsCollectedService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(emptyPaymentsCollectedResult(), nil).Once()

		rec := do(paymentsCollectedEngine(svc, setRequestID), http.MethodGet, paymentsCollectedPath, "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		require.Equal(t, "rid-7", rec.Header().Get("X-Request-ID"))
	})

	t.Run("validation error", func(t *testing.T) {
		rec := do(paymentsCollectedEngine(paymentsCollectedNeverCalled(t), setRequestID), http.MethodGet, paymentsCollectedPath+"?nope=1", "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "rid-7", decodeError(t, rec).RequestID)
	})

	t.Run("filters", func(t *testing.T) {
		svc := handlermocks.NewMockPaymentsCollectedService(t)
		svc.EXPECT().FilterOptions(mock.Anything).Return(&service.PaymentsCollectedFilterOptions{}, nil).Once()

		rec := do(paymentsCollectedEngine(svc), http.MethodGet, paymentsCollectedPath+"/filters", "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})
}

func TestPaymentsCollectedFiltersResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockPaymentsCollectedService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.PaymentsCollectedFilterOptions{
		Staff: []model.StaffOption{{ID: 2, Name: "thon nguyen"}, {ID: 1, Name: "Thọ Nguyễn Quang"}},
	}, nil).Once()

	rec := do(paymentsCollectedEngine(svc), http.MethodGet, paymentsCollectedPath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"payment_for": ["invoices", "credits"],
		"methods": ["cash", "check", "card", "other"],
		"staff": [{"id": 2, "name": "thon nguyen"}, {"id": 1, "name": "Thọ Nguyễn Quang"}],
		"sort_keys": ["payment_for", "customer", "method", "service_state", "staff", "date", "amount", "without_tax"],
		"default_sort": {"key": "date", "order": "desc"},
		"limits": {"default": 50, "max": 200}
	}`, rec.Body.String())
}

func TestPaymentsCollectedFiltersWithNoStaffIsAnEmptyList(t *testing.T) {
	svc := handlermocks.NewMockPaymentsCollectedService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.PaymentsCollectedFilterOptions{}, nil).Once()

	rec := do(paymentsCollectedEngine(svc), http.MethodGet, paymentsCollectedPath+"/filters", "")

	require.Contains(t, rec.Body.String(), `"staff":[]`)
}

func TestPaymentsCollectedFiltersMapsServiceErrors(t *testing.T) {
	svc := handlermocks.NewMockPaymentsCollectedService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(nil, errors.New("pq: down")).Once()

	rec := do(paymentsCollectedEngine(svc), http.MethodGet, paymentsCollectedPath+"/filters", "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "internal_error", decodeError(t, rec).Code)
}

func TestPaymentsCollectedRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantCode   string
	}{
		{"POST list", http.MethodPost, paymentsCollectedPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"POST filters", http.MethodPost, paymentsCollectedPath + "/filters", http.StatusMethodNotAllowed, "method_not_allowed"},
		{"unknown subpath", http.MethodGet, paymentsCollectedPath + "/nope", http.StatusNotFound, "not_found"},
		{"not under the reports prefix", http.MethodGet, "/payments-collected", http.StatusNotFound, "not_found"},
		{"the payments report is not served by this handler", http.MethodGet, paymentsPath, http.StatusNotFound, "not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(paymentsCollectedEngine(paymentsCollectedNeverCalled(t)), tt.method, tt.target, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
		})
	}
}

func TestNewPaymentsCollectedHandlerAcceptsANilLogger(t *testing.T) {
	require.NotNil(t, handler.NewPaymentsCollectedHandler(paymentsCollectedNeverCalled(t), nil))
}
