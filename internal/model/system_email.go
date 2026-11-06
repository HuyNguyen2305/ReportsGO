package model

import "time"

// EmailStatus is the state of a system email, matching the email_status enum.
type EmailStatus string

// Email statuses.
const (
	EmailUnconfirmed EmailStatus = "unconfirmed"
	EmailConfirmed   EmailStatus = "confirmed"
	EmailCancelled   EmailStatus = "cancelled"
)

// EmailStatuses lists every valid EmailStatus.
var EmailStatuses = []EmailStatus{EmailUnconfirmed, EmailConfirmed, EmailCancelled}

// EmailType is the kind of a system email, matching the email_type enum.
type EmailType string

// Email types.
const (
	EmailWelcome                 EmailType = "welcome"
	EmailInvoice                 EmailType = "invoice"
	EmailEstimate                EmailType = "estimate"
	EmailAppointmentConfirmation EmailType = "appointment_confirmation"
	EmailAppointmentReminder     EmailType = "appointment_reminder"
	EmailPaymentReceipt          EmailType = "payment_receipt"
	EmailReviewRequest           EmailType = "review_request"
)

// EmailTypes lists every valid EmailType.
var EmailTypes = []EmailType{
	EmailWelcome, EmailInvoice, EmailEstimate, EmailAppointmentConfirmation, EmailAppointmentReminder,
	EmailPaymentReceipt, EmailReviewRequest,
}

// NameFormat is how the report shows the customer's name (the "First name" dropdown of the screen). It changes the
// display only: a customer with no first (or last) name shows the full display name instead.
type NameFormat string

// Name formats.
const (
	NameFirst NameFormat = "first_name"
	NameLast  NameFormat = "last_name"
	NameFull  NameFormat = "full_name"
)

// NameFormats lists every valid NameFormat.
var NameFormats = []NameFormat{NameFirst, NameLast, NameFull}

// SystemEmailSortKey names a column the System Email History report can be sorted by. It is a closed set: the
// repository maps each key to a fixed SQL expression, so no caller-supplied text ever reaches the SQL.
type SystemEmailSortKey string

// System email sort keys.
const (
	SystemEmailSortTimeStamp SystemEmailSortKey = "time_stamp"
	SystemEmailSortCustomer  SystemEmailSortKey = "customer"
	SystemEmailSortEmail     SystemEmailSortKey = "email"
	SystemEmailSortType      SystemEmailSortKey = "email_type"
	SystemEmailSortStatus    SystemEmailSortKey = "status"
)

// SystemEmailSortKeys lists every valid SystemEmailSortKey.
var SystemEmailSortKeys = []SystemEmailSortKey{
	SystemEmailSortTimeStamp, SystemEmailSortCustomer, SystemEmailSortEmail, SystemEmailSortType, SystemEmailSortStatus,
}

// SystemEmailSort is a sort key and direction. Ties are always broken by email id in the same direction.
type SystemEmailSort struct {
	Key  SystemEmailSortKey
	Desc bool
}

// SystemEmailFilter narrows the System Email History report. An empty list means "All".
type SystemEmailFilter struct {
	// SentFrom is the first instant included and SentBefore the first instant excluded. The service builds them from
	// calendar days in the business time zone. A zero value leaves that side open.
	SentFrom, SentBefore time.Time
	Statuses             []EmailStatus
	CustomerIDs          []int64
	// Search matches, case-insensitively and as a substring, the customer name, account number and email address.
	// It is plain text: LIKE wildcards in it are matched literally.
	Search string
}

// SystemEmailCursor identifies the last row of a page: its sort value (as text) and email id.
type SystemEmailCursor struct {
	SortValue string
	ID        int64
}

// SystemEmailQuery is one page request. Limit is the number of rows wanted; the repository returns up to Limit+1 so
// the caller can tell whether another page exists.
type SystemEmailQuery struct {
	Filter     SystemEmailFilter
	NameFormat NameFormat
	Sort       SystemEmailSort
	Limit      int
	After      *SystemEmailCursor
}

// SystemEmailRow is one line of the report.
type SystemEmailRow struct {
	ID           int64
	CustomerID   int64
	CustomerName string // shown in the requested NameFormat
	Email        string
	Type         EmailType
	Status       EmailStatus
	SentAt       time.Time
	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}
