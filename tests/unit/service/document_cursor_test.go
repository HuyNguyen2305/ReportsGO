package service_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

// documentSortValue is what the repository's sort_value column looks like for each key.
var documentSortValue = map[model.DocumentSortKey]string{
	model.DocumentSortNumber:        "5979",
	model.DocumentSortType:          "1-time treatment (pestpros of mi)",
	model.DocumentSortAccountNumber: "63706",
	model.DocumentSortCustomer:      "william saliba",
	model.DocumentSortCreatedBy:     "thọ nguyễn quang",
	model.DocumentSortCreatedAt:     "2026-10-22T08:15:30.123456Z",
	model.DocumentSortStatus:        "signed",
}

func TestDocumentCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	require.Len(t, documentSortValue, len(model.DocumentSortKeys), "every sort key needs a sample value")

	for _, key := range model.DocumentSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.DocumentSort{Key: key, Desc: desc}
				r := model.DocumentRow{ID: 4242, SortValue: documentSortValue[key]}

				raw := service.EncodeDocumentCursor(sort, r)
				got, err := service.DecodeDocumentCursor(raw, sort)

				require.NoError(t, err)
				require.Equal(t, &model.DocumentCursor{SortValue: documentSortValue[key], ID: 4242}, got)
			})
		}
	}
}

func TestDocumentCursorAcceptsAnEmptyCreator(t *testing.T) {
	sort := model.DocumentSort{Key: model.DocumentSortCreatedBy}
	raw := service.EncodeDocumentCursor(sort, model.DocumentRow{ID: 5, SortValue: ""})

	got, err := service.DecodeDocumentCursor(raw, sort)

	require.NoError(t, err)
	require.Equal(t, "", got.SortValue)
}

func TestDocumentCursorRejectsInvalidCursors(t *testing.T) {
	createdAsc := model.DocumentSort{Key: model.DocumentSortCreatedAt}
	customerAsc := model.DocumentSort{Key: model.DocumentSortCustomer}

	cursorWith := func(key, value string) string {
		return encode(`{"k":"` + key + `","d":false,"v":"` + value + `","id":1}`)
	}

	tests := []struct {
		name       string
		raw        string
		sort       model.DocumentSort
		wantReason string
	}{
		{"empty", "", createdAsc, "not a valid cursor"},
		{"not base64", "!!!not-base64!!!", createdAsc, "not a valid cursor"},
		{"not json", encode("hello"), createdAsc, "not a valid cursor"},
		{"missing id", encode(`{"k":"created_at","d":false,"v":"2026-10-12T08:00:00.000000Z"}`), createdAsc, "not a valid cursor"},
		{"unknown field", encode(`{"k":"created_at","d":false,"v":"2026-10-12T08:00:00.000000Z","id":1,"x":1}`), createdAsc, "not a valid cursor"},
		{"trailing data", encode(`{"k":"created_at","d":false,"v":"2026-10-12T08:00:00.000000Z","id":1}{"k":"x"}`), createdAsc, "not a valid cursor"},
		{"id zero", encode(`{"k":"created_at","d":false,"v":"2026-10-12T08:00:00.000000Z","id":0}`), createdAsc, "not a valid cursor"},
		{"id negative", encode(`{"k":"created_at","d":false,"v":"2026-10-12T08:00:00.000000Z","id":-5}`), createdAsc, "not a valid cursor"},

		{"other sort key", cursorWith("status", "signed"), createdAsc, "does not match the requested sort"},
		{"other direction", encode(`{"k":"created_at","d":true,"v":"2026-10-12T08:00:00.000000Z","id":1}`), createdAsc, "does not match the requested sort"},
		{"an estimate-only key", cursorWith("phone", "012"), customerAsc, "does not match the requested sort"},

		{"timestamp without Z", cursorWith("created_at", "2026-10-12T08:00:00.000000"), createdAsc, "not a valid cursor"},
		{"timestamp without microseconds", cursorWith("created_at", "2026-10-12T08:00:00Z"), createdAsc, "not a valid cursor"},
		{"timestamp with milliseconds only", cursorWith("created_at", "2026-10-12T08:00:00.123Z"), createdAsc, "not a valid cursor"},
		{"timestamp with an offset instead of Z", cursorWith("created_at", "2026-10-12T08:00:00.000000+07:00"), createdAsc, "not a valid cursor"},
		{"timestamp with a space", cursorWith("created_at", "2026-10-12 08:00:00.000000Z"), createdAsc, "not a valid cursor"},
		{"timestamp date only", cursorWith("created_at", "2026-10-12"), createdAsc, "not a valid cursor"},
		{"timestamp impossible date", cursorWith("created_at", "2026-13-40T08:00:00.000000Z"), createdAsc, "not a valid cursor"},
		{"timestamp hour 24", cursorWith("created_at", "2026-10-12T24:00:00.000000Z"), createdAsc, "not a valid cursor"},
		{"timestamp empty", cursorWith("created_at", ""), createdAsc, "not a valid cursor"},
		{"timestamp with injection", cursorWith("created_at", "2026-10-12T08:00:00.000000Z'; DROP TABLE documents;--"), createdAsc, "not a valid cursor"},

		{"text with NUL", encode(`{"k":"customer","d":false,"v":"a\u0000b","id":1}`), customerAsc, "not a valid cursor"},
		{"text too long", cursorWith("customer", strings.Repeat("x", 1001)), customerAsc, "not a valid cursor"},
		{"cursor too long", strings.Repeat("A", 4097), customerAsc, "too long"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.DecodeDocumentCursor(tt.raw, tt.sort)

			require.Nil(t, got)
			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
			require.Contains(t, ce.Reason, tt.wantReason)
		})
	}
}

