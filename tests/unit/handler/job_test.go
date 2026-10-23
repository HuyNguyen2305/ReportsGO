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

const jobsPath = "/api/v1/reports/new-jobs"

func jobEngine(svc handler.JobService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewJobHandler(svc, logger)},
	})
}

func emptyJobResult() *service.ListJobsResult {
	return &service.ListJobsResult{Items: []model.JobRow{}, Limit: 50}
}

// jobParamsFor performs GET /new-jobs?query against a mocked service and returns the params it saw.
func jobParamsFor(t *testing.T, query string) service.ListJobsParams {
	t.Helper()

	svc := handlermocks.NewMockJobService(t)
	var got service.ListJobsParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListJobsParams) { got = p }).
		Return(emptyJobResult(), nil).Once()

	rec := do(jobEngine(svc), http.MethodGet, jobsPath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

func jobNeverCalled(t *testing.T) *handlermocks.MockJobService {
	return handlermocks.NewMockJobService(t)
}

func TestJobListResponseJSONContract(t *testing.T) {
	plus7 := time.FixedZone("UTC+7", 7*3600)
	svc := handlermocks.NewMockJobService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListJobsResult{
		Items: []model.JobRow{
			{
				ID: 17, CustomerID: 2, CustomerName: "Customer 1", AccountNumber: "63733",
				ServiceTypeID: 1, ServiceType: "Initial Service", SourceID: int64Ptr(4), Source: strPtr("Google"),
				InvoiceValue: 9500, CreatedAt: time.Date(2026, 10, 22, 8, 15, 30, 123456000, time.UTC), Status: model.JobCancelled,
			},
			{
				// no source; a whole second from a non-UTC zone is still written in UTC with six fractional digits
				ID: 3, CustomerID: 6, CustomerName: "# D", AccountNumber: "63716",
				ServiceTypeID: 2, ServiceType: "Bi-Monthly Service",
				InvoiceValue: 0, CreatedAt: time.Date(2026, 10, 12, 15, 0, 0, 0, plus7), Status: model.JobConfirmed,
			},
		},
		Limit:      50,
		HasMore:    true,
		NextCursor: "opaque-cursor",
		Summary:    &model.JobSummary{Count: 35, Total: 186126},
		From:       time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(jobEngine(svc), http.MethodGet, jobsPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{
				"id": 17, "customer": {"id": 2, "name": "Customer 1"}, "account_number": "63733",
				"service_type": {"id": 1, "name": "Initial Service"}, "source": {"id": 4, "name": "Google"},
				"invoice_value": "95.00", "created_at": "2026-10-22T08:15:30.123456Z", "status": "cancelled"
			},
			{
				"id": 3, "customer": {"id": 6, "name": "# D"}, "account_number": "63716",
				"service_type": {"id": 2, "name": "Bi-Monthly Service"}, "source": null,
				"invoice_value": "0.00", "created_at": "2026-10-12T08:00:00.000000Z", "status": "confirmed"
			}
		],
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"summary": {"count": 35, "total": "1861.26"},
		"range": {"from": "2026-10-01", "to": null}
	}`, rec.Body.String())
}

func TestJobListResponseForAnEmptyLaterPage(t *testing.T) {
	svc := handlermocks.NewMockJobService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListJobsResult{
		Items: []model.JobRow{}, Limit: 5,
		From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(jobEngine(svc), http.MethodGet, jobsPath+"?cursor=x", "")

	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 5, "has_more": false, "next_cursor": null},
		"summary": null,
		"range": {"from": "2026-10-01", "to": "2026-10-31"}
	}`, rec.Body.String())
}

func TestJobListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListJobsParams
	}{
		{"no query", "", service.ListJobsParams{}},
		{"every single-value parameter", "?from=2026-10-01&to=2026-10-31&q=saliba&sort=source&order=desc&limit=25&cursor=abc",
			service.ListJobsParams{From: "2026-10-01", To: "2026-10-31", Search: "saliba", Sort: "source", Order: "desc", Limit: "25", Cursor: "abc"}},
		{"repeated key", "?status=confirmed&status=completed", service.ListJobsParams{Status: []string{"confirmed", "completed"}}},
		{"comma separated", "?status=confirmed,completed", service.ListJobsParams{Status: []string{"confirmed", "completed"}}},
		{"repeated and comma separated mixed", "?status=unconfirmed,confirmed&status=completed", service.ListJobsParams{Status: []string{"unconfirmed", "confirmed", "completed"}}},
		{"service types in every form", "?service_type_id=1,2&service_type_id=3", service.ListJobsParams{ServiceTypeIDs: []string{"1", "2", "3"}}},
		{"lead sources in every form", "?lead_source_id=4&lead_source_id=5,6", service.ListJobsParams{LeadSourceIDs: []string{"4", "5", "6"}}},
		{"whitespace and blanks are dropped", "?status=%20confirmed%20,,completed,&status=", service.ListJobsParams{Status: []string{"confirmed", "completed"}}},
		{"only blanks leaves the lists empty", "?status=&service_type_id=,&lead_source_id=", service.ListJobsParams{}},
		{"all multi-value parameters", "?status=cancelled&service_type_id=4&lead_source_id=2",
			service.ListJobsParams{Status: []string{"cancelled"}, ServiceTypeIDs: []string{"4"}, LeadSourceIDs: []string{"2"}}},
		{"empty single values stay empty strings", "?from=&limit=", service.ListJobsParams{}},

		{"percent-encoded at sign", "?q=d%40cm.co", service.ListJobsParams{Search: "d@cm.co"}},
		{"plus is a space", "?q=a+b", service.ListJobsParams{Search: "a b"}},
		{"encoded percent sign", "?q=50%25", service.ListJobsParams{Search: "50%"}},
		{"a comma inside q is not a separator", "?q=a%2Cb", service.ListJobsParams{Search: "a,b"}},
		{"sql text is just text", "?q=%27%3B+DROP+TABLE+jobs%3B--", service.ListJobsParams{Search: "'; DROP TABLE jobs;--"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, jobParamsFor(t, tt.query))
		})
	}
}

func TestJobListRejectsUnsupportedAndDuplicatedParameters(t *testing.T) {
	unsupported := func(name string) []dto.ErrorDetail {
		return []dto.ErrorDetail{{Field: name, Message: "is not a supported parameter"}}
	}
	tests := []struct {
		name  string
		query string
		want  []dto.ErrorDetail
	}{
		{"typo in a parameter name", "?statuss=confirmed", unsupported("statuss")},
		{"names are case-sensitive", "?Status=confirmed", unsupported("Status")},
		{"jobs have no tabs", "?lifecycle=active", unsupported("lifecycle")},
		{"payment parameter method", "?method=cash", unsupported("method")},
		{"document parameter document_type_id", "?document_type_id=1", unsupported("document_type_id")},
		{"invoice parameter service_id", "?service_id=1", unsupported("service_id")},
		{"source has a longer name", "?source_id=1", unsupported("source_id")},
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
			rec := do(jobEngine(jobNeverCalled(t)), http.MethodGet, jobsPath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, tt.want, body.Details)
		})
	}

	t.Run("a very long unknown name is truncated in the error", func(t *testing.T) {
		rec := do(jobEngine(jobNeverCalled(t)), http.MethodGet, jobsPath+"?"+strings.Repeat("x", 5000)+"=1", "")

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Equal(t, strings.Repeat("x", 64)+"...", decodeError(t, rec).Details[0].Field)
	})
}

