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

const inboundLeadPath = "/api/v1/reports/inbound-leads"

func inboundLeadEngine(svc handler.InboundLeadService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewInboundLeadHandler(svc, logger)},
	})
}

// inboundLeadParamsFor performs GET /inbound-leads?query against a mocked service and returns the params it saw.
func inboundLeadParamsFor(t *testing.T, query string) service.ListInboundLeadsParams {
	t.Helper()
	svc := handlermocks.NewMockInboundLeadService(t)
	var got service.ListInboundLeadsParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListInboundLeadsParams) { got = p }).
		Return(&service.ListInboundLeadsResult{Items: []model.InboundLeadRow{}, Limit: 50}, nil).Once()

	rec := do(inboundLeadEngine(svc), http.MethodGet, inboundLeadPath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

func TestInboundLeadListResponseJSONContract(t *testing.T) {
	plus7 := time.FixedZone("UTC+7", 7*3600)
	svc := handlermocks.NewMockInboundLeadService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListInboundLeadsResult{
		Items: []model.InboundLeadRow{
			{
				ID: 23, CustomerName: "Noah", SourceID: int64Ptr(1), Source: strPtr("Facebook"),
				Email: strPtr("noah.berg@example.com"), Phone: strPtr("(555) 010-0003"), Notes: "Wants a Bi-Monthly Service",
				DateAdded: time.Date(2026, 11, 3, 9, 15, 30, 123456000, time.UTC),
				ActiveJob: &model.InboundLeadJob{
					ServiceTypeID: 2, ServiceType: "Bi-Monthly Service",
					ScheduledAt: time.Date(2026, 11, 12, 16, 0, 0, 0, plus7), // written in UTC
				},
			},
			{
				// no source, email, phone, note or job; a whole second from a non-UTC zone is still written in UTC
				ID: 22, CustomerName: "Cold Call Co", DateAdded: time.Date(2026, 11, 3, 16, 15, 30, 0, plus7),
			},
		},
		Limit: 50, HasMore: true, NextCursor: "opaque-cursor",
		From: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(inboundLeadEngine(svc), http.MethodGet, inboundLeadPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{"id": 23, "customer": {"id": 23, "name": "Noah"}, "source": {"id": 1, "name": "Facebook"},
			 "email": "noah.berg@example.com", "phone": "(555) 010-0003", "notes": "Wants a Bi-Monthly Service",
			 "date_added": "2026-11-03T09:15:30.123456Z",
			 "active_job": {"service_type": {"id": 2, "name": "Bi-Monthly Service"}, "scheduled_at": "2026-11-12T09:00:00.000000Z"}},
			{"id": 22, "customer": {"id": 22, "name": "Cold Call Co"}, "source": null,
			 "email": null, "phone": null, "notes": "", "date_added": "2026-11-03T09:15:30.000000Z", "active_job": null}
		],
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"range": {"from": "2026-11-01", "to": null}
	}`, rec.Body.String())
}

func TestInboundLeadListResponseForAnEmptyPage(t *testing.T) {
	svc := handlermocks.NewMockInboundLeadService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListInboundLeadsResult{
		Items: []model.InboundLeadRow{}, Limit: 5,
		From: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(inboundLeadEngine(svc), http.MethodGet, inboundLeadPath, "")

	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 5, "has_more": false, "next_cursor": null},
		"range": {"from": "2026-11-01", "to": "2026-11-30"}
	}`, rec.Body.String())
}

func TestInboundLeadListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListInboundLeadsParams
	}{
		{"no query", "", service.ListInboundLeadsParams{}},
		{"every single-value parameter", "?from=2026-11-01&to=2026-11-30&name_format=full_name&q=mia&sort=email&order=desc&limit=25&cursor=abc",
			service.ListInboundLeadsParams{From: "2026-11-01", To: "2026-11-30", NameFormat: "full_name", Search: "mia", Sort: "email", Order: "desc", Limit: "25", Cursor: "abc"}},
		{"sources in every form", "?source_id=1,2&source_id=3", service.ListInboundLeadsParams{SourceIDs: []string{"1", "2", "3"}}},
		{"none is passed on for the service to read", "?source_id=none,3", service.ListInboundLeadsParams{SourceIDs: []string{"none", "3"}}},
		{"whitespace and blanks are dropped", "?source_id=%201%20,,&q=", service.ListInboundLeadsParams{SourceIDs: []string{"1"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, inboundLeadParamsFor(t, tt.query))
		})
	}
}

