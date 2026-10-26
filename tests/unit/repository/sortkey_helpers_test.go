package repository_test

import (
	"fmt"

	"reportsgo/internal/model"
)

// sortKeySQL is the SQL a sort key is expected to order and compare by: text keys are cut to
// model.MaxSortTextLength characters, every other type is used as is.
func sortKeySQL(expr, cast string) string {
	if cast == "text" {
		return fmt.Sprintf("left(%s, %d)", expr, model.MaxSortTextLength)
	}
	return expr
}

// sortKeyPrefix is the start of sortKeySQL, for tests that only know the beginning of a long expression.
func sortKeyPrefix(expr, cast string) string {
	if cast == "text" {
		return "left(" + expr
	}
	return expr
}
