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

const serviceLookupPath = "/api/v1/reports/service-lookup"

func serviceLookupEngine(svc handler.ServiceLookupService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewServiceLookupHandler(svc, logger)},
	})
}

func emptyServiceLookupResult() *service.ListServiceLookupResult {
	return &service.ListServiceLookupResult{Items: []model.ServiceLookupRow{}, Limit: 50}
}

// serviceLookupParamsFor performs GET /service-lookup?query against a mocked service and returns the params it saw.
func serviceLookupParamsFor(t *testing.T, query string) service.ListServiceLookupParams {
	t.Helper()

	svc := handlermocks.NewMockServiceLookupService(t)
	var got service.ListServiceLookupParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListServiceLookupParams) { got = p }).
		Return(emptyServiceLookupResult(), nil).Once()

	rec := do(serviceLookupEngine(svc), http.MethodGet, serviceLookupPath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

func TestServiceLookupListResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockServiceLookupService(t)
	scheduled := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListServiceLookupResult{
		Items: []model.ServiceLookupRow{
			{
				ID: 98, CustomerID: 12, CustomerName: "Wayne Enterprises", Phone: strPtr("555-0100"),
				ServiceTypeID: 3, ServiceType: "Bi-Monthly Service", ScheduledAt: &scheduled,
				Address:      model.LookupAddress{Line1: "12 Elm St", City: "Austin", State: "TX", Zip: "78701", County: "Travis County"},
				InvoiceValue: 6500, WithoutTax: 6000, Status: model.JobConfirmed,
				AssignedToID: int64Ptr(2), AssignedTo: strPtr("thon nguyen"),
				Tags: []model.JobTag{{ID: 1, Name: "VIP"}, {ID: 4, Name: "Follow Up"}},
			},
			{
				ID: 99, CustomerID: 13, CustomerName: "Bukayo Saka", ServiceTypeID: 3, ServiceType: "Bi-Monthly Service",
				InvoiceValue: 0, WithoutTax: 0, Status: model.JobUnconfirmed,
			},
		},
		Limit: 50, HasMore: true, NextCursor: "opaque-cursor",
		Summary: &model.ServiceLookupSummary{
			Count: 120, InvoiceValue: 780000, WithoutTax: 720000,
			Tabs: model.ServiceLookupTabCounts{Active: 120, WorkPool: 8, Deleted: 3},
		},
		From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(serviceLookupEngine(svc), http.MethodGet, serviceLookupPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{"id": 98, "customer": {"id": 12, "name": "Wayne Enterprises"}, "phone": "555-0100",
			 "service_type": {"id": 3, "name": "Bi-Monthly Service"}, "scheduled_at": "2026-10-15T08:00:00.000000Z",
			 "address": {"line1": "12 Elm St", "line2": "", "city": "Austin", "state": "TX", "zip": "78701", "county": "Travis County"},
			 "invoice_value": "65.00", "without_tax": "60.00", "status": "confirmed",
			 "assigned_to": {"id": 2, "name": "thon nguyen"},
			 "tags": [{"id": 1, "name": "VIP"}, {"id": 4, "name": "Follow Up"}]},
			{"id": 99, "customer": {"id": 13, "name": "Bukayo Saka"}, "phone": null,
			 "service_type": {"id": 3, "name": "Bi-Monthly Service"}, "scheduled_at": null,
			 "address": {"line1": "", "line2": "", "city": "", "state": "", "zip": "", "county": ""},
			 "invoice_value": "0.00", "without_tax": "0.00", "status": "unconfirmed",
			 "assigned_to": null, "tags": []}
		],
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"summary": {"count": 120, "invoice_value": "7800.00", "without_tax": "7200.00",
			"tabs": {"active": 120, "work_pool": 8, "deleted": 3}},
		"range": {"from": "2026-10-01", "to": null}
	}`, rec.Body.String())
}

func TestServiceLookupListResponseForAnEmptyLaterPage(t *testing.T) {
	svc := handlermocks.NewMockServiceLookupService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(emptyServiceLookupResult(), nil).Once()

	rec := do(serviceLookupEngine(svc), http.MethodGet, serviceLookupPath+"?cursor=x", "")

	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 50, "has_more": false, "next_cursor": null},
		"summary": null,
		"range": {"from": null, "to": null}
	}`, rec.Body.String())
}

func TestServiceLookupListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListServiceLookupParams
	}{
		{"no query", "", service.ListServiceLookupParams{}},
		{"every single-value parameter", "?tab=work_pool&from=2026-10-01&to=2026-10-31&q=elm&sort=city&order=desc&limit=25&cursor=abc",
			service.ListServiceLookupParams{Tab: "work_pool", From: "2026-10-01", To: "2026-10-31", Search: "elm",
				Sort: "city", Order: "desc", Limit: "25", Cursor: "abc"}},
		{"repeated key", "?status=confirmed&status=completed", service.ListServiceLookupParams{Status: []string{"confirmed", "completed"}}},
		{"comma separated", "?status=confirmed,completed", service.ListServiceLookupParams{Status: []string{"confirmed", "completed"}}},
		{"ids repeated and comma separated", "?service_type=1,2&service_type=3&assigned_to=4&tag=5,6",
			service.ListServiceLookupParams{ServiceType: []string{"1", "2", "3"}, AssignedTo: []string{"4"}, Tag: []string{"5", "6"}}},
		{"whitespace and blanks are dropped", "?status=%20confirmed%20,,completed,&status=", service.ListServiceLookupParams{Status: []string{"confirmed", "completed"}}},
		{"only blanks leaves the lists empty", "?status=&tag=,&assigned_to=", service.ListServiceLookupParams{}},
		{"empty single values stay empty strings", "?tab=&from=&limit=", service.ListServiceLookupParams{}},
		{"sql text is just text", "?q=%27%3B+DROP+TABLE+jobs%3B--", service.ListServiceLookupParams{Search: "'; DROP TABLE jobs;--"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, serviceLookupParamsFor(t, tt.query))
		})
	}
}

