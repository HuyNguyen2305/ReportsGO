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

const paymentsPath = "/api/v1/reports/payments"

func paymentEngine(svc handler.PaymentService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewPaymentHandler(svc, logger)},
	})
}

func emptyPaymentResult() *service.ListPaymentsResult {
	return &service.ListPaymentsResult{Items: []model.PaymentRow{}, Limit: 50}
}

// paymentParamsFor performs GET /payments?query against a mocked service and returns the params it saw.
func paymentParamsFor(t *testing.T, query string) service.ListPaymentsParams {
	t.Helper()

	svc := handlermocks.NewMockPaymentService(t)
	var got service.ListPaymentsParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListPaymentsParams) { got = p }).
		Return(emptyPaymentResult(), nil).Once()

	rec := do(paymentEngine(svc), http.MethodGet, paymentsPath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

func paymentNeverCalled(t *testing.T) *handlermocks.MockPaymentService {
	return handlermocks.NewMockPaymentService(t)
}

func int64Ptr(v int64) *int64 { return &v }

func TestPaymentListResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockPaymentService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListPaymentsResult{
		Items: []model.PaymentRow{
			{
				ID: 12, CustomerID: 7, CustomerName: "Customer 3", AccountNumber: "63706",
				InvoiceID: int64Ptr(41), InvoiceNumber: strPtr("85004"),
				Method: model.MethodCard, CardBrand: strPtr("Visa"), CardLast4: strPtr("4242"),
				Amount: 2000, Date: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), Status: model.PaymentFailed,
			},
			{
				ID: 13, CustomerID: 8, CustomerName: "Customer 4", AccountNumber: "63707",
				Method: model.MethodCheck, Amount: 10000050,
				Date: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), Status: model.PaymentPaid,
			},
		},
		Limit:      50,
		HasMore:    true,
		NextCursor: "opaque-cursor",
		Summary:    &model.PaymentSummary{Count: 15, Total: 123450},
		From:       time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(paymentEngine(svc), http.MethodGet, paymentsPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{
				"id": 12, "customer": {"id": 7, "name": "Customer 3"}, "account_number": "63706",
				"payment_for": {"type": "invoice", "invoice_id": 41, "label": "85004"},
				"method": {"type": "card", "card_brand": "Visa", "card_last4": "4242"},
				"amount": "20.00", "payment_date": "2026-10-15", "status": "failed"
			},
			{
				"id": 13, "customer": {"id": 8, "name": "Customer 4"}, "account_number": "63707",
				"payment_for": {"type": "credit", "invoice_id": null, "label": "Credit"},
				"method": {"type": "check", "card_brand": null, "card_last4": null},
				"amount": "100000.50", "payment_date": "2026-10-14", "status": "paid"
			}
		],
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"summary": {"count": 15, "total": "1234.50"},
		"range": {"from": "2026-10-01", "to": null}
	}`, rec.Body.String())
}

func TestPaymentListResponseForAnEmptyLaterPage(t *testing.T) {
	svc := handlermocks.NewMockPaymentService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListPaymentsResult{
		Items: []model.PaymentRow{}, Limit: 5,
		From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(paymentEngine(svc), http.MethodGet, paymentsPath+"?cursor=x", "")

	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 5, "has_more": false, "next_cursor": null},
		"summary": null,
		"range": {"from": "2026-10-01", "to": "2026-10-31"}
	}`, rec.Body.String())
}

func TestPaymentListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListPaymentsParams
	}{
		{"no query", "", service.ListPaymentsParams{}},
		{"every single-value parameter", "?from=2026-10-01&to=2026-10-31&lifecycle=deleted&q=85004&sort=amount&order=desc&limit=25&cursor=abc",
			service.ListPaymentsParams{From: "2026-10-01", To: "2026-10-31", Lifecycle: "deleted", Search: "85004",
				Sort: "amount", Order: "desc", Limit: "25", Cursor: "abc"}},
		{"repeated key", "?status=paid&status=failed", service.ListPaymentsParams{Status: []string{"paid", "failed"}}},
		{"comma separated", "?status=paid,failed", service.ListPaymentsParams{Status: []string{"paid", "failed"}}},
		{"repeated and comma separated mixed", "?method=cash,card&method=check", service.ListPaymentsParams{Method: []string{"cash", "card", "check"}}},
		{"whitespace and blanks are dropped", "?status=%20paid%20,,failed,&status=", service.ListPaymentsParams{Status: []string{"paid", "failed"}}},
		{"only blanks leaves the list empty", "?status=&method=,", service.ListPaymentsParams{}},
		{"both multi-value parameters", "?status=pending&method=other",
			service.ListPaymentsParams{Status: []string{"pending"}, Method: []string{"other"}}},
		{"empty single values stay empty strings", "?from=&limit=", service.ListPaymentsParams{}},

		{"percent-encoded at sign", "?q=d%40cm.co", service.ListPaymentsParams{Search: "d@cm.co"}},
		{"plus is a space", "?q=a+b", service.ListPaymentsParams{Search: "a b"}},
		{"encoded percent sign", "?q=50%25", service.ListPaymentsParams{Search: "50%"}},
		{"encoded accented letter", "?q=%C3%A9", service.ListPaymentsParams{Search: "é"}},
		{"a comma inside q is not a separator", "?q=a%2Cb", service.ListPaymentsParams{Search: "a,b"}},
		{"sql text is just text", "?q=%27%3B+DROP+TABLE+payments%3B--", service.ListPaymentsParams{Search: "'; DROP TABLE payments;--"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, paymentParamsFor(t, tt.query))
		})
	}
}

