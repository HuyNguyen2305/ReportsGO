package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"reportsgo/internal/model"
)

// Limits and defaults for the invoice report request.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
	MaxSearchLength = 100
	MaxIDsPerFilter = 100
	MaxEnumValues   = 10
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

	sort := v.sort(p.Sort, p.Order)
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

// validator collects field errors, at most one per field.
type validator struct {
	fields []model.FieldError
	seen   map[string]bool
}

func (v *validator) add(field, msg string) {
	if v.seen == nil {
		v.seen = map[string]bool{}
	}
	if v.seen[field] {
		return
	}
	v.seen[field] = true
	v.fields = append(v.fields, model.FieldError{Field: field, Message: msg})
}

func (v *validator) failed(field string) bool { return v.seen[field] }

func (v *validator) err() error {
	if len(v.fields) == 0 {
		return nil
	}
	return model.InvalidInput(v.fields...)
}

// dateRange parses from/to. With neither given the range is the current UTC calendar month; when only one is
// given the other side stays open (zero time).
func (v *validator) dateRange(rawFrom, rawTo string, now time.Time) (from, to time.Time) {
	rawFrom, rawTo = strings.TrimSpace(rawFrom), strings.TrimSpace(rawTo)
	if rawFrom == "" && rawTo == "" {
		n := now.UTC()
		first := time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, time.UTC)
		return first, first.AddDate(0, 1, -1)
	}

	var fromOK, toOK bool
	if rawFrom != "" {
		from, fromOK = v.date("from", rawFrom)
	}
	if rawTo != "" {
		to, toOK = v.date("to", rawTo)
	}
	if fromOK && toOK && to.Before(from) {
		v.add("to", "must not be before from")
	}
	return from, to
}

func (v *validator) date(field, raw string) (time.Time, bool) {
	t, err := time.Parse(dateLayout, raw)
	if err != nil {
		v.add(field, "must be a date in YYYY-MM-DD format")
		return time.Time{}, false
	}
	return t, true
}

func (v *validator) lifecycle(raw string) model.Lifecycle {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return model.LifecycleActive
	}
	for _, l := range model.Lifecycles {
		if raw == string(l) {
			return l
		}
	}
	v.add("lifecycle", "must be one of: "+joinValues(model.Lifecycles))
	return model.LifecycleActive
}

// parseEnums normalizes a list of enum values: trimmed, lower-cased, de-duplicated, blanks ignored.
func parseEnums[T ~string](v *validator, field string, raw []string, valid []T) []T {
	// Bound the work before doing any: de-duplication would otherwise hide a huge list of repeated values.
	if len(raw) > MaxEnumValues {
		v.add(field, fmt.Sprintf("must have at most %d values", MaxEnumValues))
		return nil
	}
	var out []T
	seen := map[T]bool{}
	for _, r := range raw {
		r = strings.ToLower(strings.TrimSpace(r))
		if r == "" {
			continue
		}
		val := T(r)
		if !contains(valid, val) {
			v.add(field, "must be one of: "+joinValues(valid))
			return nil
		}
		if !seen[val] {
			seen[val] = true
			out = append(out, val)
		}
	}
	return out
}

func (v *validator) ids(field string, raw []string) []int64 {
	if len(raw) > MaxIDsPerFilter {
		v.add(field, fmt.Sprintf("must have at most %d values", MaxIDsPerFilter))
		return nil
	}
	var out []int64
	seen := map[int64]bool{}
	for _, r := range raw {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		n, err := strconv.ParseInt(r, 10, 64)
		if err != nil || n < 1 {
			v.add(field, "must be positive whole numbers")
			return nil
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

func (v *validator) search(raw string) string {
	s := strings.TrimSpace(raw)
	switch {
	case !utf8.ValidString(s):
		v.add("q", "must be valid UTF-8 text")
	case strings.ContainsRune(s, 0):
		v.add("q", "must not contain NUL characters")
	case utf8.RuneCountInString(s) > MaxSearchLength:
		v.add("q", fmt.Sprintf("must be at most %d characters long", MaxSearchLength))
	}
	if v.failed("q") {
		return ""
	}
	return s
}

// sort resolves sort and order. With neither given the default is date descending (newest first);
// naming a sort key without an order means ascending.
func (v *validator) sort(rawKey, rawOrder string) model.InvoiceSort {
	rawKey = strings.ToLower(strings.TrimSpace(rawKey))
	rawOrder = strings.ToLower(strings.TrimSpace(rawOrder))

	s := model.InvoiceSort{Key: model.SortDate, Desc: rawKey == "" && rawOrder == ""}

	if rawKey != "" {
		key := model.InvoiceSortKey(rawKey)
		if contains(model.InvoiceSortKeys, key) {
			s.Key = key
		} else {
			v.add("sort", "must be one of: "+joinValues(model.InvoiceSortKeys))
		}
	}
	switch rawOrder {
	case "":
	case "asc":
		s.Desc = false
	case "desc":
		s.Desc = true
	default:
		v.add("order", "must be asc or desc")
	}
	return s
}

func (v *validator) limit(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultPageSize
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > MaxPageSize {
		v.add("limit", fmt.Sprintf("must be a whole number between 1 and %d", MaxPageSize))
		return DefaultPageSize
	}
	return n
}

func contains[T comparable](list []T, want T) bool {
	for _, x := range list {
		if x == want {
			return true
		}
	}
	return false
}

func joinValues[T ~string](list []T) string {
	parts := make([]string, len(list))
	for i, x := range list {
		parts[i] = string(x)
	}
	return strings.Join(parts, ", ")
}
