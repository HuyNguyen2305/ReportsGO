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

const onlineBookingPath = "/api/v1/reports/online-bookings"

func onlineBookingEngine(svc handler.OnlineBookingService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewOnlineBookingHandler(svc, logger)},
	})
}

// onlineBookingParamsFor performs GET /online-bookings?query against a mocked service and returns the params it saw.
func onlineBookingParamsFor(t *testing.T, query string) service.ListOnlineBookingsParams {
	t.Helper()
	svc := handlermocks.NewMockOnlineBookingService(t)
	var got service.ListOnlineBookingsParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListOnlineBookingsParams) { got = p }).
		Return(&service.ListOnlineBookingsResult{Items: []model.OnlineBookingRow{}, Limit: 50}, nil).Once()

	rec := do(onlineBookingEngine(svc), http.MethodGet, onlineBookingPath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

func TestOnlineBookingListResponseJSONContract(t *testing.T) {
	plus7 := time.FixedZone("UTC+7", 7*3600)
	svc := handlermocks.NewMockOnlineBookingService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListOnlineBookingsResult{
		Items: []model.OnlineBookingRow{
			{
				ID: 31, CustomerID: 5, CustomerName: "Mia", ScheduledAt: time.Date(2026, 11, 12, 9, 0, 0, 123456000, time.UTC),
				ServiceTypeID: 2, ServiceType: "Bi-Monthly Service", Email: strPtr("mia@example.com"), Phone: strPtr("(555) 010-0001"),
				Address: model.LookupAddress{Line1: "1 Main St", City: "Austin", State: "TX", Zip: "73301", County: "Travis"},
				Notes:   "Gate code 1234", Status: model.JobConfirmed, SourceID: int64Ptr(1), Source: strPtr("Facebook"),
			},
			{
				// no source, email, phone or note; a whole second from a non-UTC zone is still written in UTC
				ID: 30, CustomerID: 6, CustomerName: "# D", ScheduledAt: time.Date(2026, 11, 2, 16, 0, 0, 0, plus7),
				ServiceTypeID: 1, ServiceType: "Blank Service 123",
				Address: model.LookupAddress{Line1: "2 Side St", City: "Dallas", State: "TX", Zip: "75001"},
				Status:  model.JobCancelled,
			},
		},
		Limit: 50, HasMore: true, NextCursor: "opaque-cursor",
		From: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(onlineBookingEngine(svc), http.MethodGet, onlineBookingPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{"id": 31, "customer": {"id": 5, "name": "Mia"}, "scheduled_at": "2026-11-12T09:00:00.123456Z",
			 "service_type": {"id": 2, "name": "Bi-Monthly Service"}, "email": "mia@example.com", "phone": "(555) 010-0001",
			 "address": {"line1": "1 Main St", "line2": "", "city": "Austin", "state": "TX", "zip": "73301", "county": "Travis"},
			 "notes": "Gate code 1234", "status": "confirmed", "source": {"id": 1, "name": "Facebook"}},
			{"id": 30, "customer": {"id": 6, "name": "# D"}, "scheduled_at": "2026-11-02T09:00:00.000000Z",
			 "service_type": {"id": 1, "name": "Blank Service 123"}, "email": null, "phone": null,
			 "address": {"line1": "2 Side St", "line2": "", "city": "Dallas", "state": "TX", "zip": "75001", "county": ""},
			 "notes": "", "status": "cancelled", "source": null}
		],
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"range": {"from": "2026-11-01", "to": null}
	}`, rec.Body.String())
}

func TestOnlineBookingListResponseForAnEmptyPage(t *testing.T) {
	svc := handlermocks.NewMockOnlineBookingService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListOnlineBookingsResult{
		Items: []model.OnlineBookingRow{}, Limit: 5,
		From: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(onlineBookingEngine(svc), http.MethodGet, onlineBookingPath, "")

	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 5, "has_more": false, "next_cursor": null},
		"range": {"from": "2026-11-01", "to": "2026-11-30"}
	}`, rec.Body.String())
}

func TestOnlineBookingListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListOnlineBookingsParams
	}{
		{"no query", "", service.ListOnlineBookingsParams{}},
		{"every single-value parameter", "?from=2026-11-01&to=2026-11-30&name_format=full_name&q=ants&sort=email&order=desc&limit=25&cursor=abc",
			service.ListOnlineBookingsParams{From: "2026-11-01", To: "2026-11-30", NameFormat: "full_name", Search: "ants", Sort: "email", Order: "desc", Limit: "25", Cursor: "abc"}},
		{"sources in every form", "?source_id=1,2&source_id=3", service.ListOnlineBookingsParams{SourceIDs: []string{"1", "2", "3"}}},
		{"none is passed on for the service to read", "?source_id=none,3", service.ListOnlineBookingsParams{SourceIDs: []string{"none", "3"}}},
		{"whitespace and blanks are dropped", "?source_id=%201%20,,&q=", service.ListOnlineBookingsParams{SourceIDs: []string{"1"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, onlineBookingParamsFor(t, tt.query))
		})
	}
}

