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

const deviceTrackingPath = "/api/v1/reports/device-tracking"

func deviceTrackingEngine(svc handler.DeviceTrackingService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewDeviceTrackingHandler(svc, logger)},
	})
}

// deviceTrackingParamsFor performs GET /device-tracking?query against a mocked service and returns the params it saw.
func deviceTrackingParamsFor(t *testing.T, query string) service.ListDeviceTrackingParams {
	t.Helper()
	svc := handlermocks.NewMockDeviceTrackingService(t)
	var got service.ListDeviceTrackingParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListDeviceTrackingParams) { got = p }).
		Return(&service.ListDeviceTrackingResult{Items: []model.DeviceTrackingRow{}, Limit: 50}, nil).Once()

	rec := do(deviceTrackingEngine(svc), http.MethodGet, deviceTrackingPath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

func floatPtr(v float64) *float64 { return &v }

func timePtr(t time.Time) *time.Time { return &t }

func TestDeviceTrackingListResponseJSONContract(t *testing.T) {
	plus7 := time.FixedZone("UTC+7", 7*3600)
	svc := handlermocks.NewMockDeviceTrackingService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListDeviceTrackingResult{
		Items: []model.DeviceTrackingRow{
			{
				ID: 5, CustomerID: 6, CustomerName: "AAA1 ccc1", ServiceTypeID: 3, ServiceType: "Every 21 Days",
				Date:     time.Date(2026, 11, 3, 3, 45, 0, 123456000, time.UTC),
				Address:  model.LookupAddress{Line1: "Terminal 4 Arrivals", City: "Queens", State: "NY", Zip: "11430", County: "Queens"},
				DeviceID: 9, DeviceName: "QA device 32d92e23", Barcode: "BC-2001", Latitude: floatPtr(37.421998), Longitude: floatPtr(-122.084),
				Area: "QA area bf9a99b2", DeviceType: "Bait Box",
				CheckedInAt: timePtr(time.Date(2026, 11, 3, 3, 50, 0, 0, time.UTC)), CheckedOutAt: timePtr(time.Date(2026, 11, 3, 11, 5, 0, 0, plus7)),
				Status: "Good", Activity: "Inspected", Notes: "Bait taken", AssignedToID: int64Ptr(1), AssignedTo: strPtr("Thọ"),
			},
			{
				// no coordinate, never checked, no assignee; a whole second from a non-UTC zone is still written in UTC
				ID: 4, CustomerID: 11, CustomerName: "Alaba David", ServiceTypeID: 1, ServiceType: "Blank Service",
				Date:     time.Date(2026, 11, 4, 9, 30, 0, 0, plus7),
				Address:  model.LookupAddress{Line1: "Española Way", Line2: "Unit 2", City: "Miami Beach", State: "FL", Zip: "33139"},
				DeviceID: 8, Area: "Area 1", DeviceType: "Laptop",
			},
		},
		Limit: 50, HasMore: true, NextCursor: "opaque-cursor",
		From: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(deviceTrackingEngine(svc), http.MethodGet, deviceTrackingPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{"id": 5, "customer": {"id": 6, "name": "AAA1 ccc1"}, "service_type": {"id": 3, "name": "Every 21 Days"},
			 "date": "2026-11-03T03:45:00.123456Z",
			 "address": {"line1": "Terminal 4 Arrivals", "line2": "", "city": "Queens", "state": "NY", "zip": "11430", "county": "Queens"},
			 "device": {"id": 9, "name": "QA device 32d92e23", "barcode": "BC-2001",
			            "coordinate": {"latitude": 37.421998, "longitude": -122.084}, "area": "QA area bf9a99b2", "device_type": "Bait Box"},
			 "check_in": "2026-11-03T03:50:00.000000Z", "status": "Good", "activity": "Inspected",
			 "check_out": "2026-11-03T04:05:00.000000Z", "notes": "Bait taken", "assigned_to": {"id": 1, "name": "Thọ"}},
			{"id": 4, "customer": {"id": 11, "name": "Alaba David"}, "service_type": {"id": 1, "name": "Blank Service"},
			 "date": "2026-11-04T02:30:00.000000Z",
			 "address": {"line1": "Española Way", "line2": "Unit 2", "city": "Miami Beach", "state": "FL", "zip": "33139", "county": ""},
			 "device": {"id": 8, "name": "", "barcode": "", "coordinate": null, "area": "Area 1", "device_type": "Laptop"},
			 "check_in": null, "status": "", "activity": "", "check_out": null, "notes": "", "assigned_to": null}
		],
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"range": {"from": "2026-11-01", "to": null}
	}`, rec.Body.String())
}

func TestDeviceTrackingListResponseForAnEmptyPage(t *testing.T) {
	svc := handlermocks.NewMockDeviceTrackingService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListDeviceTrackingResult{
		Items: []model.DeviceTrackingRow{}, Limit: 5,
		From: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(deviceTrackingEngine(svc), http.MethodGet, deviceTrackingPath, "")

	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 5, "has_more": false, "next_cursor": null},
		"range": {"from": "2026-11-01", "to": "2026-11-30"}
	}`, rec.Body.String())
}

func TestDeviceTrackingListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListDeviceTrackingParams
	}{
		{"no query", "", service.ListDeviceTrackingParams{}},
		{"every single-value parameter", "?from=2026-11-01&to=2026-11-30&q=bait&sort=barcode&order=desc&limit=25&cursor=abc",
			service.ListDeviceTrackingParams{From: "2026-11-01", To: "2026-11-30", Search: "bait", Sort: "barcode", Order: "desc", Limit: "25", Cursor: "abc"}},
		{"clients in every form", "?client_id=1,2&client_id=3", service.ListDeviceTrackingParams{CustomerIDs: []string{"1", "2", "3"}}},
		{"whitespace and blanks are dropped", "?client_id=%201%20,,&q=", service.ListDeviceTrackingParams{CustomerIDs: []string{"1"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, deviceTrackingParamsFor(t, tt.query))
		})
	}
}

func TestDeviceTrackingListRejectsUnsupportedParameters(t *testing.T) {
	tests := []struct {
		name  string
		query string
		field string
		msg   string
	}{
		{"unknown name", "?colour=red", "colour", "is not a supported parameter"},
		{"the screen has no name format", "?name_format=full_name", "name_format", "is not a supported parameter"},
		{"the screen has no source filter", "?source_id=1", "source_id", "is not a supported parameter"},
		{"the clients filter is client_id", "?customer_id=1", "customer_id", "is not a supported parameter"},
		{"single-value parameter twice", "?sort=city&sort=state", "sort", "must be given only once"},
		{"limit twice", "?limit=1&limit=2", "limit", "must be given only once"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(deviceTrackingEngine(handlermocks.NewMockDeviceTrackingService(t)), http.MethodGet, deviceTrackingPath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, []dto.ErrorDetail{{Field: tt.field, Message: tt.msg}}, body.Details)
		})
	}
}

func TestDeviceTrackingListRejectsMalformedQueryStrings(t *testing.T) {
	for _, query := range []string{"?q=%zz", "?q=%"} {
		t.Run(query, func(t *testing.T) {
			rec := do(deviceTrackingEngine(handlermocks.NewMockDeviceTrackingService(t)), http.MethodGet, deviceTrackingPath+query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestDeviceTrackingListMapsServiceErrors(t *testing.T) {
	t.Run("validation errors are 422", func(t *testing.T) {
		svc := handlermocks.NewMockDeviceTrackingService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).
			Return(nil, model.InvalidInput(model.FieldError{Field: "client_id", Message: "must be positive whole numbers"})).Once()

		rec := do(deviceTrackingEngine(svc), http.MethodGet, deviceTrackingPath, "")

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Equal(t, "invalid_input", decodeError(t, rec).Code)
	})

	t.Run("storage errors are 500 and leak nothing", func(t *testing.T) {
		svc := handlermocks.NewMockDeviceTrackingService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errors.New("pq: password authentication failed")).Once()

		rec := do(deviceTrackingEngine(svc), http.MethodGet, deviceTrackingPath, "")

		require.Equal(t, http.StatusInternalServerError, rec.Code)
		require.Equal(t, "internal_error", decodeError(t, rec).Code)
		require.NotContains(t, rec.Body.String(), "password")
	})
}

func TestDeviceTrackingListPassesTheRequestContext(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "marker")
	svc := handlermocks.NewMockDeviceTrackingService(t)
	svc.EXPECT().List(mock.MatchedBy(func(c context.Context) bool { return c.Value(key{}) == "marker" }), mock.Anything).
		Return(&service.ListDeviceTrackingResult{Items: []model.DeviceTrackingRow{}, Limit: 50}, nil).Once()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, deviceTrackingPath, nil)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	deviceTrackingEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestDeviceTrackingFiltersResponse(t *testing.T) {
	svc := handlermocks.NewMockDeviceTrackingService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.DeviceTrackingFilterOptions{
		Clients: []model.ClientOption{{ID: 7, Name: "William Saliba"}},
	}, nil).Once()

	rec := do(deviceTrackingEngine(svc), http.MethodGet, deviceTrackingPath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"clients": [{"id": 7, "name": "William Saliba"}],
		"sort_keys": ["customer", "service", "date", "address_1", "address_2", "city", "state", "zip", "device_name", "barcode",
		              "coordinate", "area", "device_type", "check_in", "status", "activity", "check_out", "notes", "assigned_to"],
		"default_sort": {"key": "date", "order": "desc"},
		"limits": {"default": 50, "max": 200}
	}`, rec.Body.String())
}

func TestDeviceTrackingFiltersMapsErrors(t *testing.T) {
	svc := handlermocks.NewMockDeviceTrackingService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(nil, errors.New("boom")).Once()

	rec := do(deviceTrackingEngine(svc), http.MethodGet, deviceTrackingPath+"/filters", "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestDeviceTrackingRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantCode   string
	}{
		{"POST list", http.MethodPost, deviceTrackingPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"POST filters", http.MethodPost, deviceTrackingPath + "/filters", http.StatusMethodNotAllowed, "method_not_allowed"},
		{"there is no summary endpoint", http.MethodGet, deviceTrackingPath + "/summary", http.StatusNotFound, "not_found"},
		{"not under the reports prefix", http.MethodGet, "/device-tracking", http.StatusNotFound, "not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(deviceTrackingEngine(handlermocks.NewMockDeviceTrackingService(t)), tt.method, tt.target, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
		})
	}
}

func TestNewDeviceTrackingHandlerAcceptsANilLogger(t *testing.T) {
	require.NotNil(t, handler.NewDeviceTrackingHandler(handlermocks.NewMockDeviceTrackingService(t), nil))
}
