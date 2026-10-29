package handler_test

import (
	"context"
	"errors"
	"fmt"
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

const (
	noActiveJobsCustomersPath = "/api/v1/reports/customers-without-active-jobs"
	noActiveJobsLocationsPath = "/api/v1/reports/locations-without-active-jobs"
)

var noActiveJobsPaths = map[model.NoActiveJobsGrain]string{
	model.NoActiveJobsCustomers: noActiveJobsCustomersPath,
	model.NoActiveJobsLocations: noActiveJobsLocationsPath,
}

func noActiveJobsEngine(svc handler.NoActiveJobsService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewNoActiveJobsHandler(svc, logger)},
	})
}

func emptyNoActiveJobsResult() *service.ListNoActiveJobsResult {
	return &service.ListNoActiveJobsResult{Items: []model.NoActiveJobsRow{}, Limit: 50}
}

// noActiveJobsParamsFor performs GET path?query against a mocked service and returns the grain and params it saw.
func noActiveJobsParamsFor(t *testing.T, path, query string) (model.NoActiveJobsGrain, service.ListNoActiveJobsParams) {
	t.Helper()

	svc := handlermocks.NewMockNoActiveJobsService(t)
	var (
		gotGrain  model.NoActiveJobsGrain
		gotParams service.ListNoActiveJobsParams
	)
	svc.EXPECT().List(mock.Anything, mock.Anything, mock.Anything).
		Run(func(_ context.Context, g model.NoActiveJobsGrain, p service.ListNoActiveJobsParams) {
			gotGrain, gotParams = g, p
		}).
		Return(emptyNoActiveJobsResult(), nil).Once()

	rec := do(noActiveJobsEngine(svc), http.MethodGet, path+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return gotGrain, gotParams
}

func TestNoActiveJobsCustomersResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockNoActiveJobsService(t)
	at := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	svc.EXPECT().List(mock.Anything, model.NoActiveJobsCustomers, mock.Anything).Return(&service.ListNoActiveJobsResult{
		Items: []model.NoActiveJobsRow{
			{
				ID: 12, CustomerID: 12, Status: model.CustomerStatusActive, AccountNumber: "5001", CustomerName: "William Saliba",
				Phone:       strPtr("555-0100"),
				LastService: &model.LastService{ServiceType: "Bi-Monthly Service", JobStatus: "completed", At: at},
			},
			{ID: 13, CustomerID: 13, Status: model.CustomerStatusLead, AccountNumber: "63730", CustomerName: "QA customer"},
		},
		Limit: 50, HasMore: true, NextCursor: "opaque-cursor", Summary: &model.NoActiveJobsSummary{Count: 12},
	}, nil).Once()

	rec := do(noActiveJobsEngine(svc), http.MethodGet, noActiveJobsCustomersPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{"id": 12, "status": "active", "account_number": "5001", "customer": "William Saliba", "phone": "555-0100",
			 "last_service": {"service_type": "Bi-Monthly Service", "job_status": "completed", "date": "2026-10-15"}},
			{"id": 13, "status": "lead", "account_number": "63730", "customer": "QA customer", "phone": null,
			 "last_service": null}
		],
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"summary": {"count": 12}
	}`, rec.Body.String())
}

func TestNoActiveJobsLocationsResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockNoActiveJobsService(t)
	svc.EXPECT().List(mock.Anything, model.NoActiveJobsLocations, mock.Anything).Return(&service.ListNoActiveJobsResult{
		Items: []model.NoActiveJobsRow{{
			ID: 31, CustomerID: 12, Status: model.CustomerStatusInactive, AccountNumber: "63717", CustomerName: "Wayne",
			Address: &model.ServiceAddress{Line1: "12 Elm St", City: "Austin", Zip: "78701"},
		}},
		Limit: 50,
	}, nil).Once()

	rec := do(noActiveJobsEngine(svc), http.MethodGet, noActiveJobsLocationsPath+"?cursor=x", "")

	require.JSONEq(t, `{
		"data": [
			{"id": 31, "customer_id": 12, "status": "inactive", "account_number": "63717", "customer": "Wayne", "phone": null,
			 "address": {"line1": "12 Elm St", "line2": "", "city": "Austin", "zip": "78701"}, "last_service": null}
		],
		"page": {"limit": 50, "has_more": false, "next_cursor": null},
		"summary": null
	}`, rec.Body.String())
}

func TestNoActiveJobsEachEndpointAsksForItsOwnGrain(t *testing.T) {
	for grain, path := range noActiveJobsPaths {
		t.Run(string(grain), func(t *testing.T) {
			got, _ := noActiveJobsParamsFor(t, path, "")
			require.Equal(t, grain, got)
		})
	}
}

func TestNoActiveJobsListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListNoActiveJobsParams
	}{
		{"no query", "", service.ListNoActiveJobsParams{}},
		{"every single-value parameter", "?q=saka&sort=status&order=desc&limit=25&cursor=abc",
			service.ListNoActiveJobsParams{Search: "saka", Sort: "status", Order: "desc", Limit: "25", Cursor: "abc"}},
		{"repeated key", "?status=lead&status=inactive", service.ListNoActiveJobsParams{Status: []string{"lead", "inactive"}}},
		{"comma separated", "?status=lead,inactive", service.ListNoActiveJobsParams{Status: []string{"lead", "inactive"}}},
		{"blanks are dropped", "?status=,&status=", service.ListNoActiveJobsParams{}},
		{"sql text is just text", "?q=%27%3B+DROP+TABLE+customers%3B--", service.ListNoActiveJobsParams{Search: "'; DROP TABLE customers;--"}},
	}

	for _, path := range noActiveJobsPaths {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, got := noActiveJobsParamsFor(t, path, tt.query)
				require.Equal(t, tt.want, got)
			})
		}
	}
}

func TestNoActiveJobsListRejectsUnsupportedAndDuplicatedParameters(t *testing.T) {
	unsupported := func(name string) []dto.ErrorDetail {
		return []dto.ErrorDetail{{Field: name, Message: "is not a supported parameter"}}
	}
	tests := []struct {
		name  string
		query string
		want  []dto.ErrorDetail
	}{
		{"a date range is not offered", "?from=2026-10-01", unsupported("from")},
		{"a typo in a parameter name", "?statuses=lead", unsupported("statuses")},
		{"status twice is fine but sort twice is not", "?sort=status&sort=city", []dto.ErrorDetail{{Field: "sort", Message: "must be given only once"}}},
	}

	for _, path := range noActiveJobsPaths {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				rec := do(noActiveJobsEngine(handlermocks.NewMockNoActiveJobsService(t)), http.MethodGet, path+tt.query, "")

				require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
				body := decodeError(t, rec)
				require.Equal(t, "invalid_input", body.Code)
				require.Equal(t, tt.want, body.Details)
			})
		}
	}
}

func TestNoActiveJobsListRejectsMalformedQueryStrings(t *testing.T) {
	for _, path := range noActiveJobsPaths {
		for _, query := range []string{"?q=%zz", "?q=%", "?q=a;b=c"} {
			t.Run(query, func(t *testing.T) {
				rec := do(noActiveJobsEngine(handlermocks.NewMockNoActiveJobsService(t)), http.MethodGet, path+query, "")

				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.Equal(t, "invalid_request", decodeError(t, rec).Code)
			})
		}
	}
}

func TestNoActiveJobsListMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation error", model.InvalidInput(model.FieldError{Field: "status", Message: "must be one of: active"}), 422, "invalid_input"},
		{"wrapped validation error", fmt.Errorf("list: %w", model.InvalidInput(model.FieldError{Field: "q", Message: "x"})), 422, "invalid_input"},
		{"storage error", errors.New("pq: password authentication failed"), 500, "internal_error"},
		{"deadline exceeded", fmt.Errorf("list customers without active jobs: %w", context.DeadlineExceeded), 504, "timeout"},
		{"client went away", fmt.Errorf("list customers without active jobs: %w", context.Canceled), 499, "client_closed_request"},
	}

	for _, path := range noActiveJobsPaths {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				svc := handlermocks.NewMockNoActiveJobsService(t)
				svc.EXPECT().List(mock.Anything, mock.Anything, mock.Anything).Return(nil, tt.err).Once()

				rec := do(noActiveJobsEngine(svc), http.MethodGet, path, "")

				require.Equal(t, tt.wantStatus, rec.Code)
				require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
				require.NotContains(t, rec.Body.String(), "password", "internal error text must not leak")
			})
		}
	}
}

func TestNoActiveJobsListPassesTheRequestContextToTheService(t *testing.T) {
	type key struct{}
	svc := handlermocks.NewMockNoActiveJobsService(t)
	svc.EXPECT().List(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(key{}) == "marker" }), mock.Anything, mock.Anything).
		Return(emptyNoActiveJobsResult(), nil).Once()

	req := httptest.NewRequest(http.MethodGet, noActiveJobsCustomersPath, nil).
		WithContext(context.WithValue(context.Background(), key{}, "marker"))
	rec := httptest.NewRecorder()
	noActiveJobsEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestNoActiveJobsResponsesAreNeverCached(t *testing.T) {
	setRequestID := func(c *gin.Context) { c.Header("X-Request-ID", "rid-7"); c.Next() }

	t.Run("success", func(t *testing.T) {
		svc := handlermocks.NewMockNoActiveJobsService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything, mock.Anything).Return(emptyNoActiveJobsResult(), nil).Once()

		rec := do(noActiveJobsEngine(svc, setRequestID), http.MethodGet, noActiveJobsCustomersPath, "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		require.Equal(t, "rid-7", rec.Header().Get("X-Request-ID"))
	})

	t.Run("validation error", func(t *testing.T) {
		rec := do(noActiveJobsEngine(handlermocks.NewMockNoActiveJobsService(t), setRequestID), http.MethodGet, noActiveJobsLocationsPath+"?nope=1", "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "rid-7", decodeError(t, rec).RequestID)
	})

	t.Run("filters", func(t *testing.T) {
		rec := do(noActiveJobsEngine(handlermocks.NewMockNoActiveJobsService(t)), http.MethodGet, noActiveJobsCustomersPath+"/filters", "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})
}

func TestNoActiveJobsFiltersResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockNoActiveJobsService(t) // fixed lists: no service call

	customers := do(noActiveJobsEngine(svc), http.MethodGet, noActiveJobsCustomersPath+"/filters", "")
	locations := do(noActiveJobsEngine(svc), http.MethodGet, noActiveJobsLocationsPath+"/filters", "")

	require.Equal(t, http.StatusOK, customers.Code)
	require.JSONEq(t, `{
		"statuses": ["active", "inactive", "lead", "archived"],
		"sort_keys": ["status", "account_number", "customer", "last_service_type", "job_status", "last_service_date"],
		"default_sort": {"key": "customer", "order": "asc"},
		"limits": {"default": 50, "max": 200}
	}`, customers.Body.String())
	require.JSONEq(t, `{
		"statuses": ["active", "inactive", "lead", "archived"],
		"sort_keys": ["status", "account_number", "customer", "address", "city", "zip", "last_service_type", "job_status", "last_service_date"],
		"default_sort": {"key": "customer", "order": "asc"},
		"limits": {"default": 50, "max": 200}
	}`, locations.Body.String())
}

func TestNoActiveJobsOnlyGetIsAllowed(t *testing.T) {
	for _, path := range []string{
		noActiveJobsCustomersPath, noActiveJobsCustomersPath + "/filters",
		noActiveJobsLocationsPath, noActiveJobsLocationsPath + "/filters",
	} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			t.Run(method+" "+path, func(t *testing.T) {
				rec := do(noActiveJobsEngine(handlermocks.NewMockNoActiveJobsService(t)), method, path, "")
				require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
			})
		}
	}
}
