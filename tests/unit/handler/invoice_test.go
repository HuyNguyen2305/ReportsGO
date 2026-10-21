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

const invoicesPath = "/api/v1/reports/invoices"

func strPtr(s string) *string { return &s }

func invoiceEngine(svc handler.InvoiceService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewInvoiceHandler(svc, logger)},
	})
}

// emptyResult is a valid, boring service result.
func emptyResult() *service.ListInvoicesResult {
	return &service.ListInvoicesResult{Items: []model.InvoiceRow{}, Limit: 50}
}

// listParamsFor performs GET /invoices?query against a mocked service and returns the params the service saw.
func listParamsFor(t *testing.T, query string) service.ListInvoicesParams {
	t.Helper()

	svc := handlermocks.NewMockInvoiceService(t)
	var got service.ListInvoicesParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListInvoicesParams) { got = p }).
		Return(emptyResult(), nil).Once()

	rec := do(invoiceEngine(svc), http.MethodGet, invoicesPath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

// neverCalled is a service that fails the test if it is used.
func neverCalled(t *testing.T) *handlermocks.MockInvoiceService {
	return handlermocks.NewMockInvoiceService(t)
}

func TestListResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockInvoiceService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListInvoicesResult{
		Items: []model.InvoiceRow{
			{
				ID: 7, InvoiceNumber: "85005", AccountNumber: "63714", CustomerID: 5, CustomerName: "#F Last Name",
				Job: strPtr("Blank Service 123 (S1 S2)"), FrequencyLabel: "With each job", BillingEmail: strPtr("d@cm.co"),
				Date: time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC), Total: 15226, Discount: 0,
				Tags: []string{"a", "b"}, PaymentTermsName: strPtr("Net 30 Terms"), PaymentTermsDescription: strPtr("Payment is due within 30 days."),
				Status: model.InvoicePaid, SignatureStatus: model.SignatureDraft,
			},
			{
				ID: 8, InvoiceNumber: "N1", AccountNumber: "63716", CustomerID: 6, CustomerName: "# D",
				FrequencyLabel: "", Date: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC), Total: -500, Discount: 123,
				Status: model.InvoiceDraft, SignatureStatus: model.SignatureDraft,
			},
		},
		Limit:      50,
		HasMore:    true,
		NextCursor: "opaque-cursor",
		Summary:    &model.InvoiceSummary{Count: 21, Total: 242840, Discount: 123, Outstanding: 26400},
		From:       time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(invoiceEngine(svc), http.MethodGet, invoicesPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{
				"id": 7, "invoice_number": "85005", "account_number": "63714",
				"customer": {"id": 5, "name": "#F Last Name"},
				"job": "Blank Service 123 (S1 S2)", "frequency": "With each job", "billing_email": "d@cm.co",
				"date": "2026-10-11", "total": "152.26", "discount": "0.00",
				"tags": ["a", "b"],
				"payment_terms": {"name": "Net 30 Terms", "description": "Payment is due within 30 days."},
				"status": "paid", "signature_status": "draft"
			},
			{
				"id": 8, "invoice_number": "N1", "account_number": "63716",
				"customer": {"id": 6, "name": "# D"},
				"job": null, "frequency": "", "billing_email": null,
				"date": "2026-10-10", "total": "-5.00", "discount": "1.23",
				"tags": [],
				"payment_terms": null,
				"status": "draft", "signature_status": "draft"
			}
		],
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"summary": {"count": 21, "total": "2428.40", "discount": "1.23", "outstanding": "264.00"},
		"range": {"from": "2026-10-01", "to": null}
	}`, rec.Body.String())
}

func TestListResponseOnALaterPageHasNullSummaryAndCursor(t *testing.T) {
	svc := handlermocks.NewMockInvoiceService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListInvoicesResult{
		Items: []model.InvoiceRow{}, Limit: 5,
		From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(invoiceEngine(svc), http.MethodGet, invoicesPath+"?cursor=x", "")

	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 5, "has_more": false, "next_cursor": null},
		"summary": null,
		"range": {"from": "2026-10-01", "to": "2026-10-31"}
	}`, rec.Body.String())
}

func TestListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListInvoicesParams
	}{
		{"no query", "", service.ListInvoicesParams{}},
		{"every single-value parameter", "?from=2026-10-01&to=2026-10-31&lifecycle=archived&q=saliba&sort=total&order=desc&limit=25&cursor=abc",
			service.ListInvoicesParams{From: "2026-10-01", To: "2026-10-31", Lifecycle: "archived", Search: "saliba",
				Sort: "total", Order: "desc", Limit: "25", Cursor: "abc"}},
		{"repeated key", "?status=paid&status=sent", service.ListInvoicesParams{Status: []string{"paid", "sent"}}},
		{"comma separated", "?status=paid,sent", service.ListInvoicesParams{Status: []string{"paid", "sent"}}},
		{"repeated and comma separated mixed", "?status=paid,sent&status=void", service.ListInvoicesParams{Status: []string{"paid", "sent", "void"}}},
		{"whitespace and blanks are dropped", "?status=%20paid%20,,sent,&status=", service.ListInvoicesParams{Status: []string{"paid", "sent"}}},
		{"only blanks leaves the list empty", "?status=&tag_id=,", service.ListInvoicesParams{}},
		{"every multi-value parameter", "?status=paid&signature_status=signed&service_id=1,2&tag_id=3&payment_term_id=4",
			service.ListInvoicesParams{Status: []string{"paid"}, SignatureStatus: []string{"signed"},
				ServiceIDs: []string{"1", "2"}, TagIDs: []string{"3"}, PaymentTermIDs: []string{"4"}}},
		{"empty single values stay empty strings", "?from=&limit=", service.ListInvoicesParams{}},

		{"percent-encoded at sign", "?q=d%40cm.co", service.ListInvoicesParams{Search: "d@cm.co"}},
		{"percent-encoded space", "?q=a%20b", service.ListInvoicesParams{Search: "a b"}},
		{"plus is a space", "?q=a+b", service.ListInvoicesParams{Search: "a b"}},
		{"encoded percent sign", "?q=50%25", service.ListInvoicesParams{Search: "50%"}},
		{"encoded accented letter", "?q=%C3%A9", service.ListInvoicesParams{Search: "é"}},
		{"a comma inside q is not a separator", "?q=a%2Cb", service.ListInvoicesParams{Search: "a,b"}},
		{"encoded ampersand and equals", "?q=%26%3D", service.ListInvoicesParams{Search: "&="}},
		{"q is passed verbatim, the service trims", "?q=%20x%20", service.ListInvoicesParams{Search: " x "}},
		{"sql text is just text", "?q=%27%3B+DROP+TABLE+invoices%3B--", service.ListInvoicesParams{Search: "'; DROP TABLE invoices;--"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, listParamsFor(t, tt.query))
		})
	}
}

func TestListRejectsUnsupportedAndDuplicatedParameters(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []dto.ErrorDetail
	}{
		{"typo in a parameter name", "?statuss=paid", []dto.ErrorDetail{{Field: "statuss", Message: "is not a supported parameter"}}},
		{"names are case-sensitive", "?Status=paid", []dto.ErrorDetail{{Field: "Status", Message: "is not a supported parameter"}}},
		{"cache buster", "?_=1699999999", []dto.ErrorDetail{{Field: "_", Message: "is not a supported parameter"}}},
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
			rec := do(invoiceEngine(neverCalled(t)), http.MethodGet, invoicesPath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, tt.want, body.Details)
		})
	}

	t.Run("a very long unknown name is truncated in the error", func(t *testing.T) {
		rec := do(invoiceEngine(neverCalled(t)), http.MethodGet, invoicesPath+"?"+strings.Repeat("x", 5000)+"=1", "")

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		field := decodeError(t, rec).Details[0].Field
		require.Equal(t, strings.Repeat("x", 64)+"...", field)
	})
}

func TestListRejectsMalformedQueryStrings(t *testing.T) {
	tests := []struct{ name, query string }{
		{"bad percent-encoding", "?q=%zz"},
		{"truncated percent-encoding", "?q=%"},
		{"semicolon separator", "?q=a;b=c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(invoiceEngine(neverCalled(t)), http.MethodGet, invoicesPath+tt.query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestListMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation error", model.InvalidInput(model.FieldError{Field: "limit", Message: "must be a whole number between 1 and 200"}), 422, "invalid_input"},
		{"wrapped validation error", fmt.Errorf("list: %w", model.InvalidInput(model.FieldError{Field: "q", Message: "x"})), 422, "invalid_input"},
		{"storage error", errors.New("pq: password authentication failed"), 500, "internal_error"},
		{"deadline exceeded", fmt.Errorf("list invoices: %w", context.DeadlineExceeded), 504, "timeout"},
		{"client went away", fmt.Errorf("list invoices: %w", context.Canceled), 499, "client_closed_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := handlermocks.NewMockInvoiceService(t)
			svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, tt.err).Once()

			rec := do(invoiceEngine(svc), http.MethodGet, invoicesPath, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, tt.wantCode, body.Code)
			require.NotContains(t, rec.Body.String(), "password", "internal error text must not leak")
		})
	}

	t.Run("validation details reach the client", func(t *testing.T) {
		svc := handlermocks.NewMockInvoiceService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil,
			model.InvalidInput(model.FieldError{Field: "limit", Message: "must be a whole number between 1 and 200"})).Once()

		rec := do(invoiceEngine(svc), http.MethodGet, invoicesPath+"?limit=0", "")

		require.Equal(t, []dto.ErrorDetail{{Field: "limit", Message: "must be a whole number between 1 and 200"}}, decodeError(t, rec).Details)
	})
}

