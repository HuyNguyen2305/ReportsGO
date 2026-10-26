package model

import "time"

// RecurringStatus is the state of a recurring invoice, matching the recurring_status enum.
type RecurringStatus string

// Recurring invoice statuses.
const (
	RecurringActive RecurringStatus = "active"
	RecurringPaused RecurringStatus = "paused"
	RecurringFailed RecurringStatus = "failed"
	RecurringEnded  RecurringStatus = "ended"
)

// RecurringStatuses lists every valid RecurringStatus.
var RecurringStatuses = []RecurringStatus{RecurringActive, RecurringPaused, RecurringFailed, RecurringEnded}

// RecurringAction is what happens when a recurring invoice comes due, matching the recurring_action enum.
type RecurringAction string

// Recurring invoice actions.
const (
	ActionSendByEmail RecurringAction = "send_by_email"
	ActionCreateOnly  RecurringAction = "create_only"
)

// RecurringActions lists every valid RecurringAction.
var RecurringActions = []RecurringAction{ActionSendByEmail, ActionCreateOnly}

// FrequencyUnit is the unit of a repeat interval, matching the frequency_unit enum.
type FrequencyUnit string

// Frequency units.
const (
	FrequencyDay   FrequencyUnit = "day"
	FrequencyWeek  FrequencyUnit = "week"
	FrequencyMonth FrequencyUnit = "month"
	FrequencyYear  FrequencyUnit = "year"
)

// FrequencyUnits lists every valid FrequencyUnit.
var FrequencyUnits = []FrequencyUnit{FrequencyDay, FrequencyWeek, FrequencyMonth, FrequencyYear}

// RecurringSortKey names a column the recurring invoice report can be sorted by. It is a closed set: the
// repository maps each key to a fixed SQL expression, so no caller-supplied text ever reaches the SQL.
type RecurringSortKey string

// Recurring invoice sort keys.
const (
	RecurringSortCustomer        RecurringSortKey = "customer"
	RecurringSortAccountNumber   RecurringSortKey = "account_number"
	RecurringSortRepeats         RecurringSortKey = "repeats"
	RecurringSortAction          RecurringSortKey = "action"
	RecurringSortStatus          RecurringSortKey = "status"
	RecurringSortTotalValue      RecurringSortKey = "total_value"
	RecurringSortStartedOn       RecurringSortKey = "started_on"
	RecurringSortEndsOn          RecurringSortKey = "ends_on"
	RecurringSortNextInvoiceDate RecurringSortKey = "next_invoice_date"
)

// RecurringSortKeys lists every valid RecurringSortKey.
var RecurringSortKeys = []RecurringSortKey{
	RecurringSortCustomer, RecurringSortAccountNumber, RecurringSortRepeats, RecurringSortAction,
	RecurringSortStatus, RecurringSortTotalValue, RecurringSortStartedOn, RecurringSortEndsOn,
	RecurringSortNextInvoiceDate,
}

// RecurringSort is a sort key and direction. Ties are always broken by id in the same direction.
type RecurringSort struct {
	Key  RecurringSortKey
	Desc bool
}

// RecurringFilter narrows the recurring invoice report. An empty list means "All". Recurring invoices cannot be
// deleted or archived, so there is no tab.
type RecurringFilter struct {
	// DateFrom and DateTo bound the start date, both inclusive. A zero value leaves that side open.
	DateFrom, DateTo time.Time
	Statuses         []RecurringStatus
	Actions          []RecurringAction
	// Search matches, case-insensitively and as a substring, the customer name, account number and the number
	// of the next invoice. It is plain text: LIKE wildcards in it are matched literally.
	Search string
}

// RecurringCursor identifies the last row of a page: its sort value (as text) and id.
type RecurringCursor struct {
	SortValue string
	ID        int64
}

// RecurringQuery is one page request. Limit is the number of rows wanted; the repository returns up to Limit+1
// so the caller can tell whether another page exists.
type RecurringQuery struct {
	Filter RecurringFilter
	Sort   RecurringSort
	Limit  int
	After  *RecurringCursor
}

// RecurringRow is one line of the recurring invoice report.
type RecurringRow struct {
	ID            int64
	CustomerID    int64
	CustomerName  string
	AccountNumber string

	// The repeat rule. Weekday is 0 (Sunday) to 6 (Saturday) and Occurrences is the number of invoices to
	// create; either is nil when not set.
	Unit        FrequencyUnit
	Interval    int
	Weekday     *int
	Occurrences *int

	Action     RecurringAction
	Status     RecurringStatus
	TotalValue Money
	StartedOn  time.Time
	EndsOn     *time.Time

	// The next invoice. Each part is nil when it is not known: a recurring invoice can have a due date without
	// an invoice created yet, and an ended one has neither.
	NextInvoiceID     *int64
	NextInvoiceNumber *string
	NextInvoiceDate   *time.Time

	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}

// RecurringPage is one page of the report read from a single database snapshot. Summary is nil unless it was
// requested, and when set it totals exactly the filtered set the rows were taken from.
type RecurringPage struct {
	Rows    []RecurringRow
	Summary *RecurringSummary
}

// RecurringSummary totals the whole filtered set, not just one page. Total adds up every recurring invoice in
// scope, whatever its status.
type RecurringSummary struct {
	Count int64
	Total Money
}
