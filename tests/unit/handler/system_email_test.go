package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

const systemEmailPath = "/api/v1/reports/system-email-history"

func systemEmailEngine(svc handler.SystemEmailService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewSystemEmailHandler(svc, logger)},
	})
}

// systemEmailParamsFor performs GET /system-email-history?query against a mocked service and returns the params it saw.
func systemEmailParamsFor(t *testing.T, query string) service.ListSystemEmailsParams {
	t.Helper()
	svc := handlermocks.NewMockSystemEmailService(t)
	var got service.ListSystemEmailsParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListSystemEmailsParams) { got = p }).
		Return(&service.ListSystemEmailsResult{Items: []model.SystemEmailRow{}, Limit: 50}, nil).Once()

	rec := do(systemEmailEngine(svc), http.MethodGet, systemEmailPath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

func TestSystemEmailListResponseJSONContract(t *testing.T) {
	plus7 := time.FixedZone("UTC+7", 7*3600)
	svc := handlermocks.NewMockSystemEmailService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListSystemEmailsResult{
		Items: []model.SystemEmailRow{
			{
				ID: 9, CustomerID: 2, CustomerName: "Customer", Email: "billing.alt@example.com",
				Type: model.EmailPaymentReceipt, Status: model.EmailConfirmed,
				SentAt: time.Date(2026, 11, 3, 12, 0, 0, 123456000, time.UTC),
			},
			{
				// a whole second from a non-UTC zone is still written in UTC with six fractional digits
				ID: 3, CustomerID: 7, CustomerName: "PerfTest", Email: "no-reply@example.com",
				Type: model.EmailInvoice, Status: model.EmailCancelled,
				SentAt: time.Date(2026, 11, 2, 15, 0, 0, 0, plus7),
			},
		},
		Limit: 50, HasMore: true, NextCursor: "opaque-cursor",
		From: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(systemEmailEngine(svc), http.MethodGet, systemEmailPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{"id": 9, "customer": {"id": 2, "name": "Customer"}, "email": "billing.alt@example.com",
			 "email_type": "payment_receipt", "status": "confirmed", "time_stamp": "2026-11-03T12:00:00.123456Z"},
			{"id": 3, "customer": {"id": 7, "name": "PerfTest"}, "email": "no-reply@example.com",
			 "email_type": "invoice", "status": "cancelled", "time_stamp": "2026-11-02T08:00:00.000000Z"}
		],
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"range": {"from": "2026-11-01", "to": null}
	}`, rec.Body.String())
}

func TestSystemEmailListResponseForAnEmptyPage(t *testing.T) {
	svc := handlermocks.NewMockSystemEmailService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListSystemEmailsResult{
		Items: []model.SystemEmailRow{}, Limit: 5,
		From: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(systemEmailEngine(svc), http.MethodGet, systemEmailPath, "")

	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 5, "has_more": false, "next_cursor": null},
		"range": {"from": "2026-11-01", "to": "2026-11-30"}
	}`, rec.Body.String())
}

func TestSystemEmailListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListSystemEmailsParams
	}{
		{"no query", "", service.ListSystemEmailsParams{}},
		{"every single-value parameter", "?from=2026-11-01&to=2026-11-30&name_format=full_name&q=saliba&sort=email&order=desc&limit=25&cursor=abc",
			service.ListSystemEmailsParams{From: "2026-11-01", To: "2026-11-30", NameFormat: "full_name", Search: "saliba", Sort: "email", Order: "desc", Limit: "25", Cursor: "abc"}},
		{"statuses in every form", "?status=confirmed&status=cancelled,unconfirmed", service.ListSystemEmailsParams{Status: []string{"confirmed", "cancelled", "unconfirmed"}}},
		{"clients in every form", "?client_id=1,2&client_id=3", service.ListSystemEmailsParams{CustomerIDs: []string{"1", "2", "3"}}},
		{"whitespace and blanks are dropped", "?status=%20confirmed%20,,&client_id=", service.ListSystemEmailsParams{Status: []string{"confirmed"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, systemEmailParamsFor(t, tt.query))
		})
	}
}