func TestListPassesTheRequestContextToTheService(t *testing.T) {
	type key struct{}
	svc := handlermocks.NewMockInvoiceService(t)
	svc.EXPECT().List(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(key{}) == "marker" }), mock.Anything).
		Return(emptyResult(), nil).Once()

	req := httptest.NewRequest(http.MethodGet, invoicesPath, nil).
		WithContext(context.WithValue(context.Background(), key{}, "marker"))
	rec := httptest.NewRecorder()
	invoiceEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestReportResponsesAreNeverCached(t *testing.T) {
	setRequestID := func(c *gin.Context) { c.Header("X-Request-ID", "rid-7"); c.Next() }

	t.Run("success", func(t *testing.T) {
		svc := handlermocks.NewMockInvoiceService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(emptyResult(), nil).Once()

		rec := do(invoiceEngine(svc, setRequestID), http.MethodGet, invoicesPath, "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		require.Equal(t, "rid-7", rec.Header().Get("X-Request-ID"))
	})

	t.Run("validation error", func(t *testing.T) {
		rec := do(invoiceEngine(neverCalled(t), setRequestID), http.MethodGet, invoicesPath+"?nope=1", "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "rid-7", decodeError(t, rec).RequestID, "error body carries the request id")
	})

	t.Run("server error", func(t *testing.T) {
		svc := handlermocks.NewMockInvoiceService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errors.New("boom")).Once()

		rec := do(invoiceEngine(svc), http.MethodGet, invoicesPath, "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})

	t.Run("health endpoints are not part of the reports group", func(t *testing.T) {
		rec := do(invoiceEngine(neverCalled(t)), http.MethodGet, "/healthz", "")
		require.Empty(t, rec.Header().Get("Cache-Control"))
	})
}

func TestFiltersResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockInvoiceService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.InvoiceFilterOptions{
		ServiceTypes: []model.ServiceType{{ID: 1, Name: "Bi-Monthly Service"}},
		Tags:         []model.InvoiceTag{{ID: 2, Name: "a"}, {ID: 3, Name: "b"}},
		PaymentTerms: []model.PaymentTerm{{ID: 4, Name: "Net 30 Terms", Description: "30 days"}},
	}, nil).Once()

	rec := do(invoiceEngine(svc), http.MethodGet, invoicesPath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.JSONEq(t, `{
		"service_types": [{"id": 1, "name": "Bi-Monthly Service"}],
		"tags": [{"id": 2, "name": "a"}, {"id": 3, "name": "b"}],
		"payment_terms": [{"id": 4, "name": "Net 30 Terms", "description": "30 days"}],
		"statuses": ["draft", "sent", "paid", "void"],
		"signature_statuses": ["draft", "pending", "signed", "declined"],
		"lifecycles": ["active", "archived", "deleted"],
		"sort_keys": ["account_number", "invoice_number", "customer", "job", "billing_email", "date",
		              "total", "discount", "status", "signature_status", "payment_terms"],
		"default_sort": {"key": "date", "order": "desc"},
		"limits": {"default": 50, "max": 200}
	}`, rec.Body.String())
}

func TestFiltersResponseWithNoOptionsUsesEmptyArrays(t *testing.T) {
	svc := handlermocks.NewMockInvoiceService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.InvoiceFilterOptions{}, nil).Once()

	rec := do(invoiceEngine(svc), http.MethodGet, invoicesPath+"/filters", "")

	require.Contains(t, rec.Body.String(), `"service_types":[]`)
	require.Contains(t, rec.Body.String(), `"tags":[]`)
	require.Contains(t, rec.Body.String(), `"payment_terms":[]`)
}

func TestFiltersServiceError(t *testing.T) {
	svc := handlermocks.NewMockInvoiceService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(nil, errors.New("db down")).Once()

	rec := do(invoiceEngine(svc), http.MethodGet, invoicesPath+"/filters", "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "internal_error", decodeError(t, rec).Code)
	require.NotContains(t, rec.Body.String(), "db down")
}

func TestInvoiceRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantCode   string
	}{
		{"POST list", http.MethodPost, invoicesPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"DELETE list", http.MethodDelete, invoicesPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"POST filters", http.MethodPost, invoicesPath + "/filters", http.StatusMethodNotAllowed, "method_not_allowed"},
		{"unknown subpath", http.MethodGet, invoicesPath + "/nope", http.StatusNotFound, "not_found"},
		{"a report that does not exist", http.MethodGet, "/api/v1/reports/estimates", http.StatusNotFound, "not_found"},
		{"not under the reports prefix", http.MethodGet, "/invoices", http.StatusNotFound, "not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(invoiceEngine(neverCalled(t)), tt.method, tt.target, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
		})
	}
}
