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

const estimatesPath = "/api/v1/reports/estimates"

func estimateEngine(svc handler.EstimateService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewEstimateHandler(svc, logger)},
	})
}

func emptyEstimateResult() *service.ListEstimatesResult {
	return &service.ListEstimatesResult{Items: []model.EstimateRow{}, Limit: 50}
}

// estimateParamsFor performs GET /estimates?query against a mocked service and returns the params it saw.
func estimateParamsFor(t *testing.T, query string) service.ListEstimatesParams {
	t.Helper()

	svc := handlermocks.NewMockEstimateService(t)
	var got service.ListEstimatesParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListEstimatesParams) { got = p }).
		Return(emptyEstimateResult(), nil).Once()

	rec := do(estimateEngine(svc), http.MethodGet, estimatesPath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

func estimateNeverCalled(t *testing.T) *handlermocks.MockEstimateService {
	return handlermocks.NewMockEstimateService(t)
}

func TestEstimateListResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockEstimateService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListEstimatesResult{
		Items: []model.EstimateRow{
			{
				ID: 7, EstimateNumber: "123183", AccountNumber: "63714", CustomerID: 5, CustomerName: "#F Last Name",
				Phone: strPtr("012222222"), BillingEmail: strPtr("d@cm.co"), CreatedBy: strPtr("Thọ Nguyễn Quang"),
				Date: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), Total: 2440,
				Status: model.EstimateAccepted, Signature: model.SignatureSigned,
			},
			{
				ID: 8, EstimateNumber: "EN1", AccountNumber: "63716", CustomerID: 6, CustomerName: "# D",
				Date: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC), Total: -500,
				Status: model.EstimateDraft, Signature: model.SignatureDraft,
			},
		},
		Limit:      50,
		HasMore:    true,
		NextCursor: "opaque-cursor",
		Summary:    &model.EstimateSummary{Count: 19, Total: 69812},
		From:       time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(estimateEngine(svc), http.MethodGet, estimatesPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{
				"id": 7, "estimate_number": "123183", "account_number": "63714",
				"customer": {"id": 5, "name": "#F Last Name"},
				"phone": "012222222", "billing_email": "d@cm.co", "created_by": "Thọ Nguyễn Quang",
				"date": "2026-10-19", "total": "24.40", "status": "accepted", "signature_status": "signed"
			},
			{
				"id": 8, "estimate_number": "EN1", "account_number": "63716",
				"customer": {"id": 6, "name": "# D"},
				"phone": null, "billing_email": null, "created_by": null,
				"date": "2026-10-10", "total": "-5.00", "status": "draft", "signature_status": "draft"
			}
		],
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"summary": {"count": 19, "total": "698.12"},
		"range": {"from": "2026-10-01", "to": null}
	}`, rec.Body.String())
}

func TestEstimateListResponseForAnEmptyLaterPage(t *testing.T) {
	svc := handlermocks.NewMockEstimateService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListEstimatesResult{
		Items: []model.EstimateRow{}, Limit: 5,
		From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(estimateEngine(svc), http.MethodGet, estimatesPath+"?cursor=x", "")

	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 5, "has_more": false, "next_cursor": null},
		"summary": null,
		"range": {"from": "2026-10-01", "to": "2026-10-31"}
	}`, rec.Body.String())
}

func TestEstimateListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListEstimatesParams
	}{
		{"no query", "", service.ListEstimatesParams{}},
		{"every single-value parameter", "?from=2026-10-01&to=2026-10-31&lifecycle=archived&q=saliba&sort=phone&order=desc&limit=25&cursor=abc",
			service.ListEstimatesParams{From: "2026-10-01", To: "2026-10-31", Lifecycle: "archived", Search: "saliba",
				Sort: "phone", Order: "desc", Limit: "25", Cursor: "abc"}},
		{"repeated key", "?status=accepted&status=sent", service.ListEstimatesParams{Status: []string{"accepted", "sent"}}},
		{"comma separated", "?status=accepted,sent", service.ListEstimatesParams{Status: []string{"accepted", "sent"}}},
		{"repeated and comma separated mixed", "?status=accepted,sent&status=declined", service.ListEstimatesParams{Status: []string{"accepted", "sent", "declined"}}},
		{"whitespace and blanks are dropped", "?status=%20accepted%20,,sent,&status=", service.ListEstimatesParams{Status: []string{"accepted", "sent"}}},
		{"only blanks leaves the list empty", "?status=&signature_status=,", service.ListEstimatesParams{}},
		{"both multi-value parameters", "?status=draft&signature_status=signed,pending",
			service.ListEstimatesParams{Status: []string{"draft"}, SignatureStatus: []string{"signed", "pending"}}},
		{"empty single values stay empty strings", "?from=&limit=", service.ListEstimatesParams{}},

		{"percent-encoded at sign", "?q=d%40cm.co", service.ListEstimatesParams{Search: "d@cm.co"}},
		{"plus is a space", "?q=a+b", service.ListEstimatesParams{Search: "a b"}},
		{"encoded percent sign", "?q=50%25", service.ListEstimatesParams{Search: "50%"}},
		{"encoded accented letter", "?q=%C3%A9", service.ListEstimatesParams{Search: "é"}},
		{"a comma inside q is not a separator", "?q=a%2Cb", service.ListEstimatesParams{Search: "a,b"}},
		{"sql text is just text", "?q=%27%3B+DROP+TABLE+billing_documents%3B--", service.ListEstimatesParams{Search: "'; DROP TABLE billing_documents;--"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, estimateParamsFor(t, tt.query))
		})
	}
}

