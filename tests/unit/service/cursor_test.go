package service_test

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

// realisticSortValue is what the repository's sort_value column looks like for each key.
var realisticSortValue = map[model.InvoiceSortKey]string{
	model.SortAccountNumber:   "63706",
	model.SortInvoiceNumber:   "85009",
	model.SortCustomer:        "william saliba",
	model.SortJob:             "blank service 123 (s1 s2)",
	model.SortBillingEmail:    "williamsaliba@yopmail.com",
	model.SortDate:            "2026-10-12",
	model.SortTotal:           "148.56",
	model.SortDiscount:        "1.23",
	model.SortStatus:          "paid",
	model.SortSignatureStatus: "draft",
	model.SortPaymentTerms:    "net 30 terms",
}

func encode(payload string) string { return base64.RawURLEncoding.EncodeToString([]byte(payload)) }

func TestCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	require.Len(t, realisticSortValue, len(model.InvoiceSortKeys), "every sort key needs a sample value")

	for _, key := range model.InvoiceSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.InvoiceSort{Key: key, Desc: desc}
				r := model.InvoiceRow{ID: 4242, SortValue: realisticSortValue[key]}

				raw := service.EncodeInvoiceCursor(sort, r)
				got, err := service.DecodeInvoiceCursor(raw, sort)

				require.NoError(t, err)
				require.Equal(t, &model.InvoiceCursor{SortValue: realisticSortValue[key], ID: 4242}, got)
			})
		}
	}
}

func TestCursorEncodingIsDeterministicAndURLSafe(t *testing.T) {
	sort := model.InvoiceSort{Key: model.SortCustomer, Desc: true}
	r := model.InvoiceRow{ID: 9, SortValue: "müller / o'brien + sons?=&"}

	first := service.EncodeInvoiceCursor(sort, r)
	second := service.EncodeInvoiceCursor(sort, r)

	require.Equal(t, first, second)
	require.NotContains(t, first, "+")
	require.NotContains(t, first, "/")
	require.NotContains(t, first, "=")

	got, err := service.DecodeInvoiceCursor(first, sort)
	require.NoError(t, err)
	require.Equal(t, r.SortValue, got.SortValue, "special characters survive the round trip")
}

func TestCursorAcceptsEmptyTextValues(t *testing.T) {
	// NULL job, billing email and payment terms sort as an empty string, so their cursors carry "".
	for _, key := range []model.InvoiceSortKey{model.SortJob, model.SortBillingEmail, model.SortPaymentTerms} {
		t.Run(string(key), func(t *testing.T) {
			sort := model.InvoiceSort{Key: key}
			raw := service.EncodeInvoiceCursor(sort, model.InvoiceRow{ID: 5, SortValue: ""})

			got, err := service.DecodeInvoiceCursor(raw, sort)

			require.NoError(t, err)
			require.Equal(t, "", got.SortValue)
		})
	}
}

