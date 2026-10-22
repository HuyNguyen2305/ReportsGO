package repository

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"reportsgo/internal/model"
)

// ErrInvalidQuery is returned when a report query cannot be built (unknown sort key or lifecycle, bad limit).
var ErrInvalidQuery = errors.New("invalid report query")

// Statement is a SQL text with its bind arguments.
type Statement struct {
	SQL  string
	Args []any
}

// sortSpec is the SQL expression behind a sort key and the type its text form is cast back to for keyset
// comparison. Expressions never yield NULL, so row comparisons in the keyset condition are always defined.
type sortSpec struct {
	expr string
	cast string
	// text is the SQL that renders the sort value for a cursor, for the keys whose default text form is not
	// stable or precise enough (timestamps). Empty means "expr cast to text".
	text string
}

// sortText returns the SQL for this key's cursor value.
func (s sortSpec) sortText() string {
	if s.text != "" {
		return s.text
	}
	return s.expr
}

// Values of billing_documents.parent_type. They are constants, so they are safe to embed in SQL text.
const (
	parentInvoice  = "invoice"
	parentEstimate = "estimate"
)

// whereBuilder collects WHERE conditions and their bind arguments. Conditions are built only from constant
// fragments and "$n" placeholders; every caller-supplied value goes into args.
type whereBuilder struct {
	conds []string
	args  []any
}

// arg registers a bind argument and returns its placeholder.
func (w *whereBuilder) arg(v any) string {
	w.args = append(w.args, v)
	return fmt.Sprintf("$%d", len(w.args))
}

func (w *whereBuilder) add(cond string) { w.conds = append(w.conds, cond) }

func (w *whereBuilder) where() string {
	if len(w.conds) == 0 {
		return ""
	}
	return "\nWHERE " + strings.Join(w.conds, "\n  AND ")
}

// commonFilter is what every billing-document report filters on. Statuses are already plain strings because
// invoices and estimates have different status types.
type commonFilter struct {
	parent           string
	dateFrom, dateTo time.Time
	lifecycle        model.Lifecycle
	statuses         []string
	signatureStatus  []model.SignatureStatus
}

// applyCommonFilter adds the conditions shared by the invoice and estimate reports. alias is the
// billing_documents alias used in the query.
func applyCommonFilter(w *whereBuilder, alias string, c commonFilter) error {
	// billing_documents holds invoices and estimates side by side; each report sees only its own type.
	w.add(alias + ".parent_type = '" + c.parent + "'")

	switch c.lifecycle {
	case model.LifecycleActive, "":
		w.add(alias + ".deleted_at IS NULL AND " + alias + ".archived_at IS NULL")
	case model.LifecycleArchived:
		w.add(alias + ".deleted_at IS NULL AND " + alias + ".archived_at IS NOT NULL")
	case model.LifecycleDeleted:
		w.add(alias + ".deleted_at IS NOT NULL")
	default:
		return fmt.Errorf("%w: unknown lifecycle %q", ErrInvalidQuery, c.lifecycle)
	}

	if !c.dateFrom.IsZero() {
		w.add(alias + ".document_date >= " + w.arg(c.dateFrom) + "::date")
	}
	if !c.dateTo.IsZero() {
		w.add(alias + ".document_date <= " + w.arg(c.dateTo) + "::date")
	}
	if len(c.statuses) > 0 {
		w.add(alias + ".status::text = ANY(" + w.arg(c.statuses) + "::text[])")
	}
	if len(c.signatureStatus) > 0 {
		w.add(alias + ".signature_status::text = ANY(" + w.arg(toStrings(c.signatureStatus)) + "::text[])")
	}
	return nil
}

// applySearch adds the case-insensitive substring search over document number, account number, customer name
// and billing email. The customer must be joined as "c".
func applySearch(w *whereBuilder, alias, search string) {
	applySearchOn(w, search, alias+".document_number", "c.account_number", "c.display_name", alias+".billing_email")
}

// applySearchOn adds a case-insensitive substring match of search against any of the given SQL columns.
// The columns are constant fragments chosen by the report, never caller input; the term is a bind argument
// with LIKE wildcards escaped so it is matched literally.
func applySearchOn(w *whereBuilder, search string, columns ...string) {
	if search == "" {
		return
	}
	p := w.arg("%" + escapeLike(search) + "%")
	matches := make([]string, len(columns))
	for i, col := range columns {
		matches[i] = col + ` ILIKE ` + p + ` ESCAPE '\'`
	}
	w.add("(" + strings.Join(matches, " OR ") + ")")
}

// applyKeyset adds the condition that keeps only rows after the cursor in the given direction.
// The value travels as text and is cast to the key's type.
func applyKeyset(w *whereBuilder, alias string, spec sortSpec, desc bool, sortValue string, id int64) {
	_, cmp := direction(desc)
	w.add(fmt.Sprintf("(%s, %s.id) %s ((%s::text)::%s, %s::bigint)",
		spec.expr, alias, cmp, w.arg(sortValue), spec.cast, w.arg(id)))
}

// orderAndLimit returns the ORDER BY and LIMIT clauses; ties are broken by id in the same direction.
// limit is an already registered placeholder.
func orderAndLimit(alias string, spec sortSpec, desc bool, limit string) string {
	dir, _ := direction(desc)
	return "\nORDER BY " + spec.expr + " " + dir + ", " + alias + ".id " + dir + "\nLIMIT " + limit
}

// direction returns the SQL sort direction and the keyset comparison operator that follows it.
func direction(desc bool) (dir, cmp string) {
	if desc {
		return "DESC", "<"
	}
	return "ASC", ">"
}

// escapeLike makes s safe to embed in a LIKE pattern that uses backslash as the escape character,
// so user input is matched literally.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func toStrings[T ~string](in []T) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}
