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

const customersPath = "/api/v1/reports/new-customers"

func customerEngine(svc handler.CustomerService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewCustomerHandler(svc, logger)},
	})
}

func emptyCustomerResult() *service.ListCustomersResult {
	return &service.ListCustomersResult{Items: []model.CustomerRow{}, Limit: 50}
}

// customerParamsFor performs GET /new-customers?query against a mocked service and returns the params it saw.
func customerParamsFor(t *testing.T, query string) service.ListCustomersParams {
	t.Helper()

	svc := handlermocks.NewMockCustomerService(t)
	var got service.ListCustomersParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListCustomersParams) { got = p }).
		Return(emptyCustomerResult(), nil).Once()

	rec := do(customerEngine(svc), http.MethodGet, customersPath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

func customerNeverCalled(t *testing.T) *handlermocks.MockCustomerService {
	return handlermocks.NewMockCustomerService(t)
}

func TestCustomerListResponseJSONContract(t *testing.T) {
	plus7 := time.FixedZone("UTC+7", 7*3600)
	svc := handlermocks.NewMockCustomerService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListCustomersResult{
		Items: []model.CustomerRow{
			{
				ID: 15, Name: "Ada Lovelace", AccountNumber: "70001",
				Email: strPtr("ada@example.com"), Phone: strPtr("555-0101"), SourceID: int64Ptr(2), Source: strPtr("Google"),
				CreatedAt: time.Date(2026, 10, 22, 8, 15, 30, 123456000, time.UTC),
			},
			{
				// nothing optional; a whole second from a non-UTC zone is still written in UTC with six fractional digits
				ID: 3, Name: "PerfTest", AccountNumber: "63701",
				CreatedAt: time.Date(2026, 10, 12, 15, 0, 0, 0, plus7),
			},
		},
		Limit:      50,
		HasMore:    true,
		NextCursor: "opaque-cursor",
		Summary:    &model.CustomerSummary{Count: 14},
		From:       time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(customerEngine(svc), http.MethodGet, customersPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{
				"id": 15, "customer": {"id": 15, "name": "Ada Lovelace"}, "account_number": "70001",
				"email": "ada@example.com", "phone": "555-0101", "source": {"id": 2, "name": "Google"},
				"created_at": "2026-10-22T08:15:30.123456Z"
			},
			{
				"id": 3, "customer": {"id": 3, "name": "PerfTest"}, "account_number": "63701",
				"email": null, "phone": null, "source": null,
				"created_at": "2026-10-12T08:00:00.000000Z"
			}
		],
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"summary": {"count": 14},
		"range": {"from": "2026-10-01", "to": null}
	}`, rec.Body.String())
}

func TestCustomerListResponseForAnEmptyLaterPage(t *testing.T) {
	svc := handlermocks.NewMockCustomerService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListCustomersResult{
		Items: []model.CustomerRow{}, Limit: 5,
		From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(customerEngine(svc), http.MethodGet, customersPath+"?cursor=x", "")

	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 5, "has_more": false, "next_cursor": null},
		"summary": null,
		"range": {"from": "2026-10-01", "to": "2026-10-31"}
	}`, rec.Body.String())
}

func TestCustomerListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListCustomersParams
	}{
		{"no query", "", service.ListCustomersParams{}},
		{"every single-value parameter", "?from=2026-10-01&to=2026-10-31&q=ada&sort=email&order=desc&limit=25&cursor=abc",
			service.ListCustomersParams{From: "2026-10-01", To: "2026-10-31", Search: "ada", Sort: "email", Order: "desc", Limit: "25", Cursor: "abc"}},
		{"repeated key", "?lead_source_id=1&lead_source_id=2", service.ListCustomersParams{LeadSourceIDs: []string{"1", "2"}}},
		{"comma separated", "?lead_source_id=1,2", service.ListCustomersParams{LeadSourceIDs: []string{"1", "2"}}},
		{"repeated and comma separated mixed", "?lead_source_id=1,2&lead_source_id=3", service.ListCustomersParams{LeadSourceIDs: []string{"1", "2", "3"}}},
		{"whitespace and blanks are dropped", "?lead_source_id=%201%20,,2,&lead_source_id=", service.ListCustomersParams{LeadSourceIDs: []string{"1", "2"}}},
		{"only blanks leaves the list empty", "?lead_source_id=,", service.ListCustomersParams{}},
		{"empty single values stay empty strings", "?from=&limit=", service.ListCustomersParams{}},

		{"percent-encoded at sign", "?q=ada%40example.com", service.ListCustomersParams{Search: "ada@example.com"}},
		{"plus is a space", "?q=a+b", service.ListCustomersParams{Search: "a b"}},
		{"encoded percent sign", "?q=50%25", service.ListCustomersParams{Search: "50%"}},
		{"a comma inside q is not a separator", "?q=a%2Cb", service.ListCustomersParams{Search: "a,b"}},
		{"sql text is just text", "?q=%27%3B+DROP+TABLE+customers%3B--", service.ListCustomersParams{Search: "'; DROP TABLE customers;--"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, customerParamsFor(t, tt.query))
		})
	}
}

