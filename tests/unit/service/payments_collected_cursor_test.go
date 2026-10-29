package service_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

// paymentCollectedSortValue is what the repository's sort_value column looks like for each key.
var paymentCollectedSortValue = map[model.PaymentCollectedSortKey]string{
	model.PaymentCollectedSortPaymentFor:   "85004",
	model.PaymentCollectedSortCustomer:     "william saliba",
	model.PaymentCollectedSortMethod:       "card",
	model.PaymentCollectedSortServiceState: "tx",
	model.PaymentCollectedSortStaff:        "thọ nguyễn quang",
	model.PaymentCollectedSortDate:         "2026-10-15",
	model.PaymentCollectedSortAmount:       "1234.50",
	model.PaymentCollectedSortWithoutTax:   "1121.00",
}

func TestPaymentCollectedCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	require.Len(t, paymentCollectedSortValue, len(model.PaymentCollectedSortKeys), "every sort key needs a sample value")

	for _, key := range model.PaymentCollectedSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.PaymentCollectedSort{Key: key, Desc: desc}
				r := model.PaymentCollectedRow{ID: 4242, SortValue: paymentCollectedSortValue[key]}

				raw := service.EncodePaymentCollectedCursor(sort, r)
				got, err := service.DecodePaymentCollectedCursor(raw, sort)

				require.NoError(t, err)
				require.Equal(t, &model.PaymentCollectedCursor{SortValue: paymentCollectedSortValue[key], ID: 4242}, got)
			})
		}
	}
}

func TestPaymentCollectedCursorAcceptsTheCreditSortValueAndAnEmptyOne(t *testing.T) {
	for _, tt := range []struct {
		key   model.PaymentCollectedSortKey
		value string
	}{
		{model.PaymentCollectedSortPaymentFor, "credit"},
		{model.PaymentCollectedSortServiceState, ""},
		{model.PaymentCollectedSortStaff, ""},
	} {
		t.Run(string(tt.key)+" "+tt.value, func(t *testing.T) {
			sort := model.PaymentCollectedSort{Key: tt.key}
			raw := service.EncodePaymentCollectedCursor(sort, model.PaymentCollectedRow{ID: 5, SortValue: tt.value})

			got, err := service.DecodePaymentCollectedCursor(raw, sort)

			require.NoError(t, err)
			require.Equal(t, tt.value, got.SortValue)
		})
	}
}

func TestPaymentCollectedCursorRejectsInvalidCursors(t *testing.T) {
	dateAsc := model.PaymentCollectedSort{Key: model.PaymentCollectedSortDate}
	amountAsc := model.PaymentCollectedSort{Key: model.PaymentCollectedSortAmount}
	withoutTaxAsc := model.PaymentCollectedSort{Key: model.PaymentCollectedSortWithoutTax}

	cursorWith := func(key, value string) string {
		return encode(`{"k":"` + key + `","d":false,"v":"` + value + `","id":1}`)
	}

	tests := []struct {
		name string
		raw  string
		sort model.PaymentCollectedSort
	}{
		{"not base64", "!!!", dateAsc},
		{"not json", encode("hello"), dateAsc},
		{"unknown field", encode(`{"k":"date","d":false,"v":"2026-10-01","id":1,"x":1}`), dateAsc},
		{"id missing", encode(`{"k":"date","d":false,"v":"2026-10-01"}`), dateAsc},
		{"another sort key", cursorWith("amount", "10.00"), dateAsc},
		{"another direction", encode(`{"k":"date","d":true,"v":"2026-10-01","id":1}`), dateAsc},
		{"bad date", cursorWith("date", "2026-02-30"), dateAsc},
		{"amount that is not money", cursorWith("amount", "abc"), amountAsc},
		{"without tax that is not money", cursorWith("without_tax", "1.234"), withoutTaxAsc},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.DecodePaymentCollectedCursor(tt.raw, tt.sort)

			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
		})
	}
}