func TestCursorRejectsInvalidCursors(t *testing.T) {
	dateAsc := model.InvoiceSort{Key: model.SortDate}
	totalAsc := model.InvoiceSort{Key: model.SortTotal}
	customerAsc := model.InvoiceSort{Key: model.SortCustomer}

	tests := []struct {
		name       string
		raw        string
		sort       model.InvoiceSort
		wantReason string
	}{
		{"empty", "", dateAsc, "not a valid cursor"},
		{"not base64", "!!!not-base64!!!", dateAsc, "not a valid cursor"},
		{"padded base64", encode(`{"k":"date","d":false,"v":"2026-10-12","id":1}`) + "==", dateAsc, "not a valid cursor"},
		{"standard base64 alphabet", "ab+/", dateAsc, "not a valid cursor"},
		{"not json", encode("hello"), dateAsc, "not a valid cursor"},
		{"json null", encode("null"), dateAsc, "does not match"},
		{"json array", encode(`[1,2]`), dateAsc, "not a valid cursor"},
		{"missing id", encode(`{"k":"date","d":false,"v":"2026-10-12"}`), dateAsc, "not a valid cursor"},
		{"unknown field", encode(`{"k":"date","d":false,"v":"2026-10-12","id":1,"x":1}`), dateAsc, "not a valid cursor"},
		{"trailing data", encode(`{"k":"date","d":false,"v":"2026-10-12","id":1}{"k":"date"}`), dateAsc, "not a valid cursor"},
		{"id is a string", encode(`{"k":"date","d":false,"v":"2026-10-12","id":"1"}`), dateAsc, "not a valid cursor"},
		{"id zero", encode(`{"k":"date","d":false,"v":"2026-10-12","id":0}`), dateAsc, "not a valid cursor"},
		{"id negative", encode(`{"k":"date","d":false,"v":"2026-10-12","id":-5}`), dateAsc, "not a valid cursor"},
		{"id overflows", encode(`{"k":"date","d":false,"v":"2026-10-12","id":99999999999999999999}`), dateAsc, "not a valid cursor"},

		{"other sort key", encode(`{"k":"total","d":false,"v":"1.00","id":1}`), dateAsc, "does not match the requested sort"},
		{"other direction", encode(`{"k":"date","d":true,"v":"2026-10-12","id":1}`), dateAsc, "does not match the requested sort"},

		{"date wrong format", encode(`{"k":"date","d":false,"v":"10/12/2026","id":1}`), dateAsc, "not a valid cursor"},
		{"date impossible", encode(`{"k":"date","d":false,"v":"2026-13-40","id":1}`), dateAsc, "not a valid cursor"},
		{"date not zero padded", encode(`{"k":"date","d":false,"v":"2026-1-5","id":1}`), dateAsc, "not a valid cursor"},
		{"date empty", encode(`{"k":"date","d":false,"v":"","id":1}`), dateAsc, "not a valid cursor"},
		{"date with injection", encode(`{"k":"date","d":false,"v":"2026-10-12'; DROP TABLE invoices;--","id":1}`), dateAsc, "not a valid cursor"},

		{"amount not numeric", encode(`{"k":"total","d":false,"v":"abc","id":1}`), totalAsc, "not a valid cursor"},
		{"amount three decimals", encode(`{"k":"total","d":false,"v":"1.234","id":1}`), totalAsc, "not a valid cursor"},
		{"amount empty", encode(`{"k":"total","d":false,"v":"","id":1}`), totalAsc, "not a valid cursor"},
		{"amount exponent", encode(`{"k":"total","d":false,"v":"1e3","id":1}`), totalAsc, "not a valid cursor"},

		{"text with NUL", encode(`{"k":"customer","d":false,"v":"a\u0000b","id":1}`), customerAsc, "not a valid cursor"},
		{"text too long", encode(`{"k":"customer","d":false,"v":"` + strings.Repeat("x", 1001) + `","id":1}`), customerAsc, "not a valid cursor"},
		{"cursor too long", strings.Repeat("A", 4097), customerAsc, "too long"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.DecodeInvoiceCursor(tt.raw, tt.sort)

			require.Nil(t, got)
			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
			require.Contains(t, ce.Reason, tt.wantReason)
		})
	}
}

func TestCursorAcceptsBoundaryValues(t *testing.T) {
	customerAsc := model.InvoiceSort{Key: model.SortCustomer}

	t.Run("text of exactly 1000 characters", func(t *testing.T) {
		long := strings.Repeat("é", 1000)
		raw := service.EncodeInvoiceCursor(customerAsc, model.InvoiceRow{ID: 1, SortValue: long})
		got, err := service.DecodeInvoiceCursor(raw, customerAsc)
		require.NoError(t, err)
		require.Equal(t, long, got.SortValue)
	})

	t.Run("negative and zero amounts", func(t *testing.T) {
		for _, v := range []string{"0.00", "-5.00", "0", "99999999999.99"} {
			sort := model.InvoiceSort{Key: model.SortDiscount, Desc: true}
			raw := service.EncodeInvoiceCursor(sort, model.InvoiceRow{ID: 1, SortValue: v})
			_, err := service.DecodeInvoiceCursor(raw, sort)
			require.NoError(t, err, "value %q", v)
		}
	})

	t.Run("largest id", func(t *testing.T) {
		sort := model.InvoiceSort{Key: model.SortDate}
		raw := service.EncodeInvoiceCursor(sort, model.InvoiceRow{ID: 9223372036854775807, SortValue: "2026-10-12"})
		got, err := service.DecodeInvoiceCursor(raw, sort)
		require.NoError(t, err)
		require.EqualValues(t, int64(9223372036854775807), got.ID)
	})
}

func TestInvalidCursorErrorMessage(t *testing.T) {
	_, err := service.DecodeInvoiceCursor("nope!", model.InvoiceSort{Key: model.SortDate})
	require.EqualError(t, err, "invalid cursor: is not a valid cursor")
}
