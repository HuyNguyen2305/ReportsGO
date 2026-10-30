package handler_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/dto"
	"reportsgo/internal/handler"
	"reportsgo/internal/model"
	"reportsgo/internal/service"
	handlermocks "reportsgo/tests/mocks/handler"
)

const salesBreakdownPath = "/api/v1/reports/sales-breakdown"

func salesBreakdownEngine(svc handler.SalesBreakdownService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewSalesBreakdownHandler(svc, logger)},
	})
}

func emptySalesBreakdownResult() *service.ListSalesBreakdownResult {
	return &service.ListSalesBreakdownResult{Items: []model.SalesBreakdownRow{}, Limit: 50, Year: 2026,
		Type: model.SalesBreakdownRevenue, By: model.SalesBreakdownByClient}
}

// salesBreakdownParamsFor performs GET /sales-breakdown?query against a mocked service and returns the params it saw.
func salesBreakdownParamsFor(t *testing.T, query string) service.ListSalesBreakdownParams {
	t.Helper()

	svc := handlermocks.NewMockSalesBreakdownService(t)
	var got service.ListSalesBreakdownParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListSalesBreakdownParams) { got = p }).
		Return(emptySalesBreakdownResult(), nil).Once()

	rec := do(salesBreakdownEngine(svc), http.MethodGet, salesBreakdownPath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

func amounts(oct, total model.Money) model.SalesBreakdownAmounts {
	var a model.SalesBreakdownAmounts
	a.Months[9] = oct
	a.Total = total
	return a
}

func TestSalesBreakdownListResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockSalesBreakdownService(t)
	unassigned := amounts(65880, 66880)
	unassigned.Months[8] = 1000
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListSalesBreakdownResult{
		Items: []model.SalesBreakdownRow{
			{ID: 1, Name: "Facebook", SalesBreakdownAmounts: amounts(15226, 15226)},
		},
		Limit: 50, HasMore: true, NextCursor: "opaque-cursor",
		Unassigned: &unassigned,
		Summary:    &model.SalesBreakdownSummary{Count: 1, SalesBreakdownAmounts: amounts(81106, 82106)},
		Type:       model.SalesBreakdownRevenue, By: model.SalesBreakdownBySource, Year: 2026,
	}, nil).Once()

	rec := do(salesBreakdownEngine(svc), http.MethodGet, salesBreakdownPath+"?type=revenue&by=source", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{"id": 1, "name": "Facebook", "account_number": null,
			 "months": ["0.00","0.00","0.00","0.00","0.00","0.00","0.00","0.00","0.00","152.26","0.00","0.00"], "total": "152.26"}
		],
		"unassigned": {"label": "Revenue w/o a source",
			"months": ["0.00","0.00","0.00","0.00","0.00","0.00","0.00","0.00","10.00","658.80","0.00","0.00"], "total": "668.80"},
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"summary": {"count": 1, "months": ["0.00","0.00","0.00","0.00","0.00","0.00","0.00","0.00","0.00","811.06","0.00","0.00"], "total": "821.06"},
		"year": 2026, "type": "revenue", "by": "source"
	}`, rec.Body.String())
}

func TestSalesBreakdownClientRowsCarryTheAccountNumber(t *testing.T) {
	svc := handlermocks.NewMockSalesBreakdownService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListSalesBreakdownResult{
		Items: []model.SalesBreakdownRow{{ID: 11, Name: "William Saliba", AccountNumber: strPtr("5001")}},
		Limit: 50, Type: model.SalesBreakdownForecast, By: model.SalesBreakdownByClient, Year: 2026,
	}, nil).Once()

	rec := do(salesBreakdownEngine(svc), http.MethodGet, salesBreakdownPath+"?type=forecast&by=client&cursor=x", "")

	require.JSONEq(t, `{
		"data": [{"id": 11, "name": "William Saliba", "account_number": "5001",
			"months": ["0.00","0.00","0.00","0.00","0.00","0.00","0.00","0.00","0.00","0.00","0.00","0.00"], "total": "0.00"}],
		"unassigned": null,
		"page": {"limit": 50, "has_more": false, "next_cursor": null},
		"summary": null,
		"year": 2026, "type": "forecast", "by": "client"
	}`, rec.Body.String())
}

func TestSalesBreakdownUnassignedLabels(t *testing.T) {
	tests := []struct {
		ty    model.SalesBreakdownType
		by    model.SalesBreakdownBy
		label string
	}{
		{model.SalesBreakdownForecast, model.SalesBreakdownByService, "Sales forecast w/o services"},
		{model.SalesBreakdownRevenue, model.SalesBreakdownByService, "Additional invoice revenue"},
		{model.SalesBreakdownForecast, model.SalesBreakdownBySource, "Sales forecast w/o source"},
		{model.SalesBreakdownRevenue, model.SalesBreakdownBySource, "Revenue w/o a source"},
		{model.SalesBreakdownForecast, model.SalesBreakdownByStaff, "Other (no job or nobody assigned)"},
		{model.SalesBreakdownRevenue, model.SalesBreakdownByItem, "Revenue w/o items"},
		{model.SalesBreakdownForecast, model.SalesBreakdownByItem, "Sales forecast w/o items"},
		{model.SalesBreakdownRevenue, model.SalesBreakdownByTag, "Untagged"},
	}
	for _, tt := range tests {
		t.Run(string(tt.ty)+" "+string(tt.by), func(t *testing.T) {
			svc := handlermocks.NewMockSalesBreakdownService(t)
			unassigned := amounts(100, 100)
			svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListSalesBreakdownResult{
				Items: []model.SalesBreakdownRow{}, Limit: 50, Unassigned: &unassigned, Type: tt.ty, By: tt.by, Year: 2026,
			}, nil).Once()

			rec := do(salesBreakdownEngine(svc), http.MethodGet, salesBreakdownPath, "")

			require.Contains(t, rec.Body.String(), `"label":"`+tt.label+`"`)
		})
	}
}

func TestSalesBreakdownListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListSalesBreakdownParams
	}{
		{"no query", "", service.ListSalesBreakdownParams{}},
		{"every single-value parameter", "?type=revenue&by=client&year=2026&measure=collected&q=saka&sort=oct&order=desc&limit=25&cursor=abc",
			service.ListSalesBreakdownParams{Type: "revenue", By: "client", Year: "2026", Measure: "collected", Search: "saka",
				Sort: "oct", Order: "desc", Limit: "25", Cursor: "abc"}},
		{"repeated and comma separated", "?group=1,2&group=3&method=cash,card",
			service.ListSalesBreakdownParams{Group: []string{"1", "2", "3"}, Method: []string{"cash", "card"}}},
		{"blanks are dropped", "?group=,&method=", service.ListSalesBreakdownParams{}},
		{"sql text is just text", "?q=%27%3B+DROP+TABLE+payments%3B--", service.ListSalesBreakdownParams{Search: "'; DROP TABLE payments;--"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, salesBreakdownParamsFor(t, tt.query))
		})
	}
}

func TestSalesBreakdownListRejectsUnsupportedAndDuplicatedParameters(t *testing.T) {
	unsupported := func(name string) []dto.ErrorDetail {
		return []dto.ErrorDetail{{Field: name, Message: "is not a supported parameter"}}
	}
	tests := []struct {
		name  string
		query string
		want  []dto.ErrorDetail
	}{
		{"a date range is not offered", "?from=2026-01-01", unsupported("from")},
		{"parent_type is called type", "?parent_type=revenue", unsupported("parent_type")},
		{"type twice", "?type=revenue&type=forecast", []dto.ErrorDetail{{Field: "type", Message: "must be given only once"}}},
		{"year twice", "?year=2025&year=2026", []dto.ErrorDetail{{Field: "year", Message: "must be given only once"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(salesBreakdownEngine(handlermocks.NewMockSalesBreakdownService(t)), http.MethodGet, salesBreakdownPath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, tt.want, body.Details)
		})
	}
}

func TestSalesBreakdownListRejectsMalformedQueryStrings(t *testing.T) {
	for _, query := range []string{"?q=%zz", "?q=%", "?q=a;b=c"} {
		t.Run(query, func(t *testing.T) {
			rec := do(salesBreakdownEngine(handlermocks.NewMockSalesBreakdownService(t)), http.MethodGet, salesBreakdownPath+query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestSalesBreakdownListMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation error", model.InvalidInput(model.FieldError{Field: "type", Message: "is required"}), 422, "invalid_input"},
		{"storage error", errors.New("pq: password authentication failed"), 500, "internal_error"},
		{"deadline exceeded", fmt.Errorf("list sales breakdown: %w", context.DeadlineExceeded), 504, "timeout"},
		{"client went away", fmt.Errorf("list sales breakdown: %w", context.Canceled), 499, "client_closed_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := handlermocks.NewMockSalesBreakdownService(t)
			svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, tt.err).Once()

			rec := do(salesBreakdownEngine(svc), http.MethodGet, salesBreakdownPath, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
			require.NotContains(t, rec.Body.String(), "password", "internal error text must not leak")
		})
	}
}

func TestSalesBreakdownListPassesTheRequestContextToTheService(t *testing.T) {
	type key struct{}
	svc := handlermocks.NewMockSalesBreakdownService(t)
	svc.EXPECT().List(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(key{}) == "marker" }), mock.Anything).
		Return(emptySalesBreakdownResult(), nil).Once()

	req := httptest.NewRequest(http.MethodGet, salesBreakdownPath, nil).
		WithContext(context.WithValue(context.Background(), key{}, "marker"))
	rec := httptest.NewRecorder()
	salesBreakdownEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestSalesBreakdownResponsesAreNeverCached(t *testing.T) {
	setRequestID := func(c *gin.Context) { c.Header("X-Request-ID", "rid-7"); c.Next() }

	t.Run("success", func(t *testing.T) {
		svc := handlermocks.NewMockSalesBreakdownService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(emptySalesBreakdownResult(), nil).Once()

		rec := do(salesBreakdownEngine(svc, setRequestID), http.MethodGet, salesBreakdownPath, "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "rid-7", rec.Header().Get("X-Request-ID"))
	})

	t.Run("validation error", func(t *testing.T) {
		rec := do(salesBreakdownEngine(handlermocks.NewMockSalesBreakdownService(t), setRequestID), http.MethodGet, salesBreakdownPath+"?nope=1", "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "rid-7", decodeError(t, rec).RequestID)
	})

	t.Run("filters", func(t *testing.T) {
		svc := handlermocks.NewMockSalesBreakdownService(t)
		svc.EXPECT().FilterOptions(mock.Anything).Return(&model.SalesBreakdownGroupOptions{}, nil).Once()

		rec := do(salesBreakdownEngine(svc), http.MethodGet, salesBreakdownPath+"/filters", "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})
}

func TestSalesBreakdownFiltersResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockSalesBreakdownService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&model.SalesBreakdownGroupOptions{
		ServiceTypes: []model.ServiceType{{ID: 1, Name: "Bi-Monthly Service"}},
		Items:        []model.ItemOption{{ID: 7, Name: "Travel Fee"}},
		Staff:        []model.StaffOption{{ID: 2, Name: "thon nguyen"}},
		Sources:      []model.LeadSource{{ID: 3, Name: "Facebook"}},
		Tags:         []model.JobTag{{ID: 1, Name: "VIP"}},
	}, nil).Once()

	rec := do(salesBreakdownEngine(svc), http.MethodGet, salesBreakdownPath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"types": ["forecast", "revenue"],
		"bys": ["client", "service", "item", "staff", "source", "tag"],
		"measures": ["collected", "collected_without_tax"],
		"methods": ["cash", "check", "card", "other"],
		"groups": {
			"service": [{"id": 1, "name": "Bi-Monthly Service"}],
			"item": [{"id": 7, "name": "Travel Fee"}],
			"staff": [{"id": 2, "name": "thon nguyen"}],
			"source": [{"id": 3, "name": "Facebook"}],
			"tag": [{"id": 1, "name": "VIP"}]
		},
		"sort_keys": ["name", "account_number", "jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec", "total"],
		"default_sort": {"key": "name", "order": "asc"},
		"years": {"min": 2000, "max": 2100},
		"limits": {"default": 50, "max": 200}
	}`, rec.Body.String())
}

func TestSalesBreakdownFiltersWithNothingIsEmptyLists(t *testing.T) {
	svc := handlermocks.NewMockSalesBreakdownService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&model.SalesBreakdownGroupOptions{}, nil).Once()

	rec := do(salesBreakdownEngine(svc), http.MethodGet, salesBreakdownPath+"/filters", "")

	for _, list := range []string{"service", "item", "staff", "source", "tag"} {
		require.Contains(t, rec.Body.String(), `"`+list+`":[]`)
	}
}

func TestSalesBreakdownFiltersMapsServiceErrors(t *testing.T) {
	svc := handlermocks.NewMockSalesBreakdownService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(nil, errors.New("pq: down")).Once()

	rec := do(salesBreakdownEngine(svc), http.MethodGet, salesBreakdownPath+"/filters", "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "internal_error", decodeError(t, rec).Code)
}

func TestSalesBreakdownOnlyGetIsAllowed(t *testing.T) {
	for _, path := range []string{salesBreakdownPath, salesBreakdownPath + "/filters"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			t.Run(method+" "+path, func(t *testing.T) {
				rec := do(salesBreakdownEngine(handlermocks.NewMockSalesBreakdownService(t)), method, path, "")
				require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
			})
		}
	}
}