func TestInboundLeadListRejectsUnsupportedParameters(t *testing.T) {
	tests := []struct {
		name  string
		query string
		field string
		msg   string
	}{
		{"unknown name", "?colour=red", "colour", "is not a supported parameter"},
		{"there are no charts, so no year", "?year=2026", "year", "is not a supported parameter"},
		{"the source filter is source_id", "?lead_source_id=1", "lead_source_id", "is not a supported parameter"},
		{"status is always lead", "?status=active", "status", "is not a supported parameter"},
		{"single-value parameter twice", "?name_format=first_name&name_format=last_name", "name_format", "must be given only once"},
		{"limit twice", "?limit=1&limit=2", "limit", "must be given only once"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(inboundLeadEngine(handlermocks.NewMockInboundLeadService(t)), http.MethodGet, inboundLeadPath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, []dto.ErrorDetail{{Field: tt.field, Message: tt.msg}}, body.Details)
		})
	}
}

func TestInboundLeadListRejectsMalformedQueryStrings(t *testing.T) {
	for _, query := range []string{"?q=%zz", "?q=%"} {
		t.Run(query, func(t *testing.T) {
			rec := do(inboundLeadEngine(handlermocks.NewMockInboundLeadService(t)), http.MethodGet, inboundLeadPath+query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestInboundLeadListMapsServiceErrors(t *testing.T) {
	t.Run("validation errors are 422", func(t *testing.T) {
		svc := handlermocks.NewMockInboundLeadService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).
			Return(nil, model.InvalidInput(model.FieldError{Field: "source_id", Message: "must be positive whole numbers"})).Once()

		rec := do(inboundLeadEngine(svc), http.MethodGet, inboundLeadPath, "")

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Equal(t, "invalid_input", decodeError(t, rec).Code)
	})

	t.Run("storage errors are 500 and leak nothing", func(t *testing.T) {
		svc := handlermocks.NewMockInboundLeadService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errors.New("pq: password authentication failed")).Once()

		rec := do(inboundLeadEngine(svc), http.MethodGet, inboundLeadPath, "")

		require.Equal(t, http.StatusInternalServerError, rec.Code)
		require.Equal(t, "internal_error", decodeError(t, rec).Code)
		require.NotContains(t, rec.Body.String(), "password")
	})
}

func TestInboundLeadListPassesTheRequestContext(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "marker")
	svc := handlermocks.NewMockInboundLeadService(t)
	svc.EXPECT().List(mock.MatchedBy(func(c context.Context) bool { return c.Value(key{}) == "marker" }), mock.Anything).
		Return(&service.ListInboundLeadsResult{Items: []model.InboundLeadRow{}, Limit: 50}, nil).Once()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, inboundLeadPath, nil)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	inboundLeadEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestInboundLeadFiltersResponse(t *testing.T) {
	svc := handlermocks.NewMockInboundLeadService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.InboundLeadFilterOptions{
		Sources: []model.LeadSource{{ID: 1, Name: "Facebook"}, {ID: 2, Name: "Google"}},
	}, nil).Once()

	rec := do(inboundLeadEngine(svc), http.MethodGet, inboundLeadPath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"sources": [{"id": 1, "name": "Facebook"}, {"id": 2, "name": "Google"}],
		"name_formats": ["first_name", "last_name", "full_name"],
		"sort_keys": ["customer", "source", "email", "phone", "notes", "date_added"],
		"default_sort": {"key": "date_added", "order": "desc"},
		"limits": {"default": 50, "max": 200}
	}`, rec.Body.String())
}

func TestInboundLeadFiltersMapsErrors(t *testing.T) {
	svc := handlermocks.NewMockInboundLeadService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(nil, errors.New("boom")).Once()

	rec := do(inboundLeadEngine(svc), http.MethodGet, inboundLeadPath+"/filters", "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestInboundLeadRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantCode   string
	}{
		{"POST list", http.MethodPost, inboundLeadPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"POST filters", http.MethodPost, inboundLeadPath + "/filters", http.StatusMethodNotAllowed, "method_not_allowed"},
		{"there is no summary endpoint", http.MethodGet, inboundLeadPath + "/summary", http.StatusNotFound, "not_found"},
		{"not under the reports prefix", http.MethodGet, "/inbound-leads", http.StatusNotFound, "not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(inboundLeadEngine(handlermocks.NewMockInboundLeadService(t)), tt.method, tt.target, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
		})
	}
}

func TestNewInboundLeadHandlerAcceptsANilLogger(t *testing.T) {
	require.NotNil(t, handler.NewInboundLeadHandler(handlermocks.NewMockInboundLeadService(t), nil))
}
