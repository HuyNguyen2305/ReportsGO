package service_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

// totalSaleSortValue is what the repository's sort_value column looks like for each key.
var totalSaleSortValue = map[model.TotalSaleSortKey]string{
	model.TotalSaleSortInvoiceNumber: "85004",
	model.TotalSaleSortCustomer:      "william saliba",
	model.TotalSaleSortDate:          "2026-10-15",
	model.TotalSaleSortStatus:        "partial",
	model.TotalSaleSortAmount:        "1234.50",
	model.TotalSaleSortWithoutTax:    "1121.00",
}

func TestTotalSaleCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	require.Len(t, totalSaleSortValue, len(model.TotalSaleSortKeys), "every sort key needs a sample value")

	for _, key := range model.TotalSaleSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.TotalSaleSort{Key: key, Desc: desc}
				r := model.TotalSaleRow{ID: 4242, SortValue: totalSaleSortValue[key]}

				raw := service.EncodeTotalSaleCursor(sort, r)
				got, err := service.DecodeTotalSaleCursor(raw, sort)

				require.NoError(t, err)
				require.Equal(t, &model.TotalSaleCursor{SortValue: totalSaleSortValue[key], ID: 4242}, got)
			})
		}
	}
}

func TestTotalSaleCursorRejectsInvalidCursors(t *testing.T) {
	dateAsc := model.TotalSaleSort{Key: model.TotalSaleSortDate}
	amountAsc := model.TotalSaleSort{Key: model.TotalSaleSortAmount}

	cursorWith := func(key, value string) string {
		return encode(`{"k":"` + key + `","d":false,"v":"` + value + `","id":1}`)
	}

	tests := []struct {
		name string
		raw  string
		sort model.TotalSaleSort
	}{
		{"not base64", "!!!", dateAsc},
		{"not json", encode("hello"), dateAsc},
		{"unknown field", encode(`{"k":"date","d":false,"v":"2026-10-01","id":1,"x":1}`), dateAsc},
		{"trailing data", encode(`{"k":"date","d":false,"v":"2026-10-01","id":1}{}`), dateAsc},
		{"id missing", encode(`{"k":"date","d":false,"v":"2026-10-01"}`), dateAsc},
		{"another sort key", cursorWith("amount", "10.00"), dateAsc},
		{"another direction", encode(`{"k":"date","d":true,"v":"2026-10-01","id":1}`), dateAsc},
		{"bad date", cursorWith("date", "2026-02-30"), dateAsc},
		{"date in the wrong layout", cursorWith("date", "10/01/2026"), dateAsc},
		{"amount that is not money", cursorWith("amount", "abc"), amountAsc},
		{"amount with three decimals", cursorWith("amount", "1.234"), amountAsc},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.DecodeTotalSaleCursor(tt.raw, tt.sort)

			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
		})
	}
}