func TestSystemEmailListRejectsUnsupportedParameters(t *testing.T) {
	tests := []struct {
		name  string
		query string
		field string
		msg   string
	}{
		{"unknown name", "?colour=red", "colour", "is not a supported parameter"},
		{"customer_id is client_id here", "?customer_id=1", "customer_id", "is not a supported parameter"},
		{"type filter is not offered", "?email_type=invoice", "email_type", "is not a supported parameter"},
		{"single-value parameter twice", "?name_format=first_name&name_format=last_name", "name_format", "must be given only once"},
		{"limit twice", "?limit=1&limit=2", "limit", "must be given only once"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(systemEmailEngine(handlermocks.NewMockSystemEmailService(t)), http.MethodGet, systemEmailPath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, []dto.ErrorDetail{{Field: tt.field, Message: tt.msg}}, body.Details)
		})
	}
}

func TestSystemEmailListRejectsMalformedQueryStrings(t *testing.T) {
	for _, query := range []string{"?q=%zz", "?q=%"} {
		t.Run(query, func(t *testing.T) {
			rec := do(systemEmailEngine(handlermocks.NewMockSystemEmailService(t)), http.MethodGet, systemEmailPath+query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestSystemEmailListMapsServiceErrors(t *testing.T) {
	t.Run("validation errors are 422", func(t *testing.T) {
		svc := handlermocks.NewMockSystemEmailService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).
			Return(nil, model.InvalidInput(model.FieldError{Field: "status", Message: "must be one of: confirmed"})).Once()

		rec := do(systemEmailEngine(svc), http.MethodGet, systemEmailPath, "")

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Equal(t, "invalid_input", decodeError(t, rec).Code)
	})

	t.Run("storage errors are 500 and leak nothing", func(t *testing.T) {
		svc := handlermocks.NewMockSystemEmailService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errors.New("pq: password authentication failed")).Once()

		rec := do(systemEmailEngine(svc), http.MethodGet, systemEmailPath, "")

		require.Equal(t, http.StatusInternalServerError, rec.Code)
		require.Equal(t, "internal_error", decodeError(t, rec).Code)
		require.NotContains(t, rec.Body.String(), "password")
	})
}

func TestSystemEmailListPassesTheRequestContext(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "marker")
	svc := handlermocks.NewMockSystemEmailService(t)
	svc.EXPECT().List(mock.MatchedBy(func(c context.Context) bool { return c.Value(key{}) == "marker" }), mock.Anything).
		Return(&service.ListSystemEmailsResult{Items: []model.SystemEmailRow{}, Limit: 50}, nil).Once()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, systemEmailPath, nil)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	systemEmailEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestSystemEmailFiltersResponse(t *testing.T) {
	svc := handlermocks.NewMockSystemEmailService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.SystemEmailFilterOptions{
		Clients: []model.ClientOption{{ID: 7, Name: "William Saliba"}},
	}, nil).Once()

	rec := do(systemEmailEngine(svc), http.MethodGet, systemEmailPath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"statuses": ["unconfirmed", "confirmed", "cancelled"],
		"email_types": ["welcome", "invoice", "estimate", "appointment_confirmation", "appointment_reminder", "payment_receipt", "review_request"],
		"clients": [{"id": 7, "name": "William Saliba"}],
		"name_formats": ["first_name", "last_name", "full_name"],
		"sort_keys": ["time_stamp", "customer", "email", "email_type", "status"],
		"default_sort": {"key": "time_stamp", "order": "desc"},
		"limits": {"default": 50, "max": 200}
	}`, rec.Body.String())
}

func TestSystemEmailFiltersMapsErrors(t *testing.T) {
	svc := handlermocks.NewMockSystemEmailService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(nil, errors.New("boom")).Once()

	rec := do(systemEmailEngine(svc), http.MethodGet, systemEmailPath+"/filters", "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestSystemEmailRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantCode   string
	}{
		{"POST list", http.MethodPost, systemEmailPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"POST filters", http.MethodPost, systemEmailPath + "/filters", http.StatusMethodNotAllowed, "method_not_allowed"},
		{"unknown subpath", http.MethodGet, systemEmailPath + "/nope", http.StatusNotFound, "not_found"},
		{"not under the reports prefix", http.MethodGet, "/system-email-history", http.StatusNotFound, "not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(systemEmailEngine(handlermocks.NewMockSystemEmailService(t)), tt.method, tt.target, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
		})
	}
}

func TestNewSystemEmailHandlerAcceptsANilLogger(t *testing.T) {
	require.NotNil(t, handler.NewSystemEmailHandler(handlermocks.NewMockSystemEmailService(t), nil))
}
