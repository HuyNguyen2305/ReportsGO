package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

// InvoiceService is what the handler needs from the service layer. *service.InvoiceService satisfies it.
type InvoiceService interface {
	List(ctx context.Context, p service.ListInvoicesParams) (*service.ListInvoicesResult, error)
	FilterOptions(ctx context.Context) (*service.InvoiceFilterOptions, error)
}

// InvoiceHandler serves the Invoices report. It is a RouteRegistrar, mounted under ReportsPrefix.
type InvoiceHandler struct {
	svc    InvoiceService
	logger *slog.Logger
}

// NewInvoiceHandler returns the Invoices report handler.
func NewInvoiceHandler(svc InvoiceService, logger *slog.Logger) *InvoiceHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &InvoiceHandler{svc: svc, logger: logger}
}

// Register mounts GET /invoices and GET /invoices/filters.
func (h *InvoiceHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/invoices", h.List)
	rg.GET("/invoices/filters", h.Filters)
}

// List serves one page of the report.
func (h *InvoiceHandler) List(c *gin.Context) {
	params, err := parseInvoiceQuery(c.Request.URL.RawQuery)
	if errors.Is(err, errMalformedQuery) {
		WriteError(c, http.StatusBadRequest, "invalid_request", "query string is malformed")
		return
	}
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}

	res, err := h.svc.List(c.Request.Context(), params)
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toInvoiceListResponse(res))
}

// Filters serves the data behind the report's filter controls.
func (h *InvoiceHandler) Filters(c *gin.Context) {
	opts, err := h.svc.FilterOptions(c.Request.Context())
	if err != nil {
		RespondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toInvoiceFiltersResponse(opts))
}

var errMalformedQuery = errors.New("malformed query string")

// Query parameters accepted by the list endpoint.
var (
	singleValueParams = []string{"from", "to", "lifecycle", "q", "sort", "order", "limit", "cursor"}
	multiValueParams  = []string{"status", "signature_status", "service_id", "tag_id", "payment_term_id"}
)

// maxEchoedParamName bounds how much of an unknown parameter name is repeated back in an error.
const maxEchoedParamName = 64

// parseInvoiceQuery turns the raw query string into service params. It is strict on purpose: an unknown
// parameter (a typo such as "statuss") or a single-value parameter sent twice is an error, instead of silently
// returning unfiltered data. Multi-value parameters accept repeated keys and comma-separated lists alike.
func parseInvoiceQuery(rawQuery string) (service.ListInvoicesParams, error) {
	// url.Query() would silently drop malformed pairs; ParseQuery reports them.
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return service.ListInvoicesParams{}, errMalformedQuery
	}

	var problems []model.FieldError
	for name, vals := range values {
		switch {
		case contains(singleValueParams, name):
			if len(vals) > 1 {
				problems = append(problems, model.FieldError{Field: name, Message: "must be given only once"})
			}
		case contains(multiValueParams, name):
		default:
			problems = append(problems, model.FieldError{Field: truncate(name, maxEchoedParamName), Message: "is not a supported parameter"})
		}
	}
	if len(problems) > 0 {
		sort.Slice(problems, func(i, j int) bool { return problems[i].Field < problems[j].Field }) // map order is random
		return service.ListInvoicesParams{}, model.InvalidInput(problems...)
	}

	return service.ListInvoicesParams{
		From:            values.Get("from"),
		To:              values.Get("to"),
		Lifecycle:       values.Get("lifecycle"),
		Search:          values.Get("q"),
		Sort:            values.Get("sort"),
		Order:           values.Get("order"),
		Limit:           values.Get("limit"),
		Cursor:          values.Get("cursor"),
		Status:          flatten(values["status"]),
		SignatureStatus: flatten(values["signature_status"]),
		ServiceIDs:      flatten(values["service_id"]),
		TagIDs:          flatten(values["tag_id"]),
		PaymentTermIDs:  flatten(values["payment_term_id"]),
	}, nil
}

// flatten merges repeated keys and comma-separated values into one list; blanks are dropped.
func flatten(vals []string) []string {
	var out []string
	for _, v := range vals {
		for _, part := range strings.Split(v, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}
