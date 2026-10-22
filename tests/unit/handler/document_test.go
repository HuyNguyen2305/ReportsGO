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

const documentsPath = "/api/v1/reports/documents"

func documentEngine(svc handler.DocumentService, extra ...gin.HandlerFunc) *gin.Engine {
	logger, _ := newLogger()
	return handler.NewRouter(handler.Deps{
		Logger:     logger,
		Middleware: extra,
		Reports:    []handler.RouteRegistrar{handler.NewDocumentHandler(svc, logger)},
	})
}

func emptyDocumentResult() *service.ListDocumentsResult {
	return &service.ListDocumentsResult{Items: []model.DocumentRow{}, Limit: 50}
}

// documentParamsFor performs GET /documents?query against a mocked service and returns the params it saw.
func documentParamsFor(t *testing.T, query string) service.ListDocumentsParams {
	t.Helper()

	svc := handlermocks.NewMockDocumentService(t)
	var got service.ListDocumentsParams
	svc.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, p service.ListDocumentsParams) { got = p }).
		Return(emptyDocumentResult(), nil).Once()

	rec := do(documentEngine(svc), http.MethodGet, documentsPath+query, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return got
}

func documentNeverCalled(t *testing.T) *handlermocks.MockDocumentService {
	return handlermocks.NewMockDocumentService(t)
}

