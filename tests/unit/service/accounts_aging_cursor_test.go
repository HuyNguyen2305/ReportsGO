package service_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

// agingSortValue is what the repository's sort_value column looks like for each key.
var agingSortValue = map[model.AgingSortKey]string{
	model.AgingSortCustomer:        "william saliba",
	model.AgingSortTotal:           "1234.50",
	model.AgingSortD0to30:          "0.00",
	model.AgingSortD31to60:         "20.00",
	model.AgingSortD61to90:         "7.07",
	model.AgingSortD90Plus:         "99999.99",
	model.AgingSortAvailableCredit: "300.00",
}

func TestAgingCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	require.Len(t, agingSortValue, len(model.AgingSortKeys), "every sort key needs a sample value")

	for _, key := range model.AgingSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.AgingSort{Key: key, Desc: desc}
				r := model.AgingRow{ID: 4242 * 4294967296, SortValue: agingSortValue[key]}

				raw := service.EncodeAgingCursor(sort, r)
				got, err := service.DecodeAgingCursor(raw, sort)

				require.NoError(t, err)
				require.Equal(t, &model.AgingCursor{SortValue: agingSortValue[key], ID: 4242 * 4294967296}, got)
			})
		}
	}
}

func TestAgingCursorRejectsInvalidCursors(t *testing.T) {
	customerAsc := model.AgingSort{Key: model.AgingSortCustomer}
	totalAsc := model.AgingSort{Key: model.AgingSortTotal}

	tests := []struct {
		name string
		raw  string
		sort model.AgingSort
	}{
		{"not base64", "!!!", customerAsc},
		{"not json", encode("hello"), customerAsc},
		{"unknown field", encode(`{"k":"customer","d":false,"v":"a","id":1,"x":1}`), customerAsc},
		{"id missing", encode(`{"k":"customer","d":false,"v":"a"}`), customerAsc},
		{"another sort key", encode(`{"k":"total","d":false,"v":"10.00","id":1}`), customerAsc},
		{"another direction", encode(`{"k":"customer","d":true,"v":"a","id":1}`), customerAsc},
		{"total that is not money", encode(`{"k":"total","d":false,"v":"abc","id":1}`), totalAsc},
		{"total with three decimals", encode(`{"k":"total","d":false,"v":"1.234","id":1}`), totalAsc},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.DecodeAgingCursor(tt.raw, tt.sort)

			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
		})
	}
}