func TestJobListRejectsMalformedQueryStrings(t *testing.T) {
	for _, query := range []string{"?q=%zz", "?q=%", "?q=a;b=c"} {
		t.Run(query, func(t *testing.T) {
			rec := do(jobEngine(jobNeverCalled(t)), http.MethodGet, jobsPath+query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestJobListMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation error", model.InvalidInput(model.FieldError{Field: "limit", Message: "must be a whole number between 1 and 200"}), 422, "invalid_input"},
		{"wrapped validation error", fmt.Errorf("list: %w", model.InvalidInput(model.FieldError{Field: "q", Message: "x"})), 422, "invalid_input"},
		{"storage error", errors.New("pq: password authentication failed"), 500, "internal_error"},
		{"deadline exceeded", fmt.Errorf("list jobs: %w", context.DeadlineExceeded), 504, "timeout"},
		{"client went away", fmt.Errorf("list jobs: %w", context.Canceled), 499, "client_closed_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := handlermocks.NewMockJobService(t)
			svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, tt.err).Once()

			rec := do(jobEngine(svc), http.MethodGet, jobsPath, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
			require.NotContains(t, rec.Body.String(), "password", "internal error text must not leak")
		})
	}

	t.Run("validation details reach the client", func(t *testing.T) {
		svc := handlermocks.NewMockJobService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil,
			model.InvalidInput(model.FieldError{Field: "status", Message: "must be one of: unconfirmed, confirmed, completed, cancelled"})).Once()

		rec := do(jobEngine(svc), http.MethodGet, jobsPath+"?status=paid", "")

		require.Equal(t, []dto.ErrorDetail{{Field: "status", Message: "must be one of: unconfirmed, confirmed, completed, cancelled"}}, decodeError(t, rec).Details)
	})
}

func TestJobListPassesTheRequestContextToTheService(t *testing.T) {
	type key struct{}
	svc := handlermocks.NewMockJobService(t)
	svc.EXPECT().List(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(key{}) == "marker" }), mock.Anything).
		Return(emptyJobResult(), nil).Once()

	req := httptest.NewRequest(http.MethodGet, jobsPath, nil).
		WithContext(context.WithValue(context.Background(), key{}, "marker"))
	rec := httptest.NewRecorder()
	jobEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestJobResponsesAreNeverCached(t *testing.T) {
	setRequestID := func(c *gin.Context) { c.Header("X-Request-ID", "rid-7"); c.Next() }

	t.Run("success", func(t *testing.T) {
		svc := handlermocks.NewMockJobService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(emptyJobResult(), nil).Once()

		rec := do(jobEngine(svc, setRequestID), http.MethodGet, jobsPath, "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		require.Equal(t, "rid-7", rec.Header().Get("X-Request-ID"))
	})

	t.Run("validation error", func(t *testing.T) {
		rec := do(jobEngine(jobNeverCalled(t), setRequestID), http.MethodGet, jobsPath+"?nope=1", "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "rid-7", decodeError(t, rec).RequestID)
	})

	t.Run("server error", func(t *testing.T) {
		svc := handlermocks.NewMockJobService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errors.New("boom")).Once()

		rec := do(jobEngine(svc), http.MethodGet, jobsPath, "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})

	t.Run("filters", func(t *testing.T) {
		svc := handlermocks.NewMockJobService(t)
		svc.EXPECT().FilterOptions(mock.Anything).Return(&service.JobFilterOptions{}, nil).Once()

		rec := do(jobEngine(svc), http.MethodGet, jobsPath+"/filters", "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})
}

func TestJobFiltersResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockJobService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.JobFilterOptions{
		ServiceTypes: []model.ServiceType{{ID: 2, Name: "Initial Service"}, {ID: 1, Name: "Bi-Monthly Service"}},
		LeadSources:  []model.LeadSource{{ID: 3, Name: "Facebook"}},
	}, nil).Once()

	rec := do(jobEngine(svc), http.MethodGet, jobsPath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"service_types": [{"id": 2, "name": "Initial Service"}, {"id": 1, "name": "Bi-Monthly Service"}],
		"lead_sources": [{"id": 3, "name": "Facebook"}],
		"statuses": ["unconfirmed", "confirmed", "completed", "cancelled"],
		"sort_keys": ["customer", "account_number", "service_type", "source", "invoice_value", "created_at", "status"],
		"default_sort": {"key": "created_at", "order": "desc"},
		"limits": {"default": 50, "max": 200}
	}`, rec.Body.String())
}

func TestJobFiltersWithNoOptionsUseEmptyArrays(t *testing.T) {
	svc := handlermocks.NewMockJobService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.JobFilterOptions{}, nil).Once()

	rec := do(jobEngine(svc), http.MethodGet, jobsPath+"/filters", "")

	require.Contains(t, rec.Body.String(), `"service_types":[]`)
	require.Contains(t, rec.Body.String(), `"lead_sources":[]`)
}

func TestJobFiltersServiceError(t *testing.T) {
	svc := handlermocks.NewMockJobService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(nil, errors.New("db down")).Once()

	rec := do(jobEngine(svc), http.MethodGet, jobsPath+"/filters", "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "internal_error", decodeError(t, rec).Code)
	require.NotContains(t, rec.Body.String(), "db down")
}

func TestJobFiltersPassTheRequestContextToTheService(t *testing.T) {
	type key struct{}
	svc := handlermocks.NewMockJobService(t)
	svc.EXPECT().FilterOptions(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(key{}) == "marker" })).
		Return(&service.JobFilterOptions{}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, jobsPath+"/filters", nil).
		WithContext(context.WithValue(context.Background(), key{}, "marker"))
	rec := httptest.NewRecorder()
	jobEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestJobRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantCode   string
	}{
		{"POST list", http.MethodPost, jobsPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"DELETE list", http.MethodDelete, jobsPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"POST filters", http.MethodPost, jobsPath + "/filters", http.StatusMethodNotAllowed, "method_not_allowed"},
		{"unknown subpath", http.MethodGet, jobsPath + "/nope", http.StatusNotFound, "not_found"},
		{"not under the reports prefix", http.MethodGet, "/new-jobs", http.StatusNotFound, "not_found"},
		{"invoice routes are not served by this handler", http.MethodGet, invoicesPath, http.StatusNotFound, "not_found"},
		{"document routes are not served by this handler", http.MethodGet, documentsPath, http.StatusNotFound, "not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(jobEngine(jobNeverCalled(t)), tt.method, tt.target, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
		})
	}
}

func TestNewJobHandlerAcceptsANilLogger(t *testing.T) {
	require.NotNil(t, handler.NewJobHandler(jobNeverCalled(t), nil))
}
