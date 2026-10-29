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

const accountsAgingPath = "/api/v1/reports/accounts-aging"

func accountsAgingEngine(svc handler.AccountsAgingService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewAccountsAgingHandler(svc, logger)},
	})
}

func emptyAgingResult() *service.ListAgingResult {
	return &service.ListAgingResult{Items: []model.AgingRow{}, Limit: 50, AsOf: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)}
}

// agingParamsFor performs GET /accounts-aging?query against a mocked service and returns the params it saw.
func agingParamsFor(t *testing.T, query string) service.ListAgingParams {
	t.Helper()

	svc := handlermocks.NewMockAccountsAgingService(t)
	var got service.ListAgingParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListAgingParams) { got = p }).
		Return(emptyAgingResult(), nil).Once()

	rec := do(accountsAgingEngine(svc), http.MethodGet, accountsAgingPath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

func TestAccountsAgingListResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockAccountsAgingService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListAgingResult{
		Items: []model.AgingRow{
			{
				ID: 4294967297, CustomerID: 1, LocationID: int64Ptr(1), CustomerName: "AAA1 ccc1",
				Location: strPtr("Miami Beach Boardwalk 2, Miami"), BillingEmail: strPtr("a@example.com"), Phone: strPtr("555-0100"),
				Card:   &model.AgingCard{Processor: "stripe", Last4: "3222", ExpMonth: 2, ExpYear: 2034},
				D0to30: 7000, D31to60: 100, D61to90: 200, D90Plus: 300, Total: 7600, AvailableCredit: 30000,
			},
			{
				ID: 38654705664, CustomerID: 9, CustomerName: "Bukayo Saka",
				D0to30: 9900, Total: 9900,
			},
		},
		Limit:      50,
		HasMore:    true,
		NextCursor: "opaque-cursor",
		Summary: &model.AgingSummary{
			Count: 2, D0to30: 16900, D31to60: 100, D61to90: 200, D90Plus: 300, Total: 17500, AvailableCredit: 30000,
		},
		AsOf: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(accountsAgingEngine(svc), http.MethodGet, accountsAgingPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{"customer_id": 1, "location_id": 1, "customer": "AAA1 ccc1", "location": "Miami Beach Boardwalk 2, Miami",
			 "billing_email": "a@example.com", "phone": "555-0100",
			 "credit_card": {"processor": "stripe", "last4": "3222", "expires": "02/2034"},
			 "d0_30": "70.00", "d31_60": "1.00", "d61_90": "2.00", "d90_plus": "3.00", "total": "76.00",
			 "available_credit": "300.00"},
			{"customer_id": 9, "location_id": null, "customer": "Bukayo Saka", "location": null,
			 "billing_email": null, "phone": null, "credit_card": null,
			 "d0_30": "99.00", "d31_60": "0.00", "d61_90": "0.00", "d90_plus": "0.00", "total": "99.00",
			 "available_credit": "0.00"}
		],
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"summary": {"count": 2, "d0_30": "169.00", "d31_60": "1.00", "d61_90": "2.00", "d90_plus": "3.00",
			"total": "175.00", "available_credit": "300.00"},
		"as_of": "2026-10-21"
	}`, rec.Body.String())
}

func TestAccountsAgingListResponseForAnEmptyLaterPage(t *testing.T) {
	svc := handlermocks.NewMockAccountsAgingService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(emptyAgingResult(), nil).Once()

	rec := do(accountsAgingEngine(svc), http.MethodGet, accountsAgingPath+"?cursor=x", "")

	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 50, "has_more": false, "next_cursor": null},
		"summary": null,
		"as_of": "2026-10-21"
	}`, rec.Body.String())
}

func TestAccountsAgingListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListAgingParams
	}{
		{"no query", "", service.ListAgingParams{}},
		{"every single-value parameter", "?as_of=2026-09-30&days=60&email=%40example.com&q=saka&sort=total&order=desc&limit=25&cursor=abc",
			service.ListAgingParams{AsOf: "2026-09-30", Days: "60", Email: "@example.com", Search: "saka", Sort: "total",
				Order: "desc", Limit: "25", Cursor: "abc"}},
		{"repeated key", "?service_type=1&service_type=2", service.ListAgingParams{ServiceType: []string{"1", "2"}}},
		{"comma separated", "?service_type=1,2", service.ListAgingParams{ServiceType: []string{"1", "2"}}},
		{"blanks are dropped", "?service_type=,&service_type=", service.ListAgingParams{}},
		{"empty single values stay empty strings", "?as_of=&days=&limit=", service.ListAgingParams{}},
		{"sql text is just text", "?q=%27%3B+DROP+TABLE+customers%3B--", service.ListAgingParams{Search: "'; DROP TABLE customers;--"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, agingParamsFor(t, tt.query))
		})
	}
}

