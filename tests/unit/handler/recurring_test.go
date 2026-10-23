package handler_test

import (
	"context"
	"encoding/json"
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

const recurringPath = "/api/v1/reports/recurring-invoices"

func recurringEngine(svc handler.RecurringService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewRecurringHandler(svc, logger)},
	})
}

func emptyRecurringResult() *service.ListRecurringResult {
	return &service.ListRecurringResult{Items: []model.RecurringRow{}, Limit: 50}
}

// recurringParamsFor performs GET /recurring-invoices?query against a mocked service and returns the params it saw.
func recurringParamsFor(t *testing.T, query string) service.ListRecurringParams {
	t.Helper()

	svc := handlermocks.NewMockRecurringService(t)
	var got service.ListRecurringParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListRecurringParams) { got = p }).
		Return(emptyRecurringResult(), nil).Once()

	rec := do(recurringEngine(svc), http.MethodGet, recurringPath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

func recurringNeverCalled(t *testing.T) *handlermocks.MockRecurringService {
	return handlermocks.NewMockRecurringService(t)
}

func recIntPtr(v int) *int { return &v }

func recDatePtr(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

func TestRecurringListResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockRecurringService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListRecurringResult{
		Items: []model.RecurringRow{
			{
				// everything set
				ID: 3, CustomerID: 2, CustomerName: "Customer 1", AccountNumber: "63733",
				Unit: model.FrequencyWeek, Interval: 2, Weekday: recIntPtr(2), Occurrences: recIntPtr(6),
				Action: model.ActionSendByEmail, Status: model.RecurringFailed, TotalValue: 4850,
				StartedOn: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), EndsOn: recDatePtr(2026, 12, 31),
				NextInvoiceID: int64Ptr(41), NextInvoiceNumber: strPtr("85032"), NextInvoiceDate: recDatePtr(2026, 10, 19),
			},
			{
				// nothing optional set: the next_invoice object is still there, with null parts
				ID: 2, CustomerID: 1, CustomerName: "#F Last Name", AccountNumber: "63714",
				Unit: model.FrequencyMonth, Interval: 1,
				Action: model.ActionCreateOnly, Status: model.RecurringPaused, TotalValue: 12000,
				StartedOn: time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC),
			},
			{
				// a due date but no invoice created yet
				ID: 5, CustomerID: 6, CustomerName: "AAA1 ccc1", AccountNumber: "63706",
				Unit: model.FrequencyDay, Interval: 3, Occurrences: recIntPtr(10),
				Action: model.ActionCreateOnly, Status: model.RecurringActive, TotalValue: 1500050,
				StartedOn:       time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC),
				NextInvoiceDate: recDatePtr(2026, 10, 18),
			},
		},
		Limit:      50,
		HasMore:    true,
		NextCursor: "opaque-cursor",
		Summary:    &model.RecurringSummary{Count: 7, Total: 71349},
	}, nil).Once()

	rec := do(recurringEngine(svc), http.MethodGet, recurringPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{
				"id": 3, "customer": {"id": 2, "name": "Customer 1"}, "account_number": "63733",
				"repeats": {"unit": "week", "interval": 2, "weekday": 2, "occurrences": 6, "label": "Every 2 weeks on Tuesday, 6 times"},
				"action": "send_by_email", "status": "failed", "total_value": "48.50",
				"started_on": "2026-10-05", "ends_on": "2026-12-31",
				"next_invoice": {"invoice_id": 41, "invoice_number": "85032", "date": "2026-10-19"}
			},
			{
				"id": 2, "customer": {"id": 1, "name": "#F Last Name"}, "account_number": "63714",
				"repeats": {"unit": "month", "interval": 1, "weekday": null, "occurrences": null, "label": "Every month"},
				"action": "create_only", "status": "paused", "total_value": "120.00",
				"started_on": "2026-08-03", "ends_on": null,
				"next_invoice": {"invoice_id": null, "invoice_number": null, "date": null}
			},
			{
				"id": 5, "customer": {"id": 6, "name": "AAA1 ccc1"}, "account_number": "63706",
				"repeats": {"unit": "day", "interval": 3, "weekday": null, "occurrences": 10, "label": "Every 3 days, 10 times"},
				"action": "create_only", "status": "active", "total_value": "15000.50",
				"started_on": "2026-10-15", "ends_on": null,
				"next_invoice": {"invoice_id": null, "invoice_number": null, "date": "2026-10-18"}
			}
		],
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"summary": {"count": 7, "total": "713.49"},
		"range": {"from": null, "to": null}
	}`, rec.Body.String())
}

func TestRecurringListResponseRangeAndEmptyLaterPage(t *testing.T) {
	svc := handlermocks.NewMockRecurringService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListRecurringResult{
		Items: []model.RecurringRow{}, Limit: 5,
		From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(recurringEngine(svc), http.MethodGet, recurringPath+"?cursor=x", "")

	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 5, "has_more": false, "next_cursor": null},
		"summary": null,
		"range": {"from": "2026-09-01", "to": "2026-10-31"}
	}`, rec.Body.String())
}

