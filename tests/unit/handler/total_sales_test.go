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

const totalSalesPath = "/api/v1/reports/total-sales"

func totalSalesEngine(svc handler.TotalSalesService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewTotalSalesHandler(svc, logger)},
	})
}

func emptyTotalSalesResult() *service.ListTotalSalesResult {
	return &service.ListTotalSalesResult{Items: []model.TotalSaleRow{}, Limit: 50}
}

// totalSalesParamsFor performs GET /total-sales?query against a mocked service and returns the params it saw.
func totalSalesParamsFor(t *testing.T, query string) service.ListTotalSalesParams {
	t.Helper()

	svc := handlermocks.NewMockTotalSalesService(t)
	var got service.ListTotalSalesParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListTotalSalesParams) { got = p }).
		Return(emptyTotalSalesResult(), nil).Once()

	rec := do(totalSalesEngine(svc), http.MethodGet, totalSalesPath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

func totalSalesNeverCalled(t *testing.T) *handlermocks.MockTotalSalesService {
	return handlermocks.NewMockTotalSalesService(t)
}

func TestTotalSalesListResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockTotalSalesService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListTotalSalesResult{
		Items: []model.TotalSaleRow{
			{ID: 12, InvoiceNumber: "85058", CustomerID: 7, CustomerName: "AAA1 ccc1",
				Date: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), Status: model.TotalSalePaid, Amount: 1364, WithoutTax: 1240},
			{ID: 13, InvoiceNumber: "85056", CustomerID: 7, CustomerName: "AAA1 ccc1",
				Date: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), Status: model.TotalSalePartial, Amount: 8540, WithoutTax: 8540},
		},
		Limit:      50,
		HasMore:    true,
		NextCursor: "opaque-cursor",
		Summary:    &model.TotalSaleSummary{Count: 28, Amount: 200635, WithoutTax: 179480},
		From:       time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(totalSalesEngine(svc), http.MethodGet, totalSalesPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{"id": 12, "invoice_number": "85058", "customer": {"id": 7, "name": "AAA1 ccc1"},
			 "date": "2026-10-26", "status": "paid", "amount": "13.64", "without_tax": "12.40"},
			{"id": 13, "invoice_number": "85056", "customer": {"id": 7, "name": "AAA1 ccc1"},
			 "date": "2026-10-26", "status": "partial", "amount": "85.40", "without_tax": "85.40"}
		],
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"summary": {"count": 28, "amount": "2006.35", "without_tax": "1794.80"},
		"range": {"from": "2026-10-01", "to": null}
	}`, rec.Body.String())
}

func TestTotalSalesListResponseForAnEmptyLaterPage(t *testing.T) {
	svc := handlermocks.NewMockTotalSalesService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListTotalSalesResult{
		Items: []model.TotalSaleRow{}, Limit: 5,
		From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(totalSalesEngine(svc), http.MethodGet, totalSalesPath+"?cursor=x", "")

	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 5, "has_more": false, "next_cursor": null},
		"summary": null,
		"range": {"from": "2026-10-01", "to": "2026-10-31"}
	}`, rec.Body.String())
}

func TestTotalSalesListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListTotalSalesParams
	}{
		{"no query", "", service.ListTotalSalesParams{}},
		{"every single-value parameter", "?from=2026-10-01&to=2026-10-31&q=85004&sort=amount&order=desc&limit=25&cursor=abc",
			service.ListTotalSalesParams{From: "2026-10-01", To: "2026-10-31", Search: "85004",
				Sort: "amount", Order: "desc", Limit: "25", Cursor: "abc"}},
		{"repeated key", "?status=paid&status=partial", service.ListTotalSalesParams{Status: []string{"paid", "partial"}}},
		{"comma separated", "?status=paid,partial", service.ListTotalSalesParams{Status: []string{"paid", "partial"}}},
		{"customer ids repeated and comma separated", "?customer_id=1,2&customer_id=3", service.ListTotalSalesParams{CustomerIDs: []string{"1", "2", "3"}}},
		{"whitespace and blanks are dropped", "?status=%20paid%20,,sent,&status=", service.ListTotalSalesParams{Status: []string{"paid", "sent"}}},
		{"only blanks leaves the list empty", "?status=&customer_id=,", service.ListTotalSalesParams{}},
		{"empty single values stay empty strings", "?from=&limit=", service.ListTotalSalesParams{}},
		{"percent-encoded text", "?q=d%40cm.co", service.ListTotalSalesParams{Search: "d@cm.co"}},
		{"a comma inside q is not a separator", "?q=a%2Cb", service.ListTotalSalesParams{Search: "a,b"}},
		{"sql text is just text", "?q=%27%3B+DROP+TABLE+billing_documents%3B--", service.ListTotalSalesParams{Search: "'; DROP TABLE billing_documents;--"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, totalSalesParamsFor(t, tt.query))
		})
	}
}

