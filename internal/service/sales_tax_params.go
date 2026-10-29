package service

import (
	"strings"
	"time"

	"reportsgo/internal/model"
)

// ListSalesTaxParams are the request values as received, all strings, so that every problem can be reported as a
// field-level validation error. Field names in errors are the query parameter names. The report has no paging, so
// there is no limit or cursor.
type ListSalesTaxParams struct {
	From, To string // from, to: YYYY-MM-DD
	Basis    string // basis: collected | invoiced

	Method []string // method; only with the collected basis

	Sort  string // sort
	Order string // order: asc | desc
}

// salesTaxRequest is the validated form of ListSalesTaxParams.
type salesTaxRequest struct {
	filter   model.SalesTaxFilter
	sort     model.SalesTaxSort
	from, to time.Time // effective dates, including a defaulted month; zero when that side is open
}

// basis resolves the basis; the default is the cash basis.
func (v *validator) basis(raw string) model.SalesTaxBasis {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return model.SalesTaxCollected
	}
	for _, b := range model.SalesTaxBases {
		if raw == string(b) {
			return b
		}
	}
	v.add("basis", "must be one of: "+joinValues(model.SalesTaxBases))
	return model.SalesTaxCollected
}

// parseSalesTaxParams validates and normalizes p. All problems are returned together as one model.InvalidInput
// error. now supplies the default date range.
func parseSalesTaxParams(p ListSalesTaxParams, now time.Time) (salesTaxRequest, error) {
	var v validator
	filter := model.SalesTaxFilter{}

	from, to := v.dateRange(p.From, p.To, now)
	filter.DateFrom, filter.DateTo = from, to

	filter.Basis = v.basis(p.Basis)
	filter.Methods = parseEnums(&v, "method", p.Method, model.PaymentMethods)
	// A payment method means nothing on the invoiced basis, where no payment is involved; say so rather than
	// silently ignoring the filter.
	if len(filter.Methods) > 0 && filter.Basis == model.SalesTaxInvoiced && !v.failed("basis") {
		v.add("method", "can only be used with basis collected")
	}

	key, desc := parseSort(&v, p.Sort, p.Order, model.SalesTaxSortKeys, model.SalesTaxSortTaxableSales)

	if err := v.err(); err != nil {
		return salesTaxRequest{}, err
	}
	return salesTaxRequest{
		filter: filter,
		sort:   model.SalesTaxSort{Key: key, Desc: desc},
		from:   from,
		to:     to,
	}, nil
}