func TestEstimateListRejectsUnsupportedAndDuplicatedParameters(t *testing.T) {
	unsupported := func(name string) []dto.ErrorDetail {
		return []dto.ErrorDetail{{Field: name, Message: "is not a supported parameter"}}
	}
	tests := []struct {
		name  string
		query string
		want  []dto.ErrorDetail
	}{
		{"typo in a parameter name", "?statuss=draft", unsupported("statuss")},
		{"names are case-sensitive", "?Status=draft", unsupported("Status")},
		{"cache buster", "?_=1699999999", unsupported("_")},
		{"invoice-only service_id", "?service_id=1", unsupported("service_id")},
		{"invoice-only tag_id", "?tag_id=1", unsupported("tag_id")},
		{"invoice-only payment_term_id", "?payment_term_id=1", unsupported("payment_term_id")},
		{"single-value parameter twice", "?limit=1&limit=2", []dto.ErrorDetail{{Field: "limit", Message: "must be given only once"}}},
		{"q twice", "?q=a&q=b", []dto.ErrorDetail{{Field: "q", Message: "must be given only once"}}},
		{"problems are listed in a stable order", "?zzz=1&aaa=2&limit=1&limit=2", []dto.ErrorDetail{
			{Field: "aaa", Message: "is not a supported parameter"},
			{Field: "limit", Message: "must be given only once"},
			{Field: "zzz", Message: "is not a supported parameter"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(estimateEngine(estimateNeverCalled(t)), http.MethodGet, estimatesPath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, tt.want, body.Details)
		})
	}

	t.Run("a very long unknown name is truncated in the error", func(t *testing.T) {
		rec := do(estimateEngine(estimateNeverCalled(t)), http.MethodGet, estimatesPath+"?"+strings.Repeat("x", 5000)+"=1", "")

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Equal(t, strings.Repeat("x", 64)+"...", decodeError(t, rec).Details[0].Field)
	})
}

func TestEstimateListRejectsMalformedQueryStrings(t *testing.T) {
	for _, query := range []string{"?q=%zz", "?q=%", "?q=a;b=c"} {
		t.Run(query, func(t *testing.T) {
			rec := do(estimateEngine(estimateNeverCalled(t)), http.MethodGet, estimatesPath+query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestEstimateListMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation error", model.InvalidInput(model.FieldError{Field: "limit", Message: "must be a whole number between 1 and 200"}), 422, "invalid_input"},
		{"wrapped validation error", fmt.Errorf("list: %w", model.InvalidInput(model.FieldError{Field: "q", Message: "x"})), 422, "invalid_input"},
		{"storage error", errors.New("pq: password authentication failed"), 500, "internal_error"},
		{"deadline exceeded", fmt.Errorf("list estimates: %w", context.DeadlineExceeded), 504, "timeout"},
		{"client went away", fmt.Errorf("list estimates: %w", context.Canceled), 499, "client_closed_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := handlermocks.NewMockEstimateService(t)
			svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, tt.err).Once()

			rec := do(estimateEngine(svc), http.MethodGet, estimatesPath, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
			require.NotContains(t, rec.Body.String(), "password", "internal error text must not leak")
		})
	}

	t.Run("validation details reach the client", func(t *testing.T) {
		svc := handlermocks.NewMockEstimateService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil,
			model.InvalidInput(model.FieldError{Field: "status", Message: "must be one of: draft, sent, accepted, declined"})).Once()

		rec := do(estimateEngine(svc), http.MethodGet, estimatesPath+"?status=paid", "")

		require.Equal(t, []dto.ErrorDetail{{Field: "status", Message: "must be one of: draft, sent, accepted, declined"}}, decodeError(t, rec).Details)
	})
}

func TestEstimateListPassesTheRequestContextToTheService(t *testing.T) {
	type key struct{}
	svc := handlermocks.NewMockEstimateService(t)
	svc.EXPECT().List(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(key{}) == "marker" }), mock.Anything).
		Return(emptyEstimateResult(), nil).Once()

	req := httptest.NewRequest(http.MethodGet, estimatesPath, nil).
		WithContext(context.WithValue(context.Background(), key{}, "marker"))
	rec := httptest.NewRecorder()
	estimateEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestEstimateResponsesAreNeverCached(t *testing.T) {
	setRequestID := func(c *gin.Context) { c.Header("X-Request-ID", "rid-7"); c.Next() }

	t.Run("success", func(t *testing.T) {
		svc := handlermocks.NewMockEstimateService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(emptyEstimateResult(), nil).Once()

		rec := do(estimateEngine(svc, setRequestID), http.MethodGet, estimatesPath, "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		require.Equal(t, "rid-7", rec.Header().Get("X-Request-ID"))
	})

	t.Run("validation error", func(t *testing.T) {
		rec := do(estimateEngine(estimateNeverCalled(t), setRequestID), http.MethodGet, estimatesPath+"?nope=1", "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "rid-7", decodeError(t, rec).RequestID)
	})

	t.Run("server error", func(t *testing.T) {
		svc := handlermocks.NewMockEstimateService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errors.New("boom")).Once()

		rec := do(estimateEngine(svc), http.MethodGet, estimatesPath, "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})

	t.Run("filters", func(t *testing.T) {
		rec := do(estimateEngine(estimateNeverCalled(t)), http.MethodGet, estimatesPath+"/filters", "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})
}

func TestEstimateFiltersResponseJSONContract(t *testing.T) {
	// The choices are fixed, so the service is never called.
	rec := do(estimateEngine(estimateNeverCalled(t)), http.MethodGet, estimatesPath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"statuses": ["draft", "sent", "accepted", "declined"],
		"signature_statuses": ["draft", "pending", "signed", "declined"],
		"lifecycles": ["active", "archived", "deleted"],
		"sort_keys": ["account_number", "estimate_number", "customer", "phone", "billing_email",
		              "created_by", "date", "total", "status", "signature_status"],
		"default_sort": {"key": "date", "order": "desc"},
		"limits": {"default": 50, "max": 200}
	}`, rec.Body.String())
}

func TestEstimateRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantCode   string
	}{
		{"POST list", http.MethodPost, estimatesPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"DELETE list", http.MethodDelete, estimatesPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"POST filters", http.MethodPost, estimatesPath + "/filters", http.StatusMethodNotAllowed, "method_not_allowed"},
		{"unknown subpath", http.MethodGet, estimatesPath + "/nope", http.StatusNotFound, "not_found"},
		{"not under the reports prefix", http.MethodGet, "/estimates", http.StatusNotFound, "not_found"},
		{"invoice routes are not served by this handler", http.MethodGet, invoicesPath, http.StatusNotFound, "not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(estimateEngine(estimateNeverCalled(t)), tt.method, tt.target, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
		})
	}
}

func TestNewEstimateHandlerAcceptsANilLogger(t *testing.T) {
	require.NotNil(t, handler.NewEstimateHandler(estimateNeverCalled(t), nil))
}
