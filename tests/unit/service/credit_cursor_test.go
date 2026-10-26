package service_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

// creditSortValue is what the repository's sort_value column looks like for each key.
var creditSortValue = map[model.CreditSortKey]string{
	model.CreditSortCustomer:      "william saliba",
	model.CreditSortAccountNumber: "63706",
	model.CreditSortMethod:        "card",
	model.CreditSortAmount:        "1234.50",
	model.CreditSortDate:          "2026-10-15",
	model.CreditSortCreatedBy:     "thọ nguyễn quang",
}

func TestCreditCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	require.Len(t, creditSortValue, len(model.CreditSortKeys), "every sort key needs a sample value")

	for _, key := range model.CreditSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.CreditSort{Key: key, Desc: desc}
				r := model.CreditRow{ID: 4242, SortValue: creditSortValue[key]}

				raw := service.EncodeCreditCursor(sort, r)
				got, err := service.DecodeCreditCursor(raw, sort)

				require.NoError(t, err)
				require.Equal(t, &model.CreditCursor{SortValue: creditSortValue[key], ID: 4242}, got)
			})
		}
	}
}

func TestCreditCursorAcceptsAnEmptyCreator(t *testing.T) {
	sort := model.CreditSort{Key: model.CreditSortCreatedBy}
	raw := service.EncodeCreditCursor(sort, model.CreditRow{ID: 5, SortValue: ""})

	got, err := service.DecodeCreditCursor(raw, sort)

	require.NoError(t, err)
	require.Equal(t, "", got.SortValue)
}

func TestCreditCursorRejectsInvalidCursors(t *testing.T) {
	dateAsc := model.CreditSort{Key: model.CreditSortDate}
	amountAsc := model.CreditSort{Key: model.CreditSortAmount}
	customerAsc := model.CreditSort{Key: model.CreditSortCustomer}

	cursorWith := func(key, value string) string {
		return encode(`{"k":"` + key + `","d":false,"v":"` + value + `","id":1}`)
	}

	tests := []struct {
		name       string
		raw        string
		sort       model.CreditSort
		wantReason string
	}{
		{"empty", "", dateAsc, "not a valid cursor"},
		{"not base64", "!!!not-base64!!!", dateAsc, "not a valid cursor"},
		{"not json", encode("hello"), dateAsc, "not a valid cursor"},
		{"id zero", encode(`{"k":"credit_date","d":false,"v":"2026-10-15","id":0}`), dateAsc, "not a valid cursor"},
		{"id negative", encode(`{"k":"credit_date","d":false,"v":"2026-10-15","id":-5}`), dateAsc, "not a valid cursor"},

		{"other sort key", cursorWith("method", "card"), dateAsc, "does not match the requested sort"},
		{"other direction", encode(`{"k":"credit_date","d":true,"v":"2026-10-15","id":1}`), dateAsc, "does not match the requested sort"},
		{"another report's key", cursorWith("payment_date", "2026-10-15"), dateAsc, "does not match the requested sort"},

		{"date with a time", cursorWith("credit_date", "2026-10-15T00:00:00Z"), dateAsc, "not a valid cursor"},
		{"date with slashes", cursorWith("credit_date", "2026/10/15"), dateAsc, "not a valid cursor"},
		{"date impossible", cursorWith("credit_date", "2026-02-30"), dateAsc, "not a valid cursor"},
		{"date empty", cursorWith("credit_date", ""), dateAsc, "not a valid cursor"},
		{"date with injection", cursorWith("credit_date", "2026-10-15'; DROP TABLE credits;--"), dateAsc, "not a valid cursor"},

		{"amount not a number", cursorWith("amount", "lots"), amountAsc, "not a valid cursor"},
		{"amount with three decimals", cursorWith("amount", "1.234"), amountAsc, "not a valid cursor"},
		{"amount empty", cursorWith("amount", ""), amountAsc, "not a valid cursor"},
		{"amount with injection", cursorWith("amount", "1; DROP TABLE credits"), amountAsc, "not a valid cursor"},

		{"text with NUL", encode(`{"k":"customer","d":false,"v":"a\u0000b","id":1}`), customerAsc, "not a valid cursor"},
		{"text too long", cursorWith("customer", strings.Repeat("x", model.MaxSortTextLength+1)), customerAsc, "not a valid cursor"},
		{"cursor too long", strings.Repeat("A", 4097), customerAsc, "too long"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.DecodeCreditCursor(tt.raw, tt.sort)

			require.Nil(t, got)
			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
			require.Contains(t, ce.Reason, tt.wantReason)
		})
	}
}

func TestCreditCursorAcceptsBoundaryValues(t *testing.T) {
	t.Run("dates", func(t *testing.T) {
		sort := model.CreditSort{Key: model.CreditSortDate}
		for _, v := range []string{"0001-01-01", "9999-12-31", "2028-02-29"} {
			raw := service.EncodeCreditCursor(sort, model.CreditRow{ID: 1, SortValue: v})
			got, err := service.DecodeCreditCursor(raw, sort)
			require.NoError(t, err, v)
			require.Equal(t, v, got.SortValue)
		}
	})

	t.Run("amounts, including zero and negative", func(t *testing.T) {
		sort := model.CreditSort{Key: model.CreditSortAmount}
		for _, v := range []string{"0.00", "0.01", "99999999.99", "-5.00"} {
			raw := service.EncodeCreditCursor(sort, model.CreditRow{ID: 1, SortValue: v})
			got, err := service.DecodeCreditCursor(raw, sort)
			require.NoError(t, err, v)
			require.Equal(t, v, got.SortValue)
		}
	})

	t.Run("largest id", func(t *testing.T) {
		sort := model.CreditSort{Key: model.CreditSortMethod}
		raw := service.EncodeCreditCursor(sort, model.CreditRow{ID: 9223372036854775807, SortValue: "cash"})
		got, err := service.DecodeCreditCursor(raw, sort)
		require.NoError(t, err)
		require.EqualValues(t, int64(9223372036854775807), got.ID)
	})
}

func TestCreditCursorsDoNotCrossBetweenReports(t *testing.T) {
	credit := service.EncodeCreditCursor(model.CreditSort{Key: model.CreditSortDate},
		model.CreditRow{ID: 3, SortValue: "2026-10-15"})

	// the date cursor of another report is bound to that report's key name
	_, err := service.DecodePaymentCursor(credit, model.PaymentSort{Key: model.PaymentSortDate})
	require.Error(t, err)
	_, err = service.DecodeInvoiceCursor(credit, model.InvoiceSort{Key: model.SortDate})
	require.Error(t, err)

	payment := service.EncodePaymentCursor(model.PaymentSort{Key: model.PaymentSortDate},
		model.PaymentRow{ID: 3, SortValue: "2026-10-15"})
	_, err = service.DecodeCreditCursor(payment, model.CreditSort{Key: model.CreditSortDate})
	require.Error(t, err)
}
