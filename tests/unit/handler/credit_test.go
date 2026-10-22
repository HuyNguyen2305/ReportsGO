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

const creditsPath = "/api/v1/reports/credits"

func creditEngine(svc handler.CreditService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewCreditHandler(svc, logger)},
	})
}

func emptyCreditResult() *service.ListCreditsResult {
	return &service.ListCreditsResult{Items: []model.CreditRow{}, Limit: 50}
}

// creditParamsFor performs GET /credits?query against a mocked service and returns the params it saw.
func creditParamsFor(t *testing.T, query string) service.ListCreditsParams {
	t.Helper()

	svc := handlermocks.NewMockCreditService(t)
	var got service.ListCreditsParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListCreditsParams) { got = p }).
		Return(emptyCreditResult(), nil).Once()

	rec := do(creditEngine(svc), http.MethodGet, creditsPath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

func creditNeverCalled(t *testing.T) *handlermocks.MockCreditService {
	return handlermocks.NewMockCreditService(t)
}

func TestCreditListResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockCreditService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListCreditsResult{
		Items: []model.CreditRow{
			{
				ID: 11, CustomerID: 6, CustomerName: "AAA1 ccc1", AccountNumber: "63706",
				Method: model.MethodCard, MethodDetail: "Stripe **** 3222 Credit", Amount: 20000,
				Date: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), CreatedBy: strPtr("Thọ Nguyễn Quang"),
			},
			{
				// no creator and no stored detail
				ID: 15, CustomerID: 9, CustomerName: "Customer 2", AccountNumber: "63717",
				Method: model.MethodCash, Amount: 1000050,
				Date: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC),
			},
		},
		Limit:      50,
		HasMore:    true,
		NextCursor: "opaque-cursor",
		Summary:    &model.CreditSummary{Count: 7, Total: 57525},
		From:       time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(creditEngine(svc), http.MethodGet, creditsPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{
				"id": 11, "customer": {"id": 6, "name": "AAA1 ccc1"}, "account_number": "63706",
				"method": {"type": "card", "detail": "Stripe **** 3222 Credit"},
				"amount": "200.00", "credit_date": "2026-10-15", "created_by": "Thọ Nguyễn Quang"
			},
			{
				"id": 15, "customer": {"id": 9, "name": "Customer 2"}, "account_number": "63717",
				"method": {"type": "cash", "detail": ""},
				"amount": "10000.50", "credit_date": "2026-10-14", "created_by": null
			}
		],
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"summary": {"count": 7, "total": "575.25"},
		"range": {"from": "2026-10-01", "to": null}
	}`, rec.Body.String())
}

func TestCreditListResponseForAnEmptyLaterPage(t *testing.T) {
	svc := handlermocks.NewMockCreditService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListCreditsResult{
		Items: []model.CreditRow{}, Limit: 5,
		From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(creditEngine(svc), http.MethodGet, creditsPath+"?cursor=x", "")

	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 5, "has_more": false, "next_cursor": null},
		"summary": null,
		"range": {"from": "2026-10-01", "to": "2026-10-31"}
	}`, rec.Body.String())
}

func TestCreditListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListCreditsParams
	}{
		{"no query", "", service.ListCreditsParams{}},
		{"every single-value parameter", "?from=2026-10-01&to=2026-10-31&q=stripe&sort=amount&order=desc&limit=25&cursor=abc",
			service.ListCreditsParams{From: "2026-10-01", To: "2026-10-31", Search: "stripe",
				Sort: "amount", Order: "desc", Limit: "25", Cursor: "abc"}},
		{"repeated key", "?method=cash&method=card", service.ListCreditsParams{Method: []string{"cash", "card"}}},
		{"comma separated", "?method=cash,card", service.ListCreditsParams{Method: []string{"cash", "card"}}},
		{"repeated and comma separated mixed", "?method=cash,card&method=check", service.ListCreditsParams{Method: []string{"cash", "card", "check"}}},
		{"whitespace and blanks are dropped", "?method=%20cash%20,,card,&method=", service.ListCreditsParams{Method: []string{"cash", "card"}}},
		{"only blanks leaves the list empty", "?method=,", service.ListCreditsParams{}},
		{"empty single values stay empty strings", "?from=&limit=", service.ListCreditsParams{}},

		{"percent-encoded at sign", "?q=d%40cm.co", service.ListCreditsParams{Search: "d@cm.co"}},
		{"plus is a space", "?q=a+b", service.ListCreditsParams{Search: "a b"}},
		{"encoded percent sign", "?q=50%25", service.ListCreditsParams{Search: "50%"}},
		{"encoded accented letter", "?q=%C3%A9", service.ListCreditsParams{Search: "é"}},
		{"encoded asterisks as in card details", "?q=%2A%2A%2A%2A+3222", service.ListCreditsParams{Search: "**** 3222"}},
		{"a comma inside q is not a separator", "?q=a%2Cb", service.ListCreditsParams{Search: "a,b"}},
		{"sql text is just text", "?q=%27%3B+DROP+TABLE+credits%3B--", service.ListCreditsParams{Search: "'; DROP TABLE credits;--"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, creditParamsFor(t, tt.query))
		})
	}
}

func TestCreditListRejectsUnsupportedAndDuplicatedParameters(t *testing.T) {
	unsupported := func(name string) []dto.ErrorDetail {
		return []dto.ErrorDetail{{Field: name, Message: "is not a supported parameter"}}
	}
	tests := []struct {
		name  string
		query string
		want  []dto.ErrorDetail
	}{
		{"typo in a parameter name", "?methodd=cash", unsupported("methodd")},
		{"names are case-sensitive", "?Method=cash", unsupported("Method")},
		{"cache buster", "?_=1699999999", unsupported("_")},
		{"credits have no tabs: lifecycle", "?lifecycle=active", unsupported("lifecycle")},
		{"credits have no status", "?status=paid", unsupported("status")},
		{"estimate parameter signature_status", "?signature_status=signed", unsupported("signature_status")},
		{"invoice parameter service_id", "?service_id=1", unsupported("service_id")},
		{"invoice parameter tag_id", "?tag_id=1", unsupported("tag_id")},
		{"invoice parameter payment_term_id", "?payment_term_id=1", unsupported("payment_term_id")},
		{"document parameter document_type_id", "?document_type_id=1", unsupported("document_type_id")},
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
			rec := do(creditEngine(creditNeverCalled(t)), http.MethodGet, creditsPath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, tt.want, body.Details)
		})
	}

	t.Run("a very long unknown name is truncated in the error", func(t *testing.T) {
		rec := do(creditEngine(creditNeverCalled(t)), http.MethodGet, creditsPath+"?"+strings.Repeat("x", 5000)+"=1", "")

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Equal(t, strings.Repeat("x", 64)+"...", decodeError(t, rec).Details[0].Field)
	})
}

