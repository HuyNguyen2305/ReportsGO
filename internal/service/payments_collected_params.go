package service

import (
	"errors"
	"strings"
	"time"

	"reportsgo/internal/model"
)

// ListPaymentsCollectedParams are the request values as received, all strings, so that every problem can be
// reported as a field-level validation error. Field names in errors are the query parameter names.
type ListPaymentsCollectedParams struct {
	From, To   string // from, to: YYYY-MM-DD
	PaymentFor string // payment_for: invoices | credits

	Method      []string // method
	CustomerIDs []string // customer_id
	StaffIDs    []string // staff_id

	Search string // q
	Sort   string // sort
	Order  string // order: asc | desc
	Limit  string // limit
	Cursor string // cursor
}

// paymentsCollectedRequest is the validated form of ListPaymentsCollectedParams.
type paymentsCollectedRequest struct {
	query    model.PaymentCollectedQuery
	from, to time.Time // effective dates, including a defaulted month; zero when that side is open
}

// paymentFor resolves payment_for; the default is the invoices view.
func (v *validator) paymentFor(raw string) model.PaymentFor {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return model.PaymentForInvoices
	}
	for _, p := range model.PaymentFors {
		if raw == string(p) {
			return p
		}
	}
	v.add("payment_for", "must be one of: "+joinValues(model.PaymentFors))
	return model.PaymentForInvoices
}

// parsePaymentsCollectedParams validates and normalizes p. All problems are returned together as one
// model.InvalidInput error. now supplies the default date range.
func parsePaymentsCollectedParams(p ListPaymentsCollectedParams, now time.Time) (paymentsCollectedRequest, error) {
	var v validator
	filter := model.PaymentCollectedFilter{}

	from, to := v.dateRange(p.From, p.To, now)
	filter.DateFrom, filter.DateTo = from, to

	filter.PaymentFor = v.paymentFor(p.PaymentFor)
	filter.Methods = parseEnums(&v, "method", p.Method, model.PaymentMethods)
	filter.CustomerIDs = v.ids("customer_id", p.CustomerIDs)
	filter.StaffIDs = v.ids("staff_id", p.StaffIDs)
	filter.Search = v.search(p.Search)

	key, desc := parseSort(&v, p.Sort, p.Order, model.PaymentCollectedSortKeys, model.PaymentCollectedSortDate)
	sort := model.PaymentCollectedSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.PaymentCollectedCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodePaymentCollectedCursor(raw, sort)
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
		return paymentsCollectedRequest{}, err
	}
	return paymentsCollectedRequest{
		query: model.PaymentCollectedQuery{Filter: filter, Sort: sort, Limit: limit, After: after},
		from:  from,
		to:    to,
	}, nil
}
