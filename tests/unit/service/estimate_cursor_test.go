package service_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

// estimateSortValue is what the repository's sort_value column looks like for each key.
var estimateSortValue = map[model.EstimateSortKey]string{
	model.EstimateSortAccountNumber:   "63706",
	model.EstimateSortNumber:          "123187",
	model.EstimateSortCustomer:        "william saliba",
	model.EstimateSortPhone:           "(098) 765-4321",
	model.EstimateSortBillingEmail:    "williamsaliba@yopmail.com",
	model.EstimateSortCreatedBy:       "thọ nguyễn quang",
	model.EstimateSortDate:            "2026-10-12",
	model.EstimateSortTotal:           "148.56",
	model.EstimateSortStatus:          "accepted",
	model.EstimateSortSignatureStatus: "signed",
}

func TestEstimateCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	require.Len(t, estimateSortValue, len(model.EstimateSortKeys), "every sort key needs a sample value")

	for _, key := range model.EstimateSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.EstimateSort{Key: key, Desc: desc}
				r := model.EstimateRow{ID: 4242, SortValue: estimateSortValue[key]}

				raw := service.EncodeEstimateCursor(sort, r)
				got, err := service.DecodeEstimateCursor(raw, sort)

				require.NoError(t, err)
				require.Equal(t, &model.EstimateCursor{SortValue: estimateSortValue[key], ID: 4242}, got)
			})
		}
	}
}

func TestEstimateCursorAcceptsEmptyTextValues(t *testing.T) {
	// A customer without a phone and an estimate without email or creator sort as an empty string.
	for _, key := range []model.EstimateSortKey{model.EstimateSortPhone, model.EstimateSortBillingEmail, model.EstimateSortCreatedBy} {
		t.Run(string(key), func(t *testing.T) {
			sort := model.EstimateSort{Key: key}
			raw := service.EncodeEstimateCursor(sort, model.EstimateRow{ID: 5, SortValue: ""})

			got, err := service.DecodeEstimateCursor(raw, sort)

			require.NoError(t, err)
			require.Equal(t, "", got.SortValue)
		})
	}
}

func TestEstimateCursorRejectsInvalidCursors(t *testing.T) {
	dateAsc := model.EstimateSort{Key: model.EstimateSortDate}
	totalAsc := model.EstimateSort{Key: model.EstimateSortTotal}
	customerAsc := model.EstimateSort{Key: model.EstimateSortCustomer}
	phoneAsc := model.EstimateSort{Key: model.EstimateSortPhone}

	tests := []struct {
		name       string
		raw        string
		sort       model.EstimateSort
		wantReason string
	}{
		{"empty", "", dateAsc, "not a valid cursor"},
		{"not base64", "!!!not-base64!!!", dateAsc, "not a valid cursor"},
		{"not json", encode("hello"), dateAsc, "not a valid cursor"},
		{"missing id", encode(`{"k":"date","d":false,"v":"2026-10-12"}`), dateAsc, "not a valid cursor"},
		{"unknown field", encode(`{"k":"date","d":false,"v":"2026-10-12","id":1,"x":1}`), dateAsc, "not a valid cursor"},
		{"trailing data", encode(`{"k":"date","d":false,"v":"2026-10-12","id":1}{"k":"date"}`), dateAsc, "not a valid cursor"},
		{"id zero", encode(`{"k":"date","d":false,"v":"2026-10-12","id":0}`), dateAsc, "not a valid cursor"},
		{"id negative", encode(`{"k":"date","d":false,"v":"2026-10-12","id":-5}`), dateAsc, "not a valid cursor"},

		{"other sort key", encode(`{"k":"total","d":false,"v":"1.00","id":1}`), dateAsc, "does not match the requested sort"},
		{"other direction", encode(`{"k":"date","d":true,"v":"2026-10-12","id":1}`), dateAsc, "does not match the requested sort"},
		{"an invoice-only key", encode(`{"k":"discount","d":false,"v":"1.00","id":1}`), totalAsc, "does not match the requested sort"},

		{"date wrong format", encode(`{"k":"date","d":false,"v":"10/12/2026","id":1}`), dateAsc, "not a valid cursor"},
		{"date impossible", encode(`{"k":"date","d":false,"v":"2026-13-40","id":1}`), dateAsc, "not a valid cursor"},
		{"date empty", encode(`{"k":"date","d":false,"v":"","id":1}`), dateAsc, "not a valid cursor"},
		{"date with injection", encode(`{"k":"date","d":false,"v":"2026-10-12'; DROP TABLE billing_documents;--","id":1}`), dateAsc, "not a valid cursor"},

		{"amount not numeric", encode(`{"k":"total","d":false,"v":"abc","id":1}`), totalAsc, "not a valid cursor"},
		{"amount three decimals", encode(`{"k":"total","d":false,"v":"1.234","id":1}`), totalAsc, "not a valid cursor"},
		{"amount empty", encode(`{"k":"total","d":false,"v":"","id":1}`), totalAsc, "not a valid cursor"},

		{"text with NUL", encode(`{"k":"customer","d":false,"v":"a\u0000b","id":1}`), customerAsc, "not a valid cursor"},
		{"phone with NUL", encode(`{"k":"phone","d":false,"v":"1\u00002","id":1}`), phoneAsc, "not a valid cursor"},
		{"text too long", encode(`{"k":"customer","d":false,"v":"` + strings.Repeat("x", model.MaxSortTextLength+1) + `","id":1}`), customerAsc, "not a valid cursor"},
		{"cursor too long", strings.Repeat("A", 4097), customerAsc, "too long"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.DecodeEstimateCursor(tt.raw, tt.sort)

			require.Nil(t, got)
			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
			require.Contains(t, ce.Reason, tt.wantReason)
		})
	}
}

