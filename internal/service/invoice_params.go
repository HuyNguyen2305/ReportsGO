package service

import (
	"errors"
	"strings"
	"time"

	"reportsgo/internal/model"
)

// ListInvoicesParams are the request values as received, all strings, so that every problem can be reported
// as a field-level validation error. Field names in errors are the query parameter names.
type ListInvoicesParams struct {
	From, To  string // from, to: YYYY-MM-DD
	Lifecycle string // lifecycle: active | archived | deleted

	Status          []string // status
	SignatureStatus []string // signature_status
	ServiceIDs      []string // service_id
	TagIDs          []string // tag_id
	PaymentTermIDs  []string // payment_term_id

	Search string // q
	Sort   string // sort
	Order  string // order: asc | desc
	Limit  string // limit
	Cursor string // cursor
}

// invoiceRequest is the validated form of ListInvoicesParams.
type invoiceRequest struct {
	query    model.InvoiceQuery
	from, to time.Time // effective dates, including a defaulted month; zero when that side is open
}

// parseInvoiceParams validates and normalizes p. All problems are returned together as one
// model.InvalidInput error. now supplies the default date range.
func parseInvoiceParams(p ListInvoicesParams, now time.Time) (invoiceRequest, error) {
	var v validator
	filter := model.InvoiceFilter{}

	from, to := v.dateRange(p.From, p.To, now)
	filter.DateFrom, filter.DateTo = from, to

	filter.Lifecycle = v.lifecycle(p.Lifecycle)
	filter.Statuses = parseEnums(&v, "status", p.Status, model.InvoiceStatuses)
	filter.SignatureStatus = parseEnums(&v, "signature_status", p.SignatureStatus, model.SignatureStatuses)
	filter.ServiceTypeIDs = v.ids("service_id", p.ServiceIDs)
	filter.TagIDs = v.ids("tag_id", p.TagIDs)
	filter.PaymentTermIDs = v.ids("payment_term_id", p.PaymentTermIDs)
	filter.Search = v.search(p.Search)

	key, desc := parseSort(&v, p.Sort, p.Order, model.InvoiceSortKeys, model.SortDate)
	sort := model.InvoiceSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.InvoiceCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodeInvoiceCursor(raw, sort)
		if err != nil {
			var ce *InvalidCursorError
			if errors.As(err, &ce) {
				v.add("cursor", ce.Reason)
			} else {
				v.add("cursor", "is not a valid cursor")
			}
		}
		after = cur
	}

	if err := v.err(); err != nil {
		return invoiceRequest{}, err
	}
	return invoiceRequest{
		query: model.InvoiceQuery{Filter: filter, Sort: sort, Limit: limit, After: after},
		from:  from,
		to:    to,
	}, nil
}