func TestDocumentListResponseJSONContract(t *testing.T) {
	plus7 := time.FixedZone("UTC+7", 7*3600)
	svc := handlermocks.NewMockDocumentService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListDocumentsResult{
		Items: []model.DocumentRow{
			{
				ID: 17, DocumentNumber: "5982", TypeID: 1, TypeName: "1-Time Treatment (PestPros of MI)",
				CustomerID: 2, CustomerName: "Customer 1", AccountNumber: "63733", CreatedBy: strPtr("Thọ Nguyễn Quang"),
				CreatedAt: time.Date(2026, 10, 22, 8, 15, 30, 123456000, time.UTC), Status: model.DocumentSigned,
			},
			{
				// a whole second, from a non-UTC zone: still written in UTC with six fractional digits
				ID: 3, DocumentNumber: "X1", TypeID: 2, TypeName: "1 Year Termite Warranty (Spidermen)",
				CustomerID: 6, CustomerName: "# D", AccountNumber: "63716",
				CreatedAt: time.Date(2026, 10, 12, 15, 0, 0, 0, plus7), Status: model.DocumentDraft,
			},
		},
		Limit:      50,
		HasMore:    true,
		NextCursor: "opaque-cursor",
		Summary:    &model.DocumentSummary{Count: 18},
		From:       time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(documentEngine(svc), http.MethodGet, documentsPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"data": [
			{
				"id": 17, "document_number": "5982",
				"document_type": {"id": 1, "name": "1-Time Treatment (PestPros of MI)"},
				"customer": {"id": 2, "name": "Customer 1"}, "account_number": "63733",
				"created_by": "Thọ Nguyễn Quang", "created_at": "2026-10-22T08:15:30.123456Z", "status": "signed"
			},
			{
				"id": 3, "document_number": "X1",
				"document_type": {"id": 2, "name": "1 Year Termite Warranty (Spidermen)"},
				"customer": {"id": 6, "name": "# D"}, "account_number": "63716",
				"created_by": null, "created_at": "2026-10-12T08:00:00.000000Z", "status": "draft"
			}
		],
		"page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
		"summary": {"count": 18},
		"range": {"from": "2026-10-01", "to": null}
	}`, rec.Body.String())
}

func TestDocumentListResponseForAnEmptyLaterPage(t *testing.T) {
	svc := handlermocks.NewMockDocumentService(t)
	svc.EXPECT().List(mock.Anything, mock.Anything).Return(&service.ListDocumentsResult{
		Items: []model.DocumentRow{}, Limit: 5,
		From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
	}, nil).Once()

	rec := do(documentEngine(svc), http.MethodGet, documentsPath+"?cursor=x", "")

	require.JSONEq(t, `{
		"data": [],
		"page": {"limit": 5, "has_more": false, "next_cursor": null},
		"summary": null,
		"range": {"from": "2026-10-01", "to": "2026-10-31"}
	}`, rec.Body.String())
}

func TestDocumentListQueryParsing(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  service.ListDocumentsParams
	}{
		{"no query", "", service.ListDocumentsParams{}},
		{"every single-value parameter", "?from=2026-10-01&to=2026-10-31&lifecycle=deleted&q=saliba&sort=type&order=desc&limit=25&cursor=abc",
			service.ListDocumentsParams{From: "2026-10-01", To: "2026-10-31", Lifecycle: "deleted", Search: "saliba",
				Sort: "type", Order: "desc", Limit: "25", Cursor: "abc"}},
		{"repeated key", "?status=sent&status=signed", service.ListDocumentsParams{Status: []string{"sent", "signed"}}},
		{"comma separated", "?status=sent,signed", service.ListDocumentsParams{Status: []string{"sent", "signed"}}},
		{"repeated and comma separated mixed", "?status=draft,sent&status=signed", service.ListDocumentsParams{Status: []string{"draft", "sent", "signed"}}},
		{"type ids in every form", "?document_type_id=1,2&document_type_id=3", service.ListDocumentsParams{DocumentTypeIDs: []string{"1", "2", "3"}}},
		{"whitespace and blanks are dropped", "?status=%20sent%20,,signed,&status=", service.ListDocumentsParams{Status: []string{"sent", "signed"}}},
		{"only blanks leaves the list empty", "?status=&document_type_id=,", service.ListDocumentsParams{}},
		{"both multi-value parameters", "?status=draft&document_type_id=4",
			service.ListDocumentsParams{Status: []string{"draft"}, DocumentTypeIDs: []string{"4"}}},
		{"empty single values stay empty strings", "?from=&limit=", service.ListDocumentsParams{}},

		{"percent-encoded at sign", "?q=d%40cm.co", service.ListDocumentsParams{Search: "d@cm.co"}},
		{"plus is a space", "?q=a+b", service.ListDocumentsParams{Search: "a b"}},
		{"encoded percent sign", "?q=50%25", service.ListDocumentsParams{Search: "50%"}},
		{"encoded accented letter", "?q=%C3%A9", service.ListDocumentsParams{Search: "é"}},
		{"a comma inside q is not a separator", "?q=a%2Cb", service.ListDocumentsParams{Search: "a,b"}},
		{"sql text is just text", "?q=%27%3B+DROP+TABLE+documents%3B--", service.ListDocumentsParams{Search: "'; DROP TABLE documents;--"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, documentParamsFor(t, tt.query))
		})
	}
}

func TestDocumentListRejectsUnsupportedAndDuplicatedParameters(t *testing.T) {
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
		{"estimate parameter signature_status", "?signature_status=signed", unsupported("signature_status")},
		{"invoice parameter service_id", "?service_id=1", unsupported("service_id")},
		{"invoice parameter tag_id", "?tag_id=1", unsupported("tag_id")},
		{"invoice parameter payment_term_id", "?payment_term_id=1", unsupported("payment_term_id")},
		{"type id has a longer name", "?type_id=1", unsupported("type_id")},
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
			rec := do(documentEngine(documentNeverCalled(t)), http.MethodGet, documentsPath+tt.query, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			body := decodeError(t, rec)
			require.Equal(t, "invalid_input", body.Code)
			require.Equal(t, tt.want, body.Details)
		})
	}

	t.Run("a very long unknown name is truncated in the error", func(t *testing.T) {
		rec := do(documentEngine(documentNeverCalled(t)), http.MethodGet, documentsPath+"?"+strings.Repeat("x", 5000)+"=1", "")

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.Equal(t, strings.Repeat("x", 64)+"...", decodeError(t, rec).Details[0].Field)
	})
}

func TestDocumentListRejectsMalformedQueryStrings(t *testing.T) {
	for _, query := range []string{"?q=%zz", "?q=%", "?q=a;b=c"} {
		t.Run(query, func(t *testing.T) {
			rec := do(documentEngine(documentNeverCalled(t)), http.MethodGet, documentsPath+query, "")

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "invalid_request", decodeError(t, rec).Code)
		})
	}
}

func TestDocumentListMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation error", model.InvalidInput(model.FieldError{Field: "limit", Message: "must be a whole number between 1 and 200"}), 422, "invalid_input"},
		{"wrapped validation error", fmt.Errorf("list: %w", model.InvalidInput(model.FieldError{Field: "q", Message: "x"})), 422, "invalid_input"},
		{"storage error", errors.New("pq: password authentication failed"), 500, "internal_error"},
		{"deadline exceeded", fmt.Errorf("list documents: %w", context.DeadlineExceeded), 504, "timeout"},
		{"client went away", fmt.Errorf("list documents: %w", context.Canceled), 499, "client_closed_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := handlermocks.NewMockDocumentService(t)
			svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, tt.err).Once()

			rec := do(documentEngine(svc), http.MethodGet, documentsPath, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
			require.NotContains(t, rec.Body.String(), "password", "internal error text must not leak")
		})
	}

	t.Run("validation details reach the client", func(t *testing.T) {
		svc := handlermocks.NewMockDocumentService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil,
			model.InvalidInput(model.FieldError{Field: "lifecycle", Message: "must be one of: active, deleted"})).Once()

		rec := do(documentEngine(svc), http.MethodGet, documentsPath+"?lifecycle=archived", "")

		require.Equal(t, []dto.ErrorDetail{{Field: "lifecycle", Message: "must be one of: active, deleted"}}, decodeError(t, rec).Details)
	})
}

func TestDocumentListPassesTheRequestContextToTheService(t *testing.T) {
	type key struct{}
	svc := handlermocks.NewMockDocumentService(t)
	svc.EXPECT().List(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(key{}) == "marker" }), mock.Anything).
		Return(emptyDocumentResult(), nil).Once()

	req := httptest.NewRequest(http.MethodGet, documentsPath, nil).
		WithContext(context.WithValue(context.Background(), key{}, "marker"))
	rec := httptest.NewRecorder()
	documentEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestDocumentResponsesAreNeverCached(t *testing.T) {
	setRequestID := func(c *gin.Context) { c.Header("X-Request-ID", "rid-7"); c.Next() }

	t.Run("success", func(t *testing.T) {
		svc := handlermocks.NewMockDocumentService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(emptyDocumentResult(), nil).Once()

		rec := do(documentEngine(svc, setRequestID), http.MethodGet, documentsPath, "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		require.Equal(t, "rid-7", rec.Header().Get("X-Request-ID"))
	})

	t.Run("validation error", func(t *testing.T) {
		rec := do(documentEngine(documentNeverCalled(t), setRequestID), http.MethodGet, documentsPath+"?nope=1", "")

		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		require.Equal(t, "rid-7", decodeError(t, rec).RequestID)
	})

	t.Run("server error", func(t *testing.T) {
		svc := handlermocks.NewMockDocumentService(t)
		svc.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errors.New("boom")).Once()

		rec := do(documentEngine(svc), http.MethodGet, documentsPath, "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})

	t.Run("filters", func(t *testing.T) {
		svc := handlermocks.NewMockDocumentService(t)
		svc.EXPECT().FilterOptions(mock.Anything).Return(&service.DocumentFilterOptions{Types: []model.DocumentType{}}, nil).Once()

		rec := do(documentEngine(svc), http.MethodGet, documentsPath+"/filters", "")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})
}

func TestDocumentFiltersResponseJSONContract(t *testing.T) {
	svc := handlermocks.NewMockDocumentService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.DocumentFilterOptions{
		Types: []model.DocumentType{{ID: 3, Name: "1 Year SubTermite Agreement (LibertyPS)"}, {ID: 1, Name: "1-Time Treatment (PestPros of MI)"}},
	}, nil).Once()

	rec := do(documentEngine(svc), http.MethodGet, documentsPath+"/filters", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"document_types": [
			{"id": 3, "name": "1 Year SubTermite Agreement (LibertyPS)"},
			{"id": 1, "name": "1-Time Treatment (PestPros of MI)"}
		],
		"statuses": ["draft", "sent", "signed"],
		"lifecycles": ["active", "deleted"],
		"sort_keys": ["document_number", "type", "account_number", "customer", "created_by", "created_at", "status"],
		"default_sort": {"key": "created_at", "order": "desc"},
		"limits": {"default": 50, "max": 200}
	}`, rec.Body.String())
}