func TestRecurringRepeatsLabel(t *testing.T) {
	tests := []struct {
		name        string
		unit        model.FrequencyUnit
		interval    int
		weekday     *int
		occurrences *int
		want        string
	}{
		{"every day", model.FrequencyDay, 1, nil, nil, "Every day"},
		{"every week", model.FrequencyWeek, 1, nil, nil, "Every week"},
		{"every month", model.FrequencyMonth, 1, nil, nil, "Every month"},
		{"every year", model.FrequencyYear, 1, nil, nil, "Every year"},
		{"every 3 days", model.FrequencyDay, 3, nil, nil, "Every 3 days"},
		{"every 2 weeks", model.FrequencyWeek, 2, nil, nil, "Every 2 weeks"},
		{"every 2 months", model.FrequencyMonth, 2, nil, nil, "Every 2 months"},
		{"every 10 years", model.FrequencyYear, 10, nil, nil, "Every 10 years"},
		{"a large interval", model.FrequencyDay, 365, nil, nil, "Every 365 days"},

		{"weekly on Sunday", model.FrequencyWeek, 1, recIntPtr(0), nil, "Every week on Sunday"},
		{"weekly on Monday", model.FrequencyWeek, 1, recIntPtr(1), nil, "Every week on Monday"},
		{"weekly on Tuesday", model.FrequencyWeek, 1, recIntPtr(2), nil, "Every week on Tuesday"},
		{"weekly on Wednesday", model.FrequencyWeek, 1, recIntPtr(3), nil, "Every week on Wednesday"},
		{"weekly on Thursday", model.FrequencyWeek, 1, recIntPtr(4), nil, "Every week on Thursday"},
		{"weekly on Friday", model.FrequencyWeek, 1, recIntPtr(5), nil, "Every week on Friday"},
		{"weekly on Saturday", model.FrequencyWeek, 1, recIntPtr(6), nil, "Every week on Saturday"},
		{"every 2 weeks on a day", model.FrequencyWeek, 2, recIntPtr(2), nil, "Every 2 weeks on Tuesday"},

		{"a weekday on a daily rule is not mentioned", model.FrequencyDay, 1, recIntPtr(4), nil, "Every day"},
		{"a weekday on a monthly rule is not mentioned", model.FrequencyMonth, 2, recIntPtr(4), nil, "Every 2 months"},
		{"a weekday on a yearly rule is not mentioned", model.FrequencyYear, 1, recIntPtr(4), nil, "Every year"},
		{"an out-of-range weekday is not mentioned", model.FrequencyWeek, 1, recIntPtr(9), nil, "Every week"},
		{"a negative weekday is not mentioned", model.FrequencyWeek, 1, recIntPtr(-1), nil, "Every week"},

		{"once", model.FrequencyMonth, 1, nil, recIntPtr(1), "Every month, 1 time"},
		{"three times", model.FrequencyWeek, 1, nil, recIntPtr(3), "Every week, 3 times"},
		{"weekday and occurrences", model.FrequencyWeek, 1, recIntPtr(4), recIntPtr(3), "Every week on Thursday, 3 times"},
		{"everything", model.FrequencyWeek, 2, recIntPtr(2), recIntPtr(6), "Every 2 weeks on Tuesday, 6 times"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := handlermocks.NewMockRecurringService(t)
			svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListRecurringResult{
				Items: []model.RecurringRow{{
					ID: 1, Unit: tt.unit, Interval: tt.interval, Weekday: tt.weekday, Occurrences: tt.occurrences,
					StartedOn: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				}},
				Limit: 50,
			}, nil).Once()

			rec := do(recurringEngine(svc), http.MethodGet, recurringPath, "")

			var body dto.RecurringListResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Equal(t, tt.want, body.Data[0].Repeats.Label)
			require.Equal(t, tt.weekday, body.Data[0].Repeats.Weekday, "the structured weekday is always returned as given")
		})
	}
}

func TestRecurringListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListRecurringParams
	}{
		{"no query", "", service.ListRecurringParams{}},
		{"every single-value parameter", "?from=2026-09-01&to=2026-10-31&q=85032&sort=total_value&order=desc&limit=25&cursor=abc",
			service.ListRecurringParams{From: "2026-09-01", To: "2026-10-31", Search: "85032",
				Sort: "total_value", Order: "desc", Limit: "25", Cursor: "abc"}},
		{"repeated key", "?status=active&status=paused", service.ListRecurringParams{Status: []string{"active", "paused"}}},
		{"comma separated", "?status=active,paused", service.ListRecurringParams{Status: []string{"active", "paused"}}},
		{"repeated and comma separated mixed", "?action=create_only,send_by_email&action=create_only", service.ListRecurringParams{Action: []string{"create_only", "send_by_email", "create_only"}}},
		{"whitespace and blanks are dropped", "?status=%20active%20,,paused,&status=", service.ListRecurringParams{Status: []string{"active", "paused"}}},
		{"only blanks leaves the list empty", "?status=&action=,", service.ListRecurringParams{}},
		{"both multi-value parameters", "?status=ended&action=send_by_email",
			service.ListRecurringParams{Status: []string{"ended"}, Action: []string{"send_by_email"}}},
		{"empty single values stay empty strings", "?from=&limit=", service.ListRecurringParams{}},

		{"percent-encoded at sign", "?q=d%40cm.co", service.ListRecurringParams{Search: "d@cm.co"}},
		{"plus is a space", "?q=a+b", service.ListRecurringParams{Search: "a b"}},
		{"encoded percent sign", "?q=50%25", service.ListRecurringParams{Search: "50%"}},
		{"encoded accented letter", "?q=%C3%A9", service.ListRecurringParams{Search: "é"}},
		{"a comma inside q is not a separator", "?q=a%2Cb", service.ListRecurringParams{Search: "a,b"}},
		{"sql text is just text", "?q=%27%3B+DROP+TABLE+recurring_invoices%3B--", service.ListRecurringParams{Search: "'; DROP TABLE recurring_invoices;--"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, recurringParamsFor(t, tt.query))
		})
	}
}

func TestRecurringListRejectsUnsupportedAndDuplicatedParameters(t *testing.T) {
	unsupported := func(name string) []dto.ErrorDetail {
		return []dto.ErrorDetail{{Field: name, Message: "is not a supported parameter"}}
	}
	tests := []struct {
		name  string
		query string
		want  []dto.ErrorDetail
	}{
		{"typo in a parameter name", "?statuss=active", unsupported("statuss")},
		{"names are case-sensitive", "?Status=active", unsupported("Status")},
		{"cache buster", "?_=1699999999", unsupported("_")},
		{"recurring invoices have no tabs: lifecycle", "?lifecycle=active", unsupported("lifecycle")},
		{"payment parameter method", "?method=card", unsupported("method")},
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
			rec := do(recurringEngine(recurringNeverCalled(t)), http.MethodGet, recurringPath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, tt.want, body.Details)
		})
	}

	t.Run("a very long unknown name is truncated in the error", func(t *testing.T) {
		rec := do(recurringEngine(recurringNeverCalled(t)), http.MethodGet, recurringPath+"?"+strings.Repeat("x", 5000)+"=1", "")

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Equal(t, strings.Repeat("x", 64)+"...", decodeError(t, rec).Details[0].Field)
	})
}