func TestDocumentCursorAcceptsBoundaryValues(t *testing.T) {
	t.Run("the earliest and latest instants the layout can hold", func(t *testing.T) {
		sort := model.DocumentSort{Key: model.DocumentSortCreatedAt}
		for _, v := range []string{"0001-01-01T00:00:00.000000Z", "9999-12-31T23:59:59.999999Z", "2026-02-28T00:00:00.000001Z", "2028-02-29T12:00:00.000000Z"} {
			raw := service.EncodeDocumentCursor(sort, model.DocumentRow{ID: 1, SortValue: v})
			got, err := service.DecodeDocumentCursor(raw, sort)
			require.NoError(t, err, v)
			require.Equal(t, v, got.SortValue)
		}
	})

	t.Run("text of exactly 1000 characters", func(t *testing.T) {
		sort := model.DocumentSort{Key: model.DocumentSortType}
		long := strings.Repeat("é", 1000)
		raw := service.EncodeDocumentCursor(sort, model.DocumentRow{ID: 1, SortValue: long})
		got, err := service.DecodeDocumentCursor(raw, sort)
		require.NoError(t, err)
		require.Equal(t, long, got.SortValue)
	})

	t.Run("largest id", func(t *testing.T) {
		sort := model.DocumentSort{Key: model.DocumentSortNumber}
		raw := service.EncodeDocumentCursor(sort, model.DocumentRow{ID: 9223372036854775807, SortValue: "5979"})
		got, err := service.DecodeDocumentCursor(raw, sort)
		require.NoError(t, err)
		require.EqualValues(t, int64(9223372036854775807), got.ID)
	})
}

func TestDocumentCursorsDoNotCrossBetweenReports(t *testing.T) {
	estimate := service.EncodeEstimateCursor(model.EstimateSort{Key: model.EstimateSortPhone},
		model.EstimateRow{ID: 3, SortValue: "012"})
	_, err := service.DecodeDocumentCursor(estimate, model.DocumentSort{Key: model.DocumentSortNumber})
	require.Error(t, err)

	document := service.EncodeDocumentCursor(model.DocumentSort{Key: model.DocumentSortCreatedAt},
		model.DocumentRow{ID: 3, SortValue: "2026-10-12T08:00:00.000000Z"})
	_, err = service.DecodeEstimateCursor(document, model.EstimateSort{Key: model.EstimateSortDate})
	require.Error(t, err)
	_, err = service.DecodeInvoiceCursor(document, model.InvoiceSort{Key: model.SortDate})
	require.Error(t, err)
}
