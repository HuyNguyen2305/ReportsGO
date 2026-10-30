package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"reportsgo/internal/model"
)

// Bounds of the year parameter.
const (
	MinSalesBreakdownYear = 2000
	MaxSalesBreakdownYear = 2100
)

// ListSalesBreakdownParams are the request values as received, all strings, so that every problem can be reported
// as a field-level validation error. Field names in errors are the query parameter names.
type ListSalesBreakdownParams struct {
	Type    string   // type: forecast | revenue
	By      string   // by: client | service | item | staff | source | tag
	Year    string   // year
	Group   []string // group: ids of the chosen grouping
	Measure string   // measure: collected | collected_without_tax (revenue only)
	Method  []string // method (revenue only)

	Search string // q
	Sort   string // sort
	Order  string // order: asc | desc
	Limit  string // limit
	Cursor string // cursor
}

// requiredEnum resolves a single required value of valid.
func requiredEnum[T ~string](v *validator, field, raw string, valid []T) T {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		v.add(field, "is required")
		return ""
	}
	for _, t := range valid {
		if raw == string(t) {
			return t
		}
	}
	v.add(field, "must be one of: "+joinValues(valid))
	return ""
}

// salesBreakdownYear resolves year; the default is the year now falls in.
func (v *validator) salesBreakdownYear(raw string, now time.Time) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return now.Year()
	}
	y, err := strconv.Atoi(raw)
	if err != nil || y < MinSalesBreakdownYear || y > MaxSalesBreakdownYear {
		v.add("year", fmt.Sprintf("must be a year between %d and %d", MinSalesBreakdownYear, MaxSalesBreakdownYear))
		return now.Year()
	}
	return y
}

// parseSalesBreakdownParams validates and normalizes p. All problems are returned together as one model.InvalidInput
// error. now supplies the default year and the business time zone the months are read in.
func parseSalesBreakdownParams(p ListSalesBreakdownParams, now time.Time) (model.SalesBreakdownQuery, error) {
	var v validator
	filter := model.SalesBreakdownFilter{}

	filter.Type = requiredEnum(&v, "type", p.Type, model.SalesBreakdownTypes)
	filter.By = requiredEnum(&v, "by", p.By, model.SalesBreakdownBys)

	filter.Year = v.salesBreakdownYear(p.Year, now)
	for m := range filter.MonthStarts {
		filter.MonthStarts[m] = time.Date(filter.Year, time.Month(m+1), 1, 0, 0, 0, 0, now.Location())
	}
	filter.YearFrom = time.Date(filter.Year, time.January, 1, 0, 0, 0, 0, time.UTC)
	filter.YearTo = time.Date(filter.Year, time.December, 31, 0, 0, 0, 0, time.UTC)

	filter.GroupIDs = v.ids("group", p.Group)
	filter.Search = v.search(p.Search)

	// The measure and the payment methods describe payments, which only Revenue has.
	methods := parseEnums(&v, "method", p.Method, model.PaymentMethods)
	measure := strings.ToLower(strings.TrimSpace(p.Measure))
	if filter.Type == model.SalesBreakdownForecast {
		if measure != "" {
			v.add("measure", "can only be used with type revenue")
		}
		if len(methods) > 0 {
			v.add("method", "can only be used with type revenue")
		}
	} else {
		filter.Methods = methods
		filter.Measure = model.SalesBreakdownCollected
		if measure != "" {
			filter.Measure = requiredEnum(&v, "measure", measure, model.SalesBreakdownMeasures)
		}
	}

	key, desc := parseSort(&v, p.Sort, p.Order, model.SalesBreakdownSortKeysFor(filter.By), model.SalesBreakdownSortName)
	// The default is the group name, A to Z; parseSort's "newest first" default does not apply to a name.
	if strings.TrimSpace(p.Sort) == "" && strings.TrimSpace(p.Order) == "" {
		desc = false
	}
	sort := model.SalesBreakdownSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.SalesBreakdownCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodeSalesBreakdownCursor(raw, sort)
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
		return model.SalesBreakdownQuery{}, err
	}
	return model.SalesBreakdownQuery{Filter: filter, Sort: sort, Limit: limit, After: after}, nil
}