func TestDocumentFiltersWithNoTypesUsesAnEmptyArray(t *testing.T) {
	svc := handlermocks.NewMockDocumentService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(&service.DocumentFilterOptions{}, nil).Once()

	rec := do(documentEngine(svc), http.MethodGet, documentsPath+"/filters", "")

	require.Contains(t, rec.Body.String(), `"document_types":[]`)
}

func TestDocumentFiltersServiceError(t *testing.T) {
	svc := handlermocks.NewMockDocumentService(t)
	svc.EXPECT().FilterOptions(mock.Anything).Return(nil, errors.New("db down")).Once()

	rec := do(documentEngine(svc), http.MethodGet, documentsPath+"/filters", "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "internal_error", decodeError(t, rec).Code)
	require.NotContains(t, rec.Body.String(), "db down")
}

func TestDocumentFiltersPassTheRequestContextToTheService(t *testing.T) {
	type key struct{}
	svc := handlermocks.NewMockDocumentService(t)
	svc.EXPECT().FilterOptions(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(key{}) == "marker" })).
		Return(&service.DocumentFilterOptions{}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, documentsPath+"/filters", nil).
		WithContext(context.WithValue(context.Background(), key{}, "marker"))
	rec := httptest.NewRecorder()
	documentEngine(svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestDocumentRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantCode   string
	}{
		{"POST list", http.MethodPost, documentsPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"DELETE list", http.MethodDelete, documentsPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"POST filters", http.MethodPost, documentsPath + "/filters", http.StatusMethodNotAllowed, "method_not_allowed"},
		{"unknown subpath", http.MethodGet, documentsPath + "/nope", http.StatusNotFound, "not_found"},
		{"not under the reports prefix", http.MethodGet, "/documents", http.StatusNotFound, "not_found"},
		{"invoice routes are not served by this handler", http.MethodGet, invoicesPath, http.StatusNotFound, "not_found"},
		{"estimate routes are not served by this handler", http.MethodGet, estimatesPath, http.StatusNotFound, "not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(documentEngine(documentNeverCalled(t)), tt.method, tt.target, "")

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantCode, decodeError(t, rec).Code)
		})
	}
}

func TestNewDocumentHandlerAcceptsANilLogger(t *testing.T) {
	require.NotNil(t, handler.NewDocumentHandler(documentNeverCalled(t), nil))
}
