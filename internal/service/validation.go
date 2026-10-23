package service

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"reportsgo/internal/model"
)

// Limits and defaults shared by the report requests.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
	MaxSearchLength = 100
	MaxIDsPerFilter = 100
	MaxEnumValues   = 10
)

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
	if strings.TrimSpace(rawFrom) == "" && strings.TrimSpace(rawTo) == "" {
		n := now.UTC()
		first := time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, time.UTC)
		return first, first.AddDate(0, 1, -1)
	}
	return v.parseRange(rawFrom, rawTo)
}

// openDateRange parses from/to for reports without a default range: with neither given both sides stay open
// (zero time), and when only one is given the other stays open too.
func (v *validator) openDateRange(rawFrom, rawTo string) (from, to time.Time) {
	return v.parseRange(rawFrom, rawTo)
}

// parseRange parses whichever of from/to is given and checks that to is not before from. A missing side is
// returned as the zero time.
func (v *validator) parseRange(rawFrom, rawTo string) (from, to time.Time) {
	rawFrom, rawTo = strings.TrimSpace(rawFrom), strings.TrimSpace(rawTo)

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
	return v.lifecycleIn(raw, model.Lifecycles)
}

// lifecycleIn resolves the tab against the ones the report offers; the default is the active tab.
func (v *validator) lifecycleIn(raw string, valid []model.Lifecycle) model.Lifecycle {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return model.LifecycleActive
	}
	for _, l := range valid {
		if raw == string(l) {
			return l
		}
	}
	v.add("lifecycle", "must be one of: "+joinValues(valid))
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

// parseSort resolves sort and order against the report's sort keys. With neither given the default key def
// is used, newest first (descending); naming a sort key without an order means ascending.
func parseSort[K ~string](v *validator, rawKey, rawOrder string, valid []K, def K) (key K, desc bool) {
	rawKey = strings.ToLower(strings.TrimSpace(rawKey))
	rawOrder = strings.ToLower(strings.TrimSpace(rawOrder))

	key, desc = def, rawKey == "" && rawOrder == ""

	if rawKey != "" {
		if k := K(rawKey); contains(valid, k) {
			key = k
		} else {
			v.add("sort", "must be one of: "+joinValues(valid))
		}
	}
	switch rawOrder {
	case "":
	case "asc":
		desc = false
	case "desc":
		desc = true
	default:
		v.add("order", "must be asc or desc")
	}
	return key, desc
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