func TestCustomerListRejectsUnsupportedAndDuplicatedParameters(t *testing.T) {
	unsupported := func(name string) []dto.ErrorDetail {
		return []dto.ErrorDetail{{Field: name, Message: "is not a supported parameter"}}
	}
	tests := []struct {
		name  string
		query string
		want  []dto.ErrorDetail
	}{
		{"typo in a parameter name", "?lead_source=1", unsupported("lead_source")},
		{"names are case-sensitive", "?Lead_Source_Id=1", unsupported("Lead_Source_Id")},
		{"customers have no status", "?status=active", unsupported("status")},
		{"customers have no tabs", "?lifecycle=active", unsupported("lifecycle")},
		{"job parameter service_type_id", "?service_type_id=1", unsupported("service_type_id")},
		{"payment parameter method", "?method=cash", unsupported("method")},
		{"single-value parameter twice", "?limit=1&limit=2", []dto.ErrorDetail{{Field: "limit", Message: "must be given only once"}}},
		{"from twice", "?from=2026-10-01&from=2026-10-02", []dto.ErrorDetail{{Field: "from", Message: "must be given only once"}}},
		{"problems are listed in a stable order", "?zzz=1&aaa=2&limit=1&limit=2", []dto.ErrorDetail{
			{Field: "aaa", Message: "is not a supported parameter"},
			{Field: "limit", Message: "must be given only once"},
			{Field: "zzz", Message: "is not a supported parameter"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(customerEngine(customerNeverCalled(t)), http.MethodGet, customersPath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, tt.want, body.Details)
		})
	}

	t.Run("a very long unknown name is truncated in the error", func(t *testing.T) {
		rec := do(customerEngine(customerNeverCalled(t)), http.MethodGet, customersPath+"?"+strings.Repeat("x", 5000)+"=1", "")

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Equal(t, strings.Repeat("x", 64)+"...", decodeError(t, rec).Details[0].Field)
	})
}

func TestCustomerListRejectsMalformedQueryStrings(t *testing.T) {
	for _, query := range []string{"?q=%zz", "?q=%", "?q=a;b=c"} {
		t.Run(query, func(t *testing.T) {
			rec := do(customerEngine(customerNeverCalled(t)), http.MethodGet, customersPath+query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestCustomerListMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation error", model.InvalidInput(model.FieldError{Field: "limit", Message: "must be a whole number between 1 and 200"}), 422, "invalid_input"},
		{"wrapped validation error", fmt.Errorf("list: %w", model.InvalidInput(model.FieldError{Field: "q", Message: "x"})), 422, "invalid_input"},
		{"storage error", errors.New("pq: password authentication failed"), 500, "internal_error"},
		{"deadline exceeded", fmt.Errorf("list customers: %w", context.DeadlineExceeded), 504, "timeout"},
		{"client went away", fmt.Errorf("list customers: %w", context.Canceled), 499, "client_closed_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := handlermocks.NewMockCustomerService(t)
			svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, tt.err).Once()

			rec := do(customerEngine(svc), http.MethodGet, customersPath, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
			require.NotContains(t, rec.Body.String(), "password", "internal error text must not leak")
		})
	}

	t.Run("validation details reach the client", func(t *testing.T) {
		svc := handlermocks.NewMockCustomerService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil,
			model.InvalidInput(model.FieldError{Field: "lead_source_id", Message: "must be positive whole numbers"})).Once()

		rec := do(customerEngine(svc), http.MethodGet, customersPath+"?lead_source_id=x", "")

		require.Equal(t, []dto.ErrorDetail{{Field: "lead_source_id", Message: "must be positive whole numbers"}}, decodeError(t, rec).Details)
	})
}

func TestCustomerListPassesTheRequestContextToTheService(t *testing.T) {
	type key struct{}
	svc := handlermocks.NewMockCustomerService(t)
	svc.EXPECT().List(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(key{}) == "marker" }), mock.Anything).
		Return(emptyCustomerResult(), nil).Once()

	req := httptest.NewRequest(http.MethodGet, customersPath, nil).
		WithContext(context.WithValue(context.Background(), key{}, "marker"))
	rec := httptest.NewRecorder()
	customerEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestCustomerResponsesAreNeverCached(t *testing.T) {
	setRequestID := func(c *gin.Context) { c.Header("X-Request-ID", "rid-7"); c.Next() }

	t.Run("success", func(t *testing.T) {
		svc := handlermocks.NewMockCustomerService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(emptyCustomerResult(), nil).Once()

		rec := do(customerEngine(svc, setRequestID), http.MethodGet, customersPath, "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		require.Equal(t, "rid-7", rec.Header().Get("X-Request-ID"))
	})

	t.Run("validation error", func(t *testing.T) {
		rec := do(customerEngine(customerNeverCalled(t), setRequestID), http.MethodGet, customersPath+"?nope=1", "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "rid-7", decodeError(t, rec).RequestID)
	})

	t.Run("server error", func(t *testing.T) {
		svc := handlermocks.NewMockCustomerService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errors.New("boom")).Once()

		rec := do(customerEngine(svc), http.MethodGet, customersPath, "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})

	t.Run("filters", func(t *testing.T) {
		svc := handlermocks.NewMockCustomerService(t)
		svc.EXPECT().FilterOptions(mock.Anything).Return(&service.CustomerFilterOptions{}, nil).Once()

		rec := do(customerEngine(svc), http.MethodGet, customersPath+"/filters", "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})
}

func TestCustomerFiltersResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockCustomerService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.CustomerFilterOptions{
		LeadSources: []model.LeadSource{{ID: 1, Name: "Facebook"}, {ID: 2, Name: "Google"}},
	}, nil).Once()

	rec := do(customerEngine(svc), http.MethodGet, customersPath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"lead_sources": [{"id": 1, "name": "Facebook"}, {"id": 2, "name": "Google"}],
		"sort_keys": ["customer", "account_number", "email", "phone", "source", "created_at"],
		"default_sort": {"key": "created_at", "order": "desc"},
		"limits": {"default": 50, "max": 200}
	}`, rec.Body.String())
}

func TestCustomerFiltersWithNoSourcesUseAnEmptyArray(t *testing.T) {
	svc := handlermocks.NewMockCustomerService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.CustomerFilterOptions{}, nil).Once()

	rec := do(customerEngine(svc), http.MethodGet, customersPath+"/filters", "")

	require.Contains(t, rec.Body.String(), `"lead_sources":[]`)
}

func TestCustomerFiltersServiceError(t *testing.T) {
	svc := handlermocks.NewMockCustomerService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(nil, errors.New("db down")).Once()

	rec := do(customerEngine(svc), http.MethodGet, customersPath+"/filters", "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "internal_error", decodeError(t, rec).Code)
	require.NotContains(t, rec.Body.String(), "db down")
}

func TestCustomerFiltersPassTheRequestContextToTheService(t *testing.T) {
	type key struct{}
	svc := handlermocks.NewMockCustomerService(t)
	svc.EXPECT().FilterOptions(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(key{}) == "marker" })).
		Return(&service.CustomerFilterOptions{}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, customersPath+"/filters", nil).
		WithContext(context.WithValue(context.Background(), key{}, "marker"))
	rec := httptest.NewRecorder()
	customerEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestCustomerRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantCode   string
	}{
		{"POST list", http.MethodPost, customersPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"DELETE list", http.MethodDelete, customersPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"POST filters", http.MethodPost, customersPath + "/filters", http.StatusMethodNotAllowed, "method_not_allowed"},
		{"unknown subpath", http.MethodGet, customersPath + "/nope", http.StatusNotFound, "not_found"},
		{"not under the reports prefix", http.MethodGet, "/new-customers", http.StatusNotFound, "not_found"},
		{"invoice routes are not served by this handler", http.MethodGet, invoicesPath, http.StatusNotFound, "not_found"},
		{"job routes are not served by this handler", http.MethodGet, jobsPath, http.StatusNotFound, "not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(customerEngine(customerNeverCalled(t)), tt.method, tt.target, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
		})
	}
}

func TestNewCustomerHandlerAcceptsANilLogger(t *testing.T) {
	require.NotNil(t, handler.NewCustomerHandler(customerNeverCalled(t), nil))
}