func TestServiceLookupListRejectsUnsupportedAndDuplicatedParameters(t *testing.T) {
	unsupported := func(name string) []dto.ErrorDetail {
		return []dto.ErrorDetail{{Field: name, Message: "is not a supported parameter"}}
	}
	tests := []struct {
		name  string
		query string
		want  []dto.ErrorDetail
	}{
		{"state is not a filter", "?state=TX", unsupported("state")},
		{"a typo in a parameter name", "?tags=1", unsupported("tags")},
		{"service_type_id is the jobs report's name", "?service_type_id=1", unsupported("service_type_id")},
		{"tab twice", "?tab=active&tab=deleted", []dto.ErrorDetail{{Field: "tab", Message: "must be given only once"}}},
		{"problems are listed in a stable order", "?zzz=1&aaa=2&limit=1&limit=2", []dto.ErrorDetail{
			{Field: "aaa", Message: "is not a supported parameter"},
			{Field: "limit", Message: "must be given only once"},
			{Field: "zzz", Message: "is not a supported parameter"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(serviceLookupEngine(handlermocks.NewMockServiceLookupService(t)), http.MethodGet, serviceLookupPath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, tt.want, body.Details)
		})
	}
}

func TestServiceLookupListRejectsMalformedQueryStrings(t *testing.T) {
	for _, query := range []string{"?q=%zz", "?q=%", "?q=a;b=c"} {
		t.Run(query, func(t *testing.T) {
			rec := do(serviceLookupEngine(handlermocks.NewMockServiceLookupService(t)), http.MethodGet, serviceLookupPath+query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestServiceLookupListMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation error", model.InvalidInput(model.FieldError{Field: "tab", Message: "must be one of: active"}), 422, "invalid_input"},
		{"wrapped validation error", fmt.Errorf("list: %w", model.InvalidInput(model.FieldError{Field: "q", Message: "x"})), 422, "invalid_input"},
		{"storage error", errors.New("pq: password authentication failed"), 500, "internal_error"},
		{"deadline exceeded", fmt.Errorf("list service lookup: %w", context.DeadlineExceeded), 504, "timeout"},
		{"client went away", fmt.Errorf("list service lookup: %w", context.Canceled), 499, "client_closed_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := handlermocks.NewMockServiceLookupService(t)
			svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, tt.err).Once()

			rec := do(serviceLookupEngine(svc), http.MethodGet, serviceLookupPath, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
			require.NotContains(t, rec.Body.String(), "password", "internal error text must not leak")
		})
	}
}

func TestServiceLookupListPassesTheRequestContextToTheService(t *testing.T) {
	type key struct{}
	svc := handlermocks.NewMockServiceLookupService(t)
	svc.EXPECT().List(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(key{}) == "marker" }), mock.Anything).
		Return(emptyServiceLookupResult(), nil).Once()

	req := httptest.NewRequest(http.MethodGet, serviceLookupPath, nil).
		WithContext(context.WithValue(context.Background(), key{}, "marker"))
	rec := httptest.NewRecorder()
	serviceLookupEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestServiceLookupResponsesAreNeverCached(t *testing.T) {
	setRequestID := func(c *gin.Context) { c.Header("X-Request-ID", "rid-7"); c.Next() }

	t.Run("success", func(t *testing.T) {
		svc := handlermocks.NewMockServiceLookupService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(emptyServiceLookupResult(), nil).Once()

		rec := do(serviceLookupEngine(svc, setRequestID), http.MethodGet, serviceLookupPath, "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		require.Equal(t, "rid-7", rec.Header().Get("X-Request-ID"))
	})

	t.Run("validation error", func(t *testing.T) {
		rec := do(serviceLookupEngine(handlermocks.NewMockServiceLookupService(t), setRequestID), http.MethodGet, serviceLookupPath+"?nope=1", "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "rid-7", decodeError(t, rec).RequestID)
	})

	t.Run("filters", func(t *testing.T) {
		svc := handlermocks.NewMockServiceLookupService(t)
		svc.EXPECT().FilterOptions(mock.Anything).Return(&service.ServiceLookupFilterOptions{}, nil).Once()

		rec := do(serviceLookupEngine(svc), http.MethodGet, serviceLookupPath+"/filters", "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})
}

func TestServiceLookupFiltersResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockServiceLookupService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.ServiceLookupFilterOptions{
		ServiceTypes: []model.ServiceType{{ID: 2, Name: "Bi-Monthly Service"}},
		Staff:        []model.StaffOption{{ID: 1, Name: "Thọ Nguyễn Quang"}},
		Tags:         []model.JobTag{{ID: 4, Name: "Follow Up"}},
	}, nil).Once()

	rec := do(serviceLookupEngine(svc), http.MethodGet, serviceLookupPath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"tabs": ["active", "work_pool", "deleted"],
		"statuses": ["unconfirmed", "confirmed", "completed", "cancelled"],
		"service_types": [{"id": 2, "name": "Bi-Monthly Service"}],
		"staff": [{"id": 1, "name": "Thọ Nguyễn Quang"}],
		"tags": [{"id": 4, "name": "Follow Up"}],
		"sort_keys": ["customer", "service_type", "scheduled_at", "address", "city", "zip", "county", "invoice_value", "without_tax", "status", "assigned_to"],
		"default_sort": {"key": "scheduled_at", "order": "asc"},
		"limits": {"default": 50, "max": 200}
	}`, rec.Body.String())
}

func TestServiceLookupFiltersWithNothingIsEmptyLists(t *testing.T) {
	svc := handlermocks.NewMockServiceLookupService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.ServiceLookupFilterOptions{}, nil).Once()

	rec := do(serviceLookupEngine(svc), http.MethodGet, serviceLookupPath+"/filters", "")

	for _, list := range []string{"service_types", "staff", "tags"} {
		require.Contains(t, rec.Body.String(), `"`+list+`":[]`)
	}
}

func TestServiceLookupFiltersMapsServiceErrors(t *testing.T) {
	svc := handlermocks.NewMockServiceLookupService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(nil, errors.New("pq: down")).Once()

	rec := do(serviceLookupEngine(svc), http.MethodGet, serviceLookupPath+"/filters", "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "internal_error", decodeError(t, rec).Code)
}

func TestServiceLookupOnlyGetIsAllowed(t *testing.T) {
	for _, path := range []string{serviceLookupPath, serviceLookupPath + "/filters"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			t.Run(method+" "+path, func(t *testing.T) {
				rec := do(serviceLookupEngine(handlermocks.NewMockServiceLookupService(t)), method, path, "")
				require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
			})
		}
	}
}