func TestTotalSalesListRejectsUnsupportedAndDuplicatedParameters(t *testing.T) {
	unsupported := func(name string) []dto.ErrorDetail {
		return []dto.ErrorDetail{{Field: name, Message: "is not a supported parameter"}}
	}
	tests := []struct {
		name  string
		query string
		want  []dto.ErrorDetail
	}{
		{"typo in a parameter name", "?statuss=paid", unsupported("statuss")},
		{"lifecycle is not offered", "?lifecycle=deleted", unsupported("lifecycle")},
		{"invoice parameter signature_status", "?signature_status=signed", unsupported("signature_status")},
		{"invoice parameter service_id", "?service_id=1", unsupported("service_id")},
		{"payment parameter method", "?method=card", unsupported("method")},
		{"cache buster", "?_=1699999999", unsupported("_")},
		{"single-value parameter twice", "?limit=1&limit=2", []dto.ErrorDetail{{Field: "limit", Message: "must be given only once"}}},
		{"problems are listed in a stable order", "?zzz=1&aaa=2&limit=1&limit=2", []dto.ErrorDetail{
			{Field: "aaa", Message: "is not a supported parameter"},
			{Field: "limit", Message: "must be given only once"},
			{Field: "zzz", Message: "is not a supported parameter"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(totalSalesEngine(totalSalesNeverCalled(t)), http.MethodGet, totalSalesPath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, tt.want, body.Details)
		})
	}
}

func TestTotalSalesListRejectsMalformedQueryStrings(t *testing.T) {
	for _, query := range []string{"?q=%zz", "?q=%", "?q=a;b=c"} {
		t.Run(query, func(t *testing.T) {
			rec := do(totalSalesEngine(totalSalesNeverCalled(t)), http.MethodGet, totalSalesPath+query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestTotalSalesListMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation error", model.InvalidInput(model.FieldError{Field: "limit", Message: "must be a whole number between 1 and 200"}), 422, "invalid_input"},
		{"wrapped validation error", fmt.Errorf("list: %w", model.InvalidInput(model.FieldError{Field: "q", Message: "x"})), 422, "invalid_input"},
		{"storage error", errors.New("pq: password authentication failed"), 500, "internal_error"},
		{"deadline exceeded", fmt.Errorf("list total sales: %w", context.DeadlineExceeded), 504, "timeout"},
		{"client went away", fmt.Errorf("list total sales: %w", context.Canceled), 499, "client_closed_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := handlermocks.NewMockTotalSalesService(t)
			svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, tt.err).Once()

			rec := do(totalSalesEngine(svc), http.MethodGet, totalSalesPath, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
			require.NotContains(t, rec.Body.String(), "password", "internal error text must not leak")
		})
	}

	t.Run("validation details reach the client", func(t *testing.T) {
		svc := handlermocks.NewMockTotalSalesService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil,
			model.InvalidInput(model.FieldError{Field: "status", Message: "must be one of: paid, partial, sent"})).Once()

		rec := do(totalSalesEngine(svc), http.MethodGet, totalSalesPath+"?status=void", "")

		require.Equal(t, []dto.ErrorDetail{{Field: "status", Message: "must be one of: paid, partial, sent"}}, decodeError(t, rec).Details)
	})
}

func TestTotalSalesListPassesTheRequestContextToTheService(t *testing.T) {
	type key struct{}
	svc := handlermocks.NewMockTotalSalesService(t)
	svc.EXPECT().List(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(key{}) == "marker" }), mock.Anything).
		Return(emptyTotalSalesResult(), nil).Once()

	req := httptest.NewRequest(http.MethodGet, totalSalesPath, nil).
		WithContext(context.WithValue(context.Background(), key{}, "marker"))
	rec := httptest.NewRecorder()
	totalSalesEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestTotalSalesResponsesAreNeverCached(t *testing.T) {
	setRequestID := func(c *gin.Context) { c.Header("X-Request-ID", "rid-7"); c.Next() }

	t.Run("success", func(t *testing.T) {
		svc := handlermocks.NewMockTotalSalesService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(emptyTotalSalesResult(), nil).Once()

		rec := do(totalSalesEngine(svc, setRequestID), http.MethodGet, totalSalesPath, "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		require.Equal(t, "rid-7", rec.Header().Get("X-Request-ID"))
	})

	t.Run("validation error", func(t *testing.T) {
		rec := do(totalSalesEngine(totalSalesNeverCalled(t), setRequestID), http.MethodGet, totalSalesPath+"?nope=1", "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "rid-7", decodeError(t, rec).RequestID)
	})

	t.Run("server error", func(t *testing.T) {
		svc := handlermocks.NewMockTotalSalesService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errors.New("boom")).Once()

		rec := do(totalSalesEngine(svc), http.MethodGet, totalSalesPath, "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})

	t.Run("filters", func(t *testing.T) {
		rec := do(totalSalesEngine(totalSalesNeverCalled(t)), http.MethodGet, totalSalesPath+"/filters", "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})
}

func TestTotalSalesFiltersResponseJSONContract(t *testing.T) {
	// the choices are fixed, so the service (a mock with no expectations) is never called
	rec := do(totalSalesEngine(totalSalesNeverCalled(t)), http.MethodGet, totalSalesPath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"statuses": ["paid", "partial", "sent"],
		"sort_keys": ["invoice_number", "customer", "date", "status", "amount", "without_tax"],
		"default_sort": {"key": "date", "order": "desc"},
		"limits": {"default": 50, "max": 200}
	}`, rec.Body.String())
}

func TestTotalSalesRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantCode   string
	}{
		{"POST list", http.MethodPost, totalSalesPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"DELETE list", http.MethodDelete, totalSalesPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"POST filters", http.MethodPost, totalSalesPath + "/filters", http.StatusMethodNotAllowed, "method_not_allowed"},
		{"unknown subpath", http.MethodGet, totalSalesPath + "/nope", http.StatusNotFound, "not_found"},
		{"not under the reports prefix", http.MethodGet, "/total-sales", http.StatusNotFound, "not_found"},
		{"invoice routes are not served by this handler", http.MethodGet, invoicesPath, http.StatusNotFound, "not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(totalSalesEngine(totalSalesNeverCalled(t)), tt.method, tt.target, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
		})
	}
}

func TestNewTotalSalesHandlerAcceptsANilLogger(t *testing.T) {
	require.NotNil(t, handler.NewTotalSalesHandler(totalSalesNeverCalled(t), nil))
}

func TestTotalSalesLongUnknownParameterNameIsTruncated(t *testing.T) {
	rec := do(totalSalesEngine(totalSalesNeverCalled(t)), http.MethodGet, totalSalesPath+"?"+strings.Repeat("x", 5000)+"=1", "")

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Equal(t, strings.Repeat("x", 64)+"...", decodeError(t, rec).Details[0].Field)
}
