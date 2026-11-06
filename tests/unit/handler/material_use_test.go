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

const materialUsePath = "/api/v1/reports/material-use"

func materialUseEngine(svc handler.MaterialUseService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewMaterialUseHandler(svc, logger)},
	})
}

// materialUseParamsFor performs GET /material-use?query against a mocked service and returns the params it saw.
func materialUseParamsFor(t *testing.T, query string) service.ListMaterialUseParams {
	t.Helper()
	svc := handlermocks.NewMockMaterialUseService(t)
	var got service.ListMaterialUseParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListMaterialUseParams) { got = p }).
		Return(&service.ListMaterialUseResult{Items: []model.MaterialUseRow{}, Limit: 50}, nil).Once()

	rec := do(materialUseEngine(svc), http.MethodGet, materialUsePath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

func TestMaterialUseListResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockMaterialUseService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListMaterialUseResult{
		Items: []model.MaterialUseRow{
			{MaterialID: 1, Material: "Demand CS", EPA: strPtr("100-1066"), Qty: "4.250", Applications: 3},
			{MaterialID: 4, Material: "Glue Boards", Qty: "4.000", Applications: 1},
		},
		Limit: 50, HasMore: true, NextCursor: "opaque-cursor",
		From: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(materialUseEngine(svc), http.MethodGet, materialUsePath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{"material_id": 1, "material": "Demand CS", "epa": "100-1066", "qty": "4.250", "applications": 3},
			{"material_id": 4, "material": "Glue Boards", "epa": null, "qty": "4.000", "applications": 1}
		],
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"range": {"from": "2026-11-01", "to": null}
	}`, rec.Body.String())
}

func TestMaterialUseListResponseForAnEmptyPage(t *testing.T) {
	svc := handlermocks.NewMockMaterialUseService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListMaterialUseResult{
		Items: []model.MaterialUseRow{}, Limit: 5,
		From: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(materialUseEngine(svc), http.MethodGet, materialUsePath, "")

	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 5, "has_more": false, "next_cursor": null},
		"range": {"from": "2026-11-01", "to": "2026-11-30"}
	}`, rec.Body.String())
}

func TestMaterialUseListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListMaterialUseParams
	}{
		{"no query", "", service.ListMaterialUseParams{}},
		{"every single-value parameter", "?from=2026-11-01&to=2026-11-30&tab=devices&q=demand&sort=qty&order=desc&limit=25&cursor=abc",
			service.ListMaterialUseParams{From: "2026-11-01", To: "2026-11-30", Tab: "devices", Search: "demand", Sort: "qty", Order: "desc", Limit: "25", Cursor: "abc"}},
		{"clients in every form", "?client_id=1,2&client_id=3", service.ListMaterialUseParams{CustomerIDs: []string{"1", "2", "3"}}},
		{"materials in every form", "?material_id=4&material_id=5,6", service.ListMaterialUseParams{MaterialIDs: []string{"4", "5", "6"}}},
		{"whitespace and blanks are dropped", "?client_id=%201%20,,2,&material_id=", service.ListMaterialUseParams{CustomerIDs: []string{"1", "2"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, materialUseParamsFor(t, tt.query))
		})
	}
}

func TestMaterialUseListRejectsUnsupportedParameters(t *testing.T) {
	tests := []struct {
		name  string
		query string
		field string
		msg   string
	}{
		{"unknown name", "?colour=red", "colour", "is not a supported parameter"},
		{"customer_id is client_id here", "?customer_id=1", "customer_id", "is not a supported parameter"},
		{"material, not material_ids", "?material=1", "material", "is not a supported parameter"},
		{"single-value parameter twice", "?tab=jobs&tab=devices", "tab", "must be given only once"},
		{"limit twice", "?limit=1&limit=2", "limit", "must be given only once"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := handlermocks.NewMockMaterialUseService(t) // never called

			rec := do(materialUseEngine(svc), http.MethodGet, materialUsePath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, []dto.ErrorDetail{{Field: tt.field, Message: tt.msg}}, body.Details)
		})
	}
}

func TestMaterialUseListRejectsMalformedQueryStrings(t *testing.T) {
	for _, query := range []string{"?q=%zz", "?q=%"} {
		t.Run(query, func(t *testing.T) {
			rec := do(materialUseEngine(handlermocks.NewMockMaterialUseService(t)), http.MethodGet, materialUsePath+query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestMaterialUseListMapsServiceErrors(t *testing.T) {
	t.Run("validation errors are 422", func(t *testing.T) {
		svc := handlermocks.NewMockMaterialUseService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).
			Return(nil, model.InvalidInput(model.FieldError{Field: "tab", Message: "must be one of: all"})).Once()

		rec := do(materialUseEngine(svc), http.MethodGet, materialUsePath, "")

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Equal(t, "invalid_input", decodeError(t, rec).Code)
	})

	t.Run("storage errors are 500 and leak nothing", func(t *testing.T) {
		svc := handlermocks.NewMockMaterialUseService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errors.New("pq: password authentication failed")).Once()

		rec := do(materialUseEngine(svc), http.MethodGet, materialUsePath, "")

		require.Equal(t, http.StatusInternalServerError, rec.Code)
		require.Equal(t, "internal_error", decodeError(t, rec).Code)
		require.NotContains(t, rec.Body.String(), "password")
	})
}

func TestMaterialUseListPassesTheRequestContext(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "marker")
	svc := handlermocks.NewMockMaterialUseService(t)
	svc.EXPECT().List(mock.MatchedBy(func(c context.Context) bool { return c.Value(key{}) == "marker" }), mock.Anything).
		Return(&service.ListMaterialUseResult{Items: []model.MaterialUseRow{}, Limit: 50}, nil).Once()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, materialUsePath, nil)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	materialUseEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestMaterialUseFiltersResponse(t *testing.T) {
	svc := handlermocks.NewMockMaterialUseService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.MaterialUseFilterOptions{
		Materials: []model.MaterialOption{{ID: 1, Name: "Demand CS"}},
		Clients:   []model.ClientOption{{ID: 7, Name: "William Saliba"}},
	}, nil).Once()

	rec := do(materialUseEngine(svc), http.MethodGet, materialUsePath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"materials": [{"id": 1, "name": "Demand CS"}],
		"clients": [{"id": 7, "name": "William Saliba"}],
		"tabs": ["all", "jobs", "devices"],
		"sort_keys": ["material", "epa", "qty", "applications"],
		"default_sort": {"key": "material", "order": "asc"},
		"limits": {"default": 50, "max": 200}
	}`, rec.Body.String())
}

func TestMaterialUseFiltersMapsErrors(t *testing.T) {
	svc := handlermocks.NewMockMaterialUseService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(nil, errors.New("boom")).Once()

	rec := do(materialUseEngine(svc), http.MethodGet, materialUsePath+"/filters", "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestMaterialUseRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantCode   string
	}{
		{"POST list", http.MethodPost, materialUsePath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"POST filters", http.MethodPost, materialUsePath + "/filters", http.StatusMethodNotAllowed, "method_not_allowed"},
		{"unknown subpath", http.MethodGet, materialUsePath + "/nope", http.StatusNotFound, "not_found"},
		{"not under the reports prefix", http.MethodGet, "/material-use", http.StatusNotFound, "not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(materialUseEngine(handlermocks.NewMockMaterialUseService(t)), tt.method, tt.target, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
		})
	}
}

func TestNewMaterialUseHandlerAcceptsANilLogger(t *testing.T) {
	require.NotNil(t, handler.NewMaterialUseHandler(handlermocks.NewMockMaterialUseService(t), nil))
}