// The two reports share one cursor format, so each must reject the other's cursors by key.
func TestCursorsDoNotCrossBetweenReports(t *testing.T) {
	invoiceCursor := service.EncodeInvoiceCursor(model.InvoiceSort{Key: model.SortDiscount},
		model.InvoiceRow{ID: 3, SortValue: "1.23"})
	_, err := service.DecodeEstimateCursor(invoiceCursor, model.EstimateSort{Key: model.EstimateSortTotal})
	require.Error(t, err)

	estimateCursor := service.EncodeEstimateCursor(model.EstimateSort{Key: model.EstimateSortPhone},
		model.EstimateRow{ID: 3, SortValue: "012"})
	_, err = service.DecodeInvoiceCursor(estimateCursor, model.InvoiceSort{Key: model.SortAccountNumber})
	require.Error(t, err)
}

func TestEstimateCursorAcceptsBoundaryValues(t *testing.T) {
	t.Run("text of exactly the maximum sort length", func(t *testing.T) {
		sort := model.EstimateSort{Key: model.EstimateSortCreatedBy}
		long := strings.Repeat("é", model.MaxSortTextLength)
		raw := service.EncodeEstimateCursor(sort, model.EstimateRow{ID: 1, SortValue: long})
		got, err := service.DecodeEstimateCursor(raw, sort)
		require.NoError(t, err)
		require.Equal(t, long, got.SortValue)
	})

	t.Run("zero and negative amounts", func(t *testing.T) {
		for _, v := range []string{"0.00", "-5.00", "0", "99999999999.99"} {
			sort := model.EstimateSort{Key: model.EstimateSortTotal, Desc: true}
			raw := service.EncodeEstimateCursor(sort, model.EstimateRow{ID: 1, SortValue: v})
			_, err := service.DecodeEstimateCursor(raw, sort)
			require.NoError(t, err, "value %q", v)
		}
	})

	t.Run("largest id", func(t *testing.T) {
		sort := model.EstimateSort{Key: model.EstimateSortDate}
		raw := service.EncodeEstimateCursor(sort, model.EstimateRow{ID: 9223372036854775807, SortValue: "2026-10-12"})
		got, err := service.DecodeEstimateCursor(raw, sort)
		require.NoError(t, err)
		require.EqualValues(t, int64(9223372036854775807), got.ID)
	})
}