func TestRecurringListRejectsMalformedQueryStrings(t *testing.T) {
	for _, query := range []string{"?q=%zz", "?q=%", "?q=a;b=c"} {
		t.Run(query, func(t *testing.T) {
			rec := do(recurringEngine(recurringNeverCalled(t)), http.MethodGet, recurringPath+query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestRecurringListMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation error", model.InvalidInput(model.FieldError{Field: "limit", Message: "must be a whole number between 1 and 200"}), 422, "invalid_input"},
		{"wrapped validation error", fmt.Errorf("list: %w", model.InvalidInput(model.FieldError{Field: "q", Message: "x"})), 422, "invalid_input"},
		{"storage error", errors.New("pq: password authentication failed"), 500, "internal_error"},
		{"deadline exceeded", fmt.Errorf("list recurring invoices: %w", context.DeadlineExceeded), 504, "timeout"},
		{"client went away", fmt.Errorf("list recurring invoices: %w", context.Canceled), 499, "client_closed_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := handlermocks.NewMockRecurringService(t)
			svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, tt.err).Once()

			rec := do(recurringEngine(svc), http.MethodGet, recurringPath, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
			require.NotContains(t, rec.Body.String(), "password", "internal error text must not leak")
		})
	}

	t.Run("validation details reach the client", func(t *testing.T) {
		svc := handlermocks.NewMockRecurringService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil,
			model.InvalidInput(model.FieldError{Field: "status", Message: "must be one of: active, paused, failed, ended"})).Once()

		rec := do(recurringEngine(svc), http.MethodGet, recurringPath+"?status=paid", "")

		require.Equal(t, []dto.ErrorDetail{{Field: "status", Message: "must be one of: active, paused, failed, ended"}}, decodeError(t, rec).Details)
	})
}

func TestRecurringListPassesTheRequestContextToTheService(t *testing.T) {
	type key struct{}
	svc := handlermocks.NewMockRecurringService(t)
	svc.EXPECT().List(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(key{}) == "marker" }), mock.Anything).
		Return(emptyRecurringResult(), nil).Once()

	req := httptest.NewRequest(http.MethodGet, recurringPath, nil).
		WithContext(context.WithValue(context.Background(), key{}, "marker"))
	rec := httptest.NewRecorder()
	recurringEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestRecurringResponsesAreNeverCached(t *testing.T) {
	setRequestID := func(c *gin.Context) { c.Header("X-Request-ID", "rid-7"); c.Next() }

	t.Run("success", func(t *testing.T) {
		svc := handlermocks.NewMockRecurringService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(emptyRecurringResult(), nil).Once()

		rec := do(recurringEngine(svc, setRequestID), http.MethodGet, recurringPath, "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		require.Equal(t, "rid-7", rec.Header().Get("X-Request-ID"))
	})

	t.Run("validation error", func(t *testing.T) {
		rec := do(recurringEngine(recurringNeverCalled(t), setRequestID), http.MethodGet, recurringPath+"?nope=1", "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "rid-7", decodeError(t, rec).RequestID)
	})

	t.Run("server error", func(t *testing.T) {
		svc := handlermocks.NewMockRecurringService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errors.New("boom")).Once()

		rec := do(recurringEngine(svc), http.MethodGet, recurringPath, "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})

	t.Run("filters", func(t *testing.T) {
		rec := do(recurringEngine(recurringNeverCalled(t)), http.MethodGet, recurringPath+"/filters", "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})
}

func TestRecurringFiltersResponseJSONContract(t *testing.T) {
	// the choices are fixed, so the service (a mock with no expectations) is never called
	rec := do(recurringEngine(recurringNeverCalled(t)), http.MethodGet, recurringPath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"statuses": ["active", "paused", "failed", "ended"],
		"actions": ["send_by_email", "create_only"],
		"sort_keys": ["customer", "account_number", "repeats", "action", "status", "total_value", "started_on", "ends_on", "next_invoice_date"],
		"default_sort": {"key": "started_on", "order": "desc"},
		"limits": {"default": 50, "max": 200}
	}`, rec.Body.String())
}

func TestRecurringRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantCode   string
	}{
		{"POST list", http.MethodPost, recurringPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"DELETE list", http.MethodDelete, recurringPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"POST filters", http.MethodPost, recurringPath + "/filters", http.StatusMethodNotAllowed, "method_not_allowed"},
		{"unknown subpath", http.MethodGet, recurringPath + "/nope", http.StatusNotFound, "not_found"},
		{"not under the reports prefix", http.MethodGet, "/recurring-invoices", http.StatusNotFound, "not_found"},
		{"invoice routes are not served by this handler", http.MethodGet, invoicesPath, http.StatusNotFound, "not_found"},
		{"credit routes are not served by this handler", http.MethodGet, creditsPath, http.StatusNotFound, "not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(recurringEngine(recurringNeverCalled(t)), tt.method, tt.target, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
		})
	}
}

func TestNewRecurringHandlerAcceptsANilLogger(t *testing.T) {
	require.NotNil(t, handler.NewRecurringHandler(recurringNeverCalled(t), nil))
}
