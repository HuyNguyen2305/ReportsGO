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

// ListAgingParams are the request values as received, all strings, so that every problem can be reported as a
// field-level validation error. Field names in errors are the query parameter names.
type ListAgingParams struct {
	AsOf        string   // as_of: YYYY-MM-DD; empty means today (the Current tab)
	Days        string   // days: 30 | 60 | 90
	ServiceType []string // service_type
	Email       string   // email
	Search      string   // q
	Sort        string   // sort
	Order       string   // order: asc | desc
	Limit       string   // limit
	Cursor      string   // cursor
}

// agingRequest is the validated form of ListAgingParams.
type agingRequest struct {
	query model.AgingQuery
	asOf  time.Time
}

// asOf resolves as_of: the given day, or today when it is missing. A day after today is rejected, because nothing
// can have happened yet. today is a calendar date in the business time zone.
func (v *validator) asOf(raw string, today time.Time) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return today
	}
	t, ok := v.date("as_of", raw)
	if !ok {
		return today
	}
	if t.After(today) {
		v.add("as_of", "must not be in the future")
		return today
	}
	return t
}

// minAgeDays resolves days; empty means no age filter (0).
func (v *validator) minAgeDays(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err == nil {
		for _, d := range model.AgingMinAgeDays {
			if n == d {
				return n
			}
		}
	}
	v.add("days", fmt.Sprintf("must be one of: %s", joinInts(model.AgingMinAgeDays)))
	return 0
}

func joinInts(in []int) string {
	parts := make([]string, len(in))
	for i, n := range in {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

// email validates the billing email search term: plain text, trimmed, no longer than a search term.
func (v *validator) email(raw string) string {
	s := strings.TrimSpace(raw)
	switch {
	case !utf8.ValidString(s):
		v.add("email", "must be valid UTF-8 text")
	case strings.ContainsRune(s, 0):
		v.add("email", "must not contain NUL characters")
	case utf8.RuneCountInString(s) > MaxSearchLength:
		v.add("email", fmt.Sprintf("must be at most %d characters long", MaxSearchLength))
	}
	if v.failed("email") {
		return ""
	}
	return s
}

// parseAgingParams validates and normalizes p. All problems are returned together as one model.InvalidInput
// error. now supplies today's date in the business time zone.
func parseAgingParams(p ListAgingParams, now time.Time) (agingRequest, error) {
	var v validator
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	filter := model.AgingFilter{}
	filter.AsOf = v.asOf(p.AsOf, today)
	filter.MinAgeDays = v.minAgeDays(p.Days)
	filter.ServiceTypeIDs = v.ids("service_type", p.ServiceType)
	filter.Email = v.email(p.Email)
	filter.Search = v.search(p.Search)

	key, desc := parseSort(&v, p.Sort, p.Order, model.AgingSortKeys, model.AgingSortCustomer)
	// The default is the customer's name, A to Z; parseSort's "newest first" default does not apply to a name.
	if strings.TrimSpace(p.Sort) == "" && strings.TrimSpace(p.Order) == "" {
		desc = false
	}
	sort := model.AgingSort{Key: key, Desc: desc}
	limit := v.limit(p.Limit)

	var after *model.AgingCursor
	if raw := strings.TrimSpace(p.Cursor); raw != "" && !v.failed("sort") && !v.failed("order") {
		cur, err := DecodeAgingCursor(raw, sort)
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
		return agingRequest{}, err
	}
	return agingRequest{
		query: model.AgingQuery{Filter: filter, Sort: sort, Limit: limit, After: after},
		asOf:  filter.AsOf,
	}, nil
}
