package service_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

// paymentSortValue is what the repository's sort_value column looks like for each key.
var paymentSortValue = map[model.PaymentSortKey]string{
	model.PaymentSortPaymentFor: "85004",
	model.PaymentSortCustomer:   "william saliba",
	model.PaymentSortMethod:     "card",
	model.PaymentSortDate:       "2026-10-15",
	model.PaymentSortAmount:     "1234.50",
	model.PaymentSortStatus:     "refunded",
}

func TestPaymentCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	require.Len(t, paymentSortValue, len(model.PaymentSortKeys), "every sort key needs a sample value")

	for _, key := range model.PaymentSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.PaymentSort{Key: key, Desc: desc}
				r := model.PaymentRow{ID: 4242, SortValue: paymentSortValue[key]}

				raw := service.EncodePaymentCursor(sort, r)
				got, err := service.DecodePaymentCursor(raw, sort)

				require.NoError(t, err)
				require.Equal(t, &model.PaymentCursor{SortValue: paymentSortValue[key], ID: 4242}, got)
			})
		}
	}
}

func TestPaymentCursorAcceptsTheCreditSortValue(t *testing.T) {
	sort := model.PaymentSort{Key: model.PaymentSortPaymentFor}
	raw := service.EncodePaymentCursor(sort, model.PaymentRow{ID: 5, SortValue: "credit"})

	got, err := service.DecodePaymentCursor(raw, sort)

	require.NoError(t, err)
	require.Equal(t, "credit", got.SortValue)
}

func TestPaymentCursorRejectsInvalidCursors(t *testing.T) {
	dateAsc := model.PaymentSort{Key: model.PaymentSortDate}
	amountAsc := model.PaymentSort{Key: model.PaymentSortAmount}
	customerAsc := model.PaymentSort{Key: model.PaymentSortCustomer}

	cursorWith := func(key, value string) string {
		return encode(`{"k":"` + key + `","d":false,"v":"` + value + `","id":1}`)
	}

	tests := []struct {
		name       string
		raw        string
		sort       model.PaymentSort
		wantReason string
	}{
		{"empty", "", dateAsc, "not a valid cursor"},
		{"not base64", "!!!not-base64!!!", dateAsc, "not a valid cursor"},
		{"not json", encode("hello"), dateAsc, "not a valid cursor"},
		{"id zero", encode(`{"k":"payment_date","d":false,"v":"2026-10-15","id":0}`), dateAsc, "not a valid cursor"},
		{"id negative", encode(`{"k":"payment_date","d":false,"v":"2026-10-15","id":-5}`), dateAsc, "not a valid cursor"},

		{"other sort key", cursorWith("status", "paid"), dateAsc, "does not match the requested sort"},
		{"other direction", encode(`{"k":"payment_date","d":true,"v":"2026-10-15","id":1}`), dateAsc, "does not match the requested sort"},
		{"another report's key", cursorWith("document_number", "5979"), customerAsc, "does not match the requested sort"},

		{"date with a time", cursorWith("payment_date", "2026-10-15T00:00:00Z"), dateAsc, "not a valid cursor"},
		{"date with slashes", cursorWith("payment_date", "2026/10/15"), dateAsc, "not a valid cursor"},
		{"date impossible", cursorWith("payment_date", "2026-02-30"), dateAsc, "not a valid cursor"},
		{"date without zero padding", cursorWith("payment_date", "2026-1-5"), dateAsc, "not a valid cursor"},
		{"date empty", cursorWith("payment_date", ""), dateAsc, "not a valid cursor"},
		{"date with injection", cursorWith("payment_date", "2026-10-15'; DROP TABLE payments;--"), dateAsc, "not a valid cursor"},

		{"amount not a number", cursorWith("amount", "lots"), amountAsc, "not a valid cursor"},
		{"amount with three decimals", cursorWith("amount", "1.234"), amountAsc, "not a valid cursor"},
		{"amount empty", cursorWith("amount", ""), amountAsc, "not a valid cursor"},
		{"amount with injection", cursorWith("amount", "1; DROP TABLE payments"), amountAsc, "not a valid cursor"},

		{"text with NUL", encode(`{"k":"customer","d":false,"v":"a\u0000b","id":1}`), customerAsc, "not a valid cursor"},
		{"text too long", cursorWith("customer", strings.Repeat("x", 1001)), customerAsc, "not a valid cursor"},
		{"cursor too long", strings.Repeat("A", 4097), customerAsc, "too long"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.DecodePaymentCursor(tt.raw, tt.sort)

			require.Nil(t, got)
			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
			require.Contains(t, ce.Reason, tt.wantReason)
		})
	}
}

func TestPaymentCursorAcceptsBoundaryValues(t *testing.T) {
	t.Run("dates", func(t *testing.T) {
		sort := model.PaymentSort{Key: model.PaymentSortDate}
		for _, v := range []string{"0001-01-01", "9999-12-31", "2028-02-29"} {
			raw := service.EncodePaymentCursor(sort, model.PaymentRow{ID: 1, SortValue: v})
			got, err := service.DecodePaymentCursor(raw, sort)
			require.NoError(t, err, v)
			require.Equal(t, v, got.SortValue)
		}
	})

	t.Run("amounts, including zero and negative", func(t *testing.T) {
		sort := model.PaymentSort{Key: model.PaymentSortAmount}
		for _, v := range []string{"0.00", "0.01", "99999999.99", "-5.00"} {
			raw := service.EncodePaymentCursor(sort, model.PaymentRow{ID: 1, SortValue: v})
			got, err := service.DecodePaymentCursor(raw, sort)
			require.NoError(t, err, v)
			require.Equal(t, v, got.SortValue)
		}
	})

	t.Run("largest id", func(t *testing.T) {
		sort := model.PaymentSort{Key: model.PaymentSortStatus}
		raw := service.EncodePaymentCursor(sort, model.PaymentRow{ID: 9223372036854775807, SortValue: "paid"})
		got, err := service.DecodePaymentCursor(raw, sort)
		require.NoError(t, err)
		require.EqualValues(t, int64(9223372036854775807), got.ID)
	})
}

func TestPaymentCursorsDoNotCrossBetweenReports(t *testing.T) {
	payment := service.EncodePaymentCursor(model.PaymentSort{Key: model.PaymentSortDate},
		model.PaymentRow{ID: 3, SortValue: "2026-10-15"})
	_, err := service.DecodeDocumentCursor(payment, model.DocumentSort{Key: model.DocumentSortCreatedAt})
	require.Error(t, err)
	_, err = service.DecodeInvoiceCursor(payment, model.InvoiceSort{Key: model.SortDate})
	require.Error(t, err)
}