func TestOnlineBookingListRejectsUnsupportedParameters(t *testing.T) {
	tests := []struct {
		name  string
		query string
		field string
		msg   string
	}{
		{"unknown name", "?colour=red", "colour", "is not a supported parameter"},
		{"there are no charts, so no year", "?year=2026", "year", "is not a supported parameter"},
		{"the screen has no status filter", "?status=confirmed", "status", "is not a supported parameter"},
		{"the source filter is source_id", "?lead_source_id=1", "lead_source_id", "is not a supported parameter"},
		{"single-value parameter twice", "?name_format=first_name&name_format=last_name", "name_format", "must be given only once"},
		{"limit twice", "?limit=1&limit=2", "limit", "must be given only once"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(onlineBookingEngine(handlermocks.NewMockOnlineBookingService(t)), http.MethodGet, onlineBookingPath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, []dto.ErrorDetail{{Field: tt.field, Message: tt.msg}}, body.Details)
		})
	}
}

func TestOnlineBookingListRejectsMalformedQueryStrings(t *testing.T) {
	for _, query := range []string{"?q=%zz", "?q=%"} {
		t.Run(query, func(t *testing.T) {
			rec := do(onlineBookingEngine(handlermocks.NewMockOnlineBookingService(t)), http.MethodGet, onlineBookingPath+query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestOnlineBookingListMapsServiceErrors(t *testing.T) {
	t.Run("validation errors are 422", func(t *testing.T) {
		svc := handlermocks.NewMockOnlineBookingService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).
			Return(nil, model.InvalidInput(model.FieldError{Field: "source_id", Message: "must be positive whole numbers"})).Once()

		rec := do(onlineBookingEngine(svc), http.MethodGet, onlineBookingPath, "")

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Equal(t, "invalid_input", decodeError(t, rec).Code)
	})

	t.Run("storage errors are 500 and leak nothing", func(t *testing.T) {
		svc := handlermocks.NewMockOnlineBookingService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errors.New("pq: password authentication failed")).Once()

		rec := do(onlineBookingEngine(svc), http.MethodGet, onlineBookingPath, "")

		require.Equal(t, http.StatusInternalServerError, rec.Code)
		require.Equal(t, "internal_error", decodeError(t, rec).Code)
		require.NotContains(t, rec.Body.String(), "password")
	})
}

func TestOnlineBookingListPassesTheRequestContext(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "marker")
	svc := handlermocks.NewMockOnlineBookingService(t)
	svc.EXPECT().List(mock.MatchedBy(func(c context.Context) bool { return c.Value(key{}) == "marker" }), mock.Anything).
		Return(&service.ListOnlineBookingsResult{Items: []model.OnlineBookingRow{}, Limit: 50}, nil).Once()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, onlineBookingPath, nil)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	onlineBookingEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestOnlineBookingFiltersResponse(t *testing.T) {
	svc := handlermocks.NewMockOnlineBookingService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.OnlineBookingFilterOptions{
		Sources: []model.LeadSource{{ID: 1, Name: "Facebook"}, {ID: 2, Name: "Google"}},
	}, nil).Once()

	rec := do(onlineBookingEngine(svc), http.MethodGet, onlineBookingPath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"sources": [{"id": 1, "name": "Facebook"}, {"id": 2, "name": "Google"}],
		"name_formats": ["first_name", "last_name", "full_name"],
		"sort_keys": ["customer", "scheduled_at", "service", "email", "phone", "address", "notes", "status"],
		"default_sort": {"key": "scheduled_at", "order": "desc"},
		"limits": {"default": 50, "max": 200}
	}`, rec.Body.String())
}

func TestOnlineBookingFiltersMapsErrors(t *testing.T) {
	svc := handlermocks.NewMockOnlineBookingService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(nil, errors.New("boom")).Once()

	rec := do(onlineBookingEngine(svc), http.MethodGet, onlineBookingPath+"/filters", "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestOnlineBookingRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantCode   string
	}{
		{"POST list", http.MethodPost, onlineBookingPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"POST filters", http.MethodPost, onlineBookingPath + "/filters", http.StatusMethodNotAllowed, "method_not_allowed"},
		{"there is no summary endpoint", http.MethodGet, onlineBookingPath + "/summary", http.StatusNotFound, "not_found"},
		{"not under the reports prefix", http.MethodGet, "/online-bookings", http.StatusNotFound, "not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(onlineBookingEngine(handlermocks.NewMockOnlineBookingService(t)), tt.method, tt.target, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
		})
	}
}

func TestNewOnlineBookingHandlerAcceptsANilLogger(t *testing.T) {
	require.NotNil(t, handler.NewOnlineBookingHandler(handlermocks.NewMockOnlineBookingService(t), nil))
}
