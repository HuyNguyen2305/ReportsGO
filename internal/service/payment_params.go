package service

import (
	"errors"
	"strings"
	"time"

	"reportsgo/internal/model"
)

// ListPaymentsParams are the request values as received, all strings, so that every problem can be reported as
// a field-level validation error. Field names in errors are the query parameter names.
type ListPaymentsParams struct {
	From, To  string // from, to: YYYY-MM-DD, on the payment date
	Lifecycle string // lifecycle: active | deleted

	Status []string // status
	Method []string // method

	Search string // q
	Sort   string // sort
	Order  string // order: asc | desc
	Limit  string // limit
	Cursor string // cursor
}

// paymentRequest is the validated form of ListPaymentsParams.
type paymentRequest struct {
	query    model.PaymentQuery
	from, to time.Time // effective dates, including a defaulted month; zero when that side is open
}

// parsePaymentParams validates and normalizes p. All problems are returned together as one
// model.InvalidInput error. now supplies the default date range.
func parsePaymentParams(p ListPaymentsParams, now time.Time) (paymentRequest, error) {
	var v validator
	filter := model.PaymentFilter{}

	// The payment date is a plain calendar date, so the inclusive range maps straight onto the filter.
	from, to := v.dateRange(p.From, p.To, now)
	filter.DateFrom, filter.DateTo = from, to

	filter.Lifecycle = v.lifecycleIn(p.Lifecycle, model.PaymentLifecycles)
	filter.Statuses = parseEnums(&v, "status", p.Status, model.PaymentStatuses)
	filter.Methods = parseEnums(&v, "method", p.Method, model.PaymentMethods)
	filter.Search = v.search(p.Search)

	key, desc := parseSort(&v, p.Sort, p.Order, model.PaymentSortKeys, model.PaymentSortDate)
	sort := model.PaymentSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.PaymentCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodePaymentCursor(raw, sort)
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
		return paymentRequest{}, err
	}
	return paymentRequest{
		query: model.PaymentQuery{Filter: filter, Sort: sort, Limit: limit, After: after},
		from:  from,
		to:    to,
	}, nil
}
