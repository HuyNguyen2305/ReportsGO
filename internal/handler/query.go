package handler

import (
	"errors"
	"net/url"
	"sort"
	"strings"

	"reportsgo/internal/model"
)

var errMalformedQuery = errors.New("malformed query string")

// maxEchoedParamName bounds how much of an unknown parameter name is repeated back in an error.
const maxEchoedParamName = 64

// parseStrictQuery parses a raw query string for an endpoint that accepts exactly the given parameters. It is
// strict on purpose: an unknown parameter (a typo such as "statuss") or a single-value parameter sent twice is an
// error, instead of silently returning unfiltered data. Multi-value parameters may repeat. A malformed query
// string (bad percent-encoding) gives errMalformedQuery; every other problem is one model.InvalidInput error,
// sorted by parameter name because map order is random.
func parseStrictQuery(rawQuery string, single, multi []string) (url.Values, error) {
	// url.Query() would silently drop malformed pairs; ParseQuery reports them.
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, errMalformedQuery
	}

	var problems []model.FieldError
	for name, vals := range values {
		switch {
		case contains(single, name):
			if len(vals) > 1 {
				problems = append(problems, model.FieldError{Field: name, Message: "must be given only once"})
			}
		case contains(multi, name):
		default:
			problems = append(problems, model.FieldError{Field: truncate(name, maxEchoedParamName), Message: "is not a supported parameter"})
		}
	}
	if len(problems) > 0 {
		sort.Slice(problems, func(i, j int) bool { return problems[i].Field < problems[j].Field })
		return nil, model.InvalidInput(problems...)
	}
	return values, nil
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