func TestAccountsAgingListRejectsUnsupportedAndDuplicatedParameters(t *testing.T) {
	unsupported := func(name string) []dto.ErrorDetail {
		return []dto.ErrorDetail{{Field: name, Message: "is not a supported parameter"}}
	}
	tests := []struct {
		name  string
		query string
		want  []dto.ErrorDetail
	}{
		{"a date range is not offered", "?from=2026-10-01", unsupported("from")},
		{"a typo in a parameter name", "?service_types=1", unsupported("service_types")},
		{"customer id is not offered", "?customer_id=1", unsupported("customer_id")},
		{"days twice", "?days=30&days=60", []dto.ErrorDetail{{Field: "days", Message: "must be given only once"}}},
		{"as_of twice", "?as_of=2026-10-01&as_of=2026-10-02", []dto.ErrorDetail{{Field: "as_of", Message: "must be given only once"}}},
		{"problems are listed in a stable order", "?zzz=1&aaa=2&limit=1&limit=2", []dto.ErrorDetail{
			{Field: "aaa", Message: "is not a supported parameter"},
			{Field: "limit", Message: "must be given only once"},
			{Field: "zzz", Message: "is not a supported parameter"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(accountsAgingEngine(handlermocks.NewMockAccountsAgingService(t)), http.MethodGet, accountsAgingPath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, tt.want, body.Details)
		})
	}
}

func TestAccountsAgingListRejectsMalformedQueryStrings(t *testing.T) {
	for _, query := range []string{"?q=%zz", "?q=%", "?q=a;b=c"} {
		t.Run(query, func(t *testing.T) {
			rec := do(accountsAgingEngine(handlermocks.NewMockAccountsAgingService(t)), http.MethodGet, accountsAgingPath+query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestAccountsAgingListMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation error", model.InvalidInput(model.FieldError{Field: "as_of", Message: "must not be in the future"}), 422, "invalid_input"},
		{"wrapped validation error", fmt.Errorf("list: %w", model.InvalidInput(model.FieldError{Field: "q", Message: "x"})), 422, "invalid_input"},
		{"storage error", errors.New("pq: password authentication failed"), 500, "internal_error"},
		{"deadline exceeded", fmt.Errorf("list accounts aging: %w", context.DeadlineExceeded), 504, "timeout"},
		{"client went away", fmt.Errorf("list accounts aging: %w", context.Canceled), 499, "client_closed_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := handlermocks.NewMockAccountsAgingService(t)
			svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, tt.err).Once()

			rec := do(accountsAgingEngine(svc), http.MethodGet, accountsAgingPath, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
			require.NotContains(t, rec.Body.String(), "password", "internal error text must not leak")
		})
	}
}

func TestAccountsAgingListPassesTheRequestContextToTheService(t *testing.T) {
	type key struct{}
	svc := handlermocks.NewMockAccountsAgingService(t)
	svc.EXPECT().List(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(key{}) == "marker" }), mock.Anything).
		Return(emptyAgingResult(), nil).Once()

	req := httptest.NewRequest(http.MethodGet, accountsAgingPath, nil).
		WithContext(context.WithValue(context.Background(), key{}, "marker"))
	rec := httptest.NewRecorder()
	accountsAgingEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestAccountsAgingResponsesAreNeverCached(t *testing.T) {
	setRequestID := func(c *gin.Context) { c.Header("X-Request-ID", "rid-7"); c.Next() }

	t.Run("success", func(t *testing.T) {
		svc := handlermocks.NewMockAccountsAgingService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(emptyAgingResult(), nil).Once()

		rec := do(accountsAgingEngine(svc, setRequestID), http.MethodGet, accountsAgingPath, "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		require.Equal(t, "rid-7", rec.Header().Get("X-Request-ID"))
	})

	t.Run("validation error", func(t *testing.T) {
		rec := do(accountsAgingEngine(handlermocks.NewMockAccountsAgingService(t), setRequestID), http.MethodGet, accountsAgingPath+"?nope=1", "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "rid-7", decodeError(t, rec).RequestID)
	})

	t.Run("filters", func(t *testing.T) {
		svc := handlermocks.NewMockAccountsAgingService(t)
		svc.EXPECT().FilterOptions(mock.Anything).Return(&service.AgingFilterOptions{}, nil).Once()

		rec := do(accountsAgingEngine(svc), http.MethodGet, accountsAgingPath+"/filters", "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})
}

func TestAccountsAgingFiltersResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockAccountsAgingService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.AgingFilterOptions{
		ServiceTypes: []model.ServiceType{{ID: 2, Name: "Bi-Monthly Service"}, {ID: 1, Name: "Pool Cleaning"}},
	}, nil).Once()

	rec := do(accountsAgingEngine(svc), http.MethodGet, accountsAgingPath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"days": [30, 60, 90],
		"service_types": [{"id": 2, "name": "Bi-Monthly Service"}, {"id": 1, "name": "Pool Cleaning"}],
		"sort_keys": ["customer", "total", "d0_30", "d31_60", "d61_90", "d90_plus", "available_credit"],
		"default_sort": {"key": "customer", "order": "asc"},
		"limits": {"default": 50, "max": 200}
	}`, rec.Body.String())
}

func TestAccountsAgingFiltersWithNoServiceTypesIsAnEmptyList(t *testing.T) {
	svc := handlermocks.NewMockAccountsAgingService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.AgingFilterOptions{}, nil).Once()

	rec := do(accountsAgingEngine(svc), http.MethodGet, accountsAgingPath+"/filters", "")

	require.Contains(t, rec.Body.String(), `"service_types":[]`)
}

func TestAccountsAgingFiltersMapsServiceErrors(t *testing.T) {
	svc := handlermocks.NewMockAccountsAgingService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(nil, errors.New("pq: down")).Once()

	rec := do(accountsAgingEngine(svc), http.MethodGet, accountsAgingPath+"/filters", "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "internal_error", decodeError(t, rec).Code)
}

func TestAccountsAgingOnlyGetIsAllowed(t *testing.T) {
	for _, path := range []string{accountsAgingPath, accountsAgingPath + "/filters"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			t.Run(method+" "+path, func(t *testing.T) {
				rec := do(accountsAgingEngine(handlermocks.NewMockAccountsAgingService(t)), method, path, "")
				require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
			})
		}
	}
}

func TestAccountsAgingLongUnknownParameterNameIsTruncated(t *testing.T) {
	rec := do(accountsAgingEngine(handlermocks.NewMockAccountsAgingService(t)), http.MethodGet, accountsAgingPath+"?"+strings.Repeat("x", 5000)+"=1", "")

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Equal(t, strings.Repeat("x", 64)+"...", decodeError(t, rec).Details[0].Field)
}