func TestCreditListRejectsMalformedQueryStrings(t *testing.T) {
	for _, query := range []string{"?q=%zz", "?q=%", "?q=a;b=c"} {
		t.Run(query, func(t *testing.T) {
			rec := do(creditEngine(creditNeverCalled(t)), http.MethodGet, creditsPath+query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestCreditListMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation error", model.InvalidInput(model.FieldError{Field: "limit", Message: "must be a whole number between 1 and 200"}), 422, "invalid_input"},
		{"wrapped validation error", fmt.Errorf("list: %w", model.InvalidInput(model.FieldError{Field: "q", Message: "x"})), 422, "invalid_input"},
		{"storage error", errors.New("pq: password authentication failed"), 500, "internal_error"},
		{"deadline exceeded", fmt.Errorf("list credits: %w", context.DeadlineExceeded), 504, "timeout"},
		{"client went away", fmt.Errorf("list credits: %w", context.Canceled), 499, "client_closed_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := handlermocks.NewMockCreditService(t)
			svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, tt.err).Once()

			rec := do(creditEngine(svc), http.MethodGet, creditsPath, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
			require.NotContains(t, rec.Body.String(), "password", "internal error text must not leak")
		})
	}

	t.Run("validation details reach the client", func(t *testing.T) {
		svc := handlermocks.NewMockCreditService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil,
			model.InvalidInput(model.FieldError{Field: "method", Message: "must be one of: cash, check, card, other"})).Once()

		rec := do(creditEngine(svc), http.MethodGet, creditsPath+"?method=wire", "")

		require.Equal(t, []dto.ErrorDetail{{Field: "method", Message: "must be one of: cash, check, card, other"}}, decodeError(t, rec).Details)
	})
}

func TestCreditListPassesTheRequestContextToTheService(t *testing.T) {
	type key struct{}
	svc := handlermocks.NewMockCreditService(t)
	svc.EXPECT().List(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(key{}) == "marker" }), mock.Anything).
		Return(emptyCreditResult(), nil).Once()

	req := httptest.NewRequest(http.MethodGet, creditsPath, nil).
		WithContext(context.WithValue(context.Background(), key{}, "marker"))
	rec := httptest.NewRecorder()
	creditEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestCreditResponsesAreNeverCached(t *testing.T) {
	setRequestID := func(c *gin.Context) { c.Header("X-Request-ID", "rid-7"); c.Next() }

	t.Run("success", func(t *testing.T) {
		svc := handlermocks.NewMockCreditService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(emptyCreditResult(), nil).Once()

		rec := do(creditEngine(svc, setRequestID), http.MethodGet, creditsPath, "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		require.Equal(t, "rid-7", rec.Header().Get("X-Request-ID"))
	})

	t.Run("validation error", func(t *testing.T) {
		rec := do(creditEngine(creditNeverCalled(t), setRequestID), http.MethodGet, creditsPath+"?nope=1", "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "rid-7", decodeError(t, rec).RequestID)
	})

	t.Run("server error", func(t *testing.T) {
		svc := handlermocks.NewMockCreditService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errors.New("boom")).Once()

		rec := do(creditEngine(svc), http.MethodGet, creditsPath, "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})

	t.Run("filters", func(t *testing.T) {
		rec := do(creditEngine(creditNeverCalled(t)), http.MethodGet, creditsPath+"/filters", "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})
}

func TestCreditFiltersResponseJSONContract(t *testing.T) {
	// the choices are fixed, so the service (a mock with no expectations) is never called
	rec := do(creditEngine(creditNeverCalled(t)), http.MethodGet, creditsPath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"methods": ["cash", "check", "card", "other"],
		"sort_keys": ["customer", "account_number", "method", "amount", "credit_date", "created_by"],
		"default_sort": {"key": "credit_date", "order": "desc"},
		"limits": {"default": 50, "max": 200}
	}`, rec.Body.String())
}

func TestCreditRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantCode   string
	}{
		{"POST list", http.MethodPost, creditsPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"DELETE list", http.MethodDelete, creditsPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"POST filters", http.MethodPost, creditsPath + "/filters", http.StatusMethodNotAllowed, "method_not_allowed"},
		{"unknown subpath", http.MethodGet, creditsPath + "/nope", http.StatusNotFound, "not_found"},
		{"not under the reports prefix", http.MethodGet, "/credits", http.StatusNotFound, "not_found"},
		{"invoice routes are not served by this handler", http.MethodGet, invoicesPath, http.StatusNotFound, "not_found"},
		{"payment routes are not served by this handler", http.MethodGet, paymentsPath, http.StatusNotFound, "not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(creditEngine(creditNeverCalled(t)), tt.method, tt.target, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
		})
	}
}

func TestNewCreditHandlerAcceptsANilLogger(t *testing.T) {
	require.NotNil(t, handler.NewCreditHandler(creditNeverCalled(t), nil))
}
