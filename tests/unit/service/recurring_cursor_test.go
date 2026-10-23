package service_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

// recurringSortValue is what the repository's sort_value column looks like for each key.
var recurringSortValue = map[model.RecurringSortKey]string{
	model.RecurringSortCustomer:        "william saliba",
	model.RecurringSortAccountNumber:   "63706",
	model.RecurringSortRepeats:         "0000000014",
	model.RecurringSortAction:          "send_by_email",
	model.RecurringSortStatus:          "active",
	model.RecurringSortTotalValue:      "1234.50",
	model.RecurringSortStartedOn:       "2026-10-15",
	model.RecurringSortEndsOn:          "9999-12-31",
	model.RecurringSortNextInvoiceDate: "2026-09-24",
}

func TestRecurringCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	require.Len(t, recurringSortValue, len(model.RecurringSortKeys), "every sort key needs a sample value")

	for _, key := range model.RecurringSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.RecurringSort{Key: key, Desc: desc}
				r := model.RecurringRow{ID: 4242, SortValue: recurringSortValue[key]}

				raw := service.EncodeRecurringCursor(sort, r)
				got, err := service.DecodeRecurringCursor(raw, sort)

				require.NoError(t, err)
				require.Equal(t, &model.RecurringCursor{SortValue: recurringSortValue[key], ID: 4242}, got)
			})
		}
	}
}

func TestRecurringCursorRejectsInvalidCursors(t *testing.T) {
	startedAsc := model.RecurringSort{Key: model.RecurringSortStartedOn}
	endsAsc := model.RecurringSort{Key: model.RecurringSortEndsOn}
	nextAsc := model.RecurringSort{Key: model.RecurringSortNextInvoiceDate}
	totalAsc := model.RecurringSort{Key: model.RecurringSortTotalValue}
	customerAsc := model.RecurringSort{Key: model.RecurringSortCustomer}

	cursorWith := func(key, value string) string {
		return encode(`{"k":"` + key + `","d":false,"v":"` + value + `","id":1}`)
	}

	tests := []struct {
		name       string
		raw        string
		sort       model.RecurringSort
		wantReason string
	}{
		{"empty", "", startedAsc, "not a valid cursor"},
		{"not base64", "!!!not-base64!!!", startedAsc, "not a valid cursor"},
		{"not json", encode("hello"), startedAsc, "not a valid cursor"},
		{"id zero", encode(`{"k":"started_on","d":false,"v":"2026-10-15","id":0}`), startedAsc, "not a valid cursor"},
		{"id negative", encode(`{"k":"started_on","d":false,"v":"2026-10-15","id":-5}`), startedAsc, "not a valid cursor"},

		{"other sort key", cursorWith("status", "active"), startedAsc, "does not match the requested sort"},
		{"other date key", cursorWith("ends_on", "2026-10-15"), startedAsc, "does not match the requested sort"},
		{"other direction", encode(`{"k":"started_on","d":true,"v":"2026-10-15","id":1}`), startedAsc, "does not match the requested sort"},
		{"another report's key", cursorWith("credit_date", "2026-10-15"), startedAsc, "does not match the requested sort"},

		{"started_on with a time", cursorWith("started_on", "2026-10-15T00:00:00Z"), startedAsc, "not a valid cursor"},
		{"started_on impossible", cursorWith("started_on", "2026-02-30"), startedAsc, "not a valid cursor"},
		{"started_on empty", cursorWith("started_on", ""), startedAsc, "not a valid cursor"},
		{"ends_on not a date", cursorWith("ends_on", "never"), endsAsc, "not a valid cursor"},
		{"ends_on empty", cursorWith("ends_on", ""), endsAsc, "not a valid cursor"},
		{"next_invoice_date with injection", cursorWith("next_invoice_date", "2026-10-15'; DROP TABLE recurring_invoices;--"), nextAsc, "not a valid cursor"},

		{"total not a number", cursorWith("total_value", "lots"), totalAsc, "not a valid cursor"},
		{"total with three decimals", cursorWith("total_value", "1.234"), totalAsc, "not a valid cursor"},
		{"total empty", cursorWith("total_value", ""), totalAsc, "not a valid cursor"},

		{"text with NUL", encode(`{"k":"customer","d":false,"v":"a\u0000b","id":1}`), customerAsc, "not a valid cursor"},
		{"text too long", cursorWith("customer", strings.Repeat("x", 1001)), customerAsc, "not a valid cursor"},
		{"cursor too long", strings.Repeat("A", 4097), customerAsc, "too long"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.DecodeRecurringCursor(tt.raw, tt.sort)

			require.Nil(t, got)
			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
			require.Contains(t, ce.Reason, tt.wantReason)
		})
	}
}

func TestRecurringCursorAcceptsBoundaryValues(t *testing.T) {
	t.Run("dates, including the sentinel for a missing date", func(t *testing.T) {
		for _, key := range []model.RecurringSortKey{model.RecurringSortStartedOn, model.RecurringSortEndsOn, model.RecurringSortNextInvoiceDate} {
			sort := model.RecurringSort{Key: key}
			for _, v := range []string{"0001-01-01", "9999-12-31", "2028-02-29"} {
				raw := service.EncodeRecurringCursor(sort, model.RecurringRow{ID: 1, SortValue: v})
				got, err := service.DecodeRecurringCursor(raw, sort)
				require.NoError(t, err, "%s %s", key, v)
				require.Equal(t, v, got.SortValue)
			}
		}
	})

	t.Run("amounts, including zero", func(t *testing.T) {
		sort := model.RecurringSort{Key: model.RecurringSortTotalValue}
		for _, v := range []string{"0.00", "0.01", "99999999.99"} {
			raw := service.EncodeRecurringCursor(sort, model.RecurringRow{ID: 1, SortValue: v})
			got, err := service.DecodeRecurringCursor(raw, sort)
			require.NoError(t, err, v)
			require.Equal(t, v, got.SortValue)
		}
	})

	t.Run("largest id", func(t *testing.T) {
		sort := model.RecurringSort{Key: model.RecurringSortStatus}
		raw := service.EncodeRecurringCursor(sort, model.RecurringRow{ID: 9223372036854775807, SortValue: "active"})
		got, err := service.DecodeRecurringCursor(raw, sort)
		require.NoError(t, err)
		require.EqualValues(t, int64(9223372036854775807), got.ID)
	})
}

func TestRecurringCursorsDoNotCrossBetweenReports(t *testing.T) {
	recurring := service.EncodeRecurringCursor(model.RecurringSort{Key: model.RecurringSortStartedOn},
		model.RecurringRow{ID: 3, SortValue: "2026-10-15"})

	_, err := service.DecodeCreditCursor(recurring, model.CreditSort{Key: model.CreditSortDate})
	require.Error(t, err)
	_, err = service.DecodePaymentCursor(recurring, model.PaymentSort{Key: model.PaymentSortDate})
	require.Error(t, err)

	credit := service.EncodeCreditCursor(model.CreditSort{Key: model.CreditSortDate},
		model.CreditRow{ID: 3, SortValue: "2026-10-15"})
	_, err = service.DecodeRecurringCursor(credit, model.RecurringSort{Key: model.RecurringSortStartedOn})
	require.Error(t, err)
}