func TestPaymentListRejectsUnsupportedAndDuplicatedParameters(t *testing.T) {
	unsupported := func(name string) []dto.ErrorDetail {
		return []dto.ErrorDetail{{Field: name, Message: "is not a supported parameter"}}
	}
	tests := []struct {
		name  string
		query string
		want  []dto.ErrorDetail
	}{
		{"typo in a parameter name", "?statuss=paid", unsupported("statuss")},
		{"names are case-sensitive", "?Status=paid", unsupported("Status")},
		{"cache buster", "?_=1699999999", unsupported("_")},
		{"estimate parameter signature_status", "?signature_status=signed", unsupported("signature_status")},
		{"invoice parameter service_id", "?service_id=1", unsupported("service_id")},
		{"invoice parameter tag_id", "?tag_id=1", unsupported("tag_id")},
		{"invoice parameter payment_term_id", "?payment_term_id=1", unsupported("payment_term_id")},
		{"document parameter document_type_id", "?document_type_id=1", unsupported("document_type_id")},
		{"method has a longer name", "?payment_method=card", unsupported("payment_method")},
		{"single-value parameter twice", "?limit=1&limit=2", []dto.ErrorDetail{{Field: "limit", Message: "must be given only once"}}},
		{"q twice", "?q=a&q=b", []dto.ErrorDetail{{Field: "q", Message: "must be given only once"}}},
		{"problems are listed in a stable order", "?zzz=1&aaa=2&limit=1&limit=2", []dto.ErrorDetail{
			{Field: "aaa", Message: "is not a supported parameter"},
			{Field: "limit", Message: "must be given only once"},
			{Field: "zzz", Message: "is not a supported parameter"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(paymentEngine(paymentNeverCalled(t)), http.MethodGet, paymentsPath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, tt.want, body.Details)
		})
	}

	t.Run("a very long unknown name is truncated in the error", func(t *testing.T) {
		rec := do(paymentEngine(paymentNeverCalled(t)), http.MethodGet, paymentsPath+"?"+strings.Repeat("x", 5000)+"=1", "")

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Equal(t, strings.Repeat("x", 64)+"...", decodeError(t, rec).Details[0].Field)
	})
}

func TestPaymentListRejectsMalformedQueryStrings(t *testing.T) {
	for _, query := range []string{"?q=%zz", "?q=%", "?q=a;b=c"} {
		t.Run(query, func(t *testing.T) {
			rec := do(paymentEngine(paymentNeverCalled(t)), http.MethodGet, paymentsPath+query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestPaymentListMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation error", model.InvalidInput(model.FieldError{Field: "limit", Message: "must be a whole number between 1 and 200"}), 422, "invalid_input"},
		{"wrapped validation error", fmt.Errorf("list: %w", model.InvalidInput(model.FieldError{Field: "q", Message: "x"})), 422, "invalid_input"},
		{"storage error", errors.New("pq: password authentication failed"), 500, "internal_error"},
		{"deadline exceeded", fmt.Errorf("list payments: %w", context.DeadlineExceeded), 504, "timeout"},
		{"client went away", fmt.Errorf("list payments: %w", context.Canceled), 499, "client_closed_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := handlermocks.NewMockPaymentService(t)
			svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, tt.err).Once()

			rec := do(paymentEngine(svc), http.MethodGet, paymentsPath, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
			require.NotContains(t, rec.Body.String(), "password", "internal error text must not leak")
		})
	}

	t.Run("validation details reach the client", func(t *testing.T) {
		svc := handlermocks.NewMockPaymentService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil,
			model.InvalidInput(model.FieldError{Field: "method", Message: "must be one of: cash, check, card, other"})).Once()

		rec := do(paymentEngine(svc), http.MethodGet, paymentsPath+"?method=wire", "")

		require.Equal(t, []dto.ErrorDetail{{Field: "method", Message: "must be one of: cash, check, card, other"}}, decodeError(t, rec).Details)
	})
}

func TestPaymentListPassesTheRequestContextToTheService(t *testing.T) {
	type key struct{}
	svc := handlermocks.NewMockPaymentService(t)
	svc.EXPECT().List(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(key{}) == "marker" }), mock.Anything).
		Return(emptyPaymentResult(), nil).Once()

	req := httptest.NewRequest(http.MethodGet, paymentsPath, nil).
		WithContext(context.WithValue(context.Background(), key{}, "marker"))
	rec := httptest.NewRecorder()
	paymentEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestPaymentResponsesAreNeverCached(t *testing.T) {
	setRequestID := func(c *gin.Context) { c.Header("X-Request-ID", "rid-7"); c.Next() }

	t.Run("success", func(t *testing.T) {
		svc := handlermocks.NewMockPaymentService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(emptyPaymentResult(), nil).Once()

		rec := do(paymentEngine(svc, setRequestID), http.MethodGet, paymentsPath, "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		require.Equal(t, "rid-7", rec.Header().Get("X-Request-ID"))
	})

	t.Run("validation error", func(t *testing.T) {
		rec := do(paymentEngine(paymentNeverCalled(t), setRequestID), http.MethodGet, paymentsPath+"?nope=1", "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "rid-7", decodeError(t, rec).RequestID)
	})

	t.Run("server error", func(t *testing.T) {
		svc := handlermocks.NewMockPaymentService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errors.New("boom")).Once()

		rec := do(paymentEngine(svc), http.MethodGet, paymentsPath, "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})

	t.Run("filters", func(t *testing.T) {
		rec := do(paymentEngine(paymentNeverCalled(t)), http.MethodGet, paymentsPath+"/filters", "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})
}

func TestPaymentFiltersResponseJSONContract(t *testing.T) {
	// the choices are fixed, so the service (a mock with no expectations) is never called
	rec := do(paymentEngine(paymentNeverCalled(t)), http.MethodGet, paymentsPath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"statuses": ["paid", "pending", "failed", "refunded"],
		"methods": ["cash", "check", "card", "other"],
		"lifecycles": ["active", "deleted"],
		"sort_keys": ["payment_for", "customer", "method", "payment_date", "amount", "status"],
		"default_sort": {"key": "payment_date", "order": "desc"},
		"limits": {"default": 50, "max": 200}
	}`, rec.Body.String())
}

func TestPaymentRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantCode   string
	}{
		{"POST list", http.MethodPost, paymentsPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"DELETE list", http.MethodDelete, paymentsPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"POST filters", http.MethodPost, paymentsPath + "/filters", http.StatusMethodNotAllowed, "method_not_allowed"},
		{"unknown subpath", http.MethodGet, paymentsPath + "/nope", http.StatusNotFound, "not_found"},
		{"not under the reports prefix", http.MethodGet, "/payments", http.StatusNotFound, "not_found"},
		{"invoice routes are not served by this handler", http.MethodGet, invoicesPath, http.StatusNotFound, "not_found"},
		{"document routes are not served by this handler", http.MethodGet, documentsPath, http.StatusNotFound, "not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(paymentEngine(paymentNeverCalled(t)), tt.method, tt.target, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
		})
	}
}

func TestNewPaymentHandlerAcceptsANilLogger(t *testing.T) {
	require.NotNil(t, handler.NewPaymentHandler(paymentNeverCalled(t), nil))
}
