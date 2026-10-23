package service_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

// jobSortValue is what the repository's sort_value column looks like for each key.
var jobSortValue = map[model.JobSortKey]string{
	model.JobSortCustomer:      "william saliba",
	model.JobSortAccountNumber: "63706",
	model.JobSortServiceType:   "bi-monthly",
	model.JobSortSource:        "google",
	model.JobSortInvoiceValue:  "65.00",
	model.JobSortCreatedAt:     "2026-10-22T08:15:30.123456Z",
	model.JobSortStatus:        "completed",
}

func TestJobCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	require.Len(t, jobSortValue, len(model.JobSortKeys), "every sort key needs a sample value")

	for _, key := range model.JobSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.JobSort{Key: key, Desc: desc}
				r := model.JobRow{ID: 4242, SortValue: jobSortValue[key]}

				raw := service.EncodeJobCursor(sort, r)
				got, err := service.DecodeJobCursor(raw, sort)

				require.NoError(t, err)
				require.Equal(t, &model.JobCursor{SortValue: jobSortValue[key], ID: 4242}, got)
			})
		}
	}
}

func TestJobCursorAcceptsAnEmptySource(t *testing.T) {
	sort := model.JobSort{Key: model.JobSortSource}
	raw := service.EncodeJobCursor(sort, model.JobRow{ID: 5, SortValue: ""})

	got, err := service.DecodeJobCursor(raw, sort)

	require.NoError(t, err)
	require.Equal(t, "", got.SortValue, "jobs without a lead source sort as an empty name")
}

func TestJobCursorRejectsInvalidCursors(t *testing.T) {
	createdAsc := model.JobSort{Key: model.JobSortCreatedAt}
	valueAsc := model.JobSort{Key: model.JobSortInvoiceValue}
	customerAsc := model.JobSort{Key: model.JobSortCustomer}

	cursorWith := func(key, value string) string {
		return encode(`{"k":"` + key + `","d":false,"v":"` + value + `","id":1}`)
	}

	tests := []struct {
		name       string
		raw        string
		sort       model.JobSort
		wantReason string
	}{
		{"empty", "", createdAsc, "not a valid cursor"},
		{"not base64", "!!!not-base64!!!", createdAsc, "not a valid cursor"},
		{"id zero", encode(`{"k":"created_at","d":false,"v":"2026-10-12T08:00:00.000000Z","id":0}`), createdAsc, "not a valid cursor"},

		{"other sort key", cursorWith("status", "completed"), createdAsc, "does not match the requested sort"},
		{"other direction", encode(`{"k":"created_at","d":true,"v":"2026-10-12T08:00:00.000000Z","id":1}`), createdAsc, "does not match the requested sort"},
		{"a document-only key", cursorWith("document_number", "5979"), customerAsc, "does not match the requested sort"},

		{"timestamp without microseconds", cursorWith("created_at", "2026-10-12T08:00:00Z"), createdAsc, "not a valid cursor"},
		{"timestamp with an offset", cursorWith("created_at", "2026-10-12T08:00:00.000000+07:00"), createdAsc, "not a valid cursor"},
		{"timestamp date only", cursorWith("created_at", "2026-10-12"), createdAsc, "not a valid cursor"},
		{"timestamp with injection", cursorWith("created_at", "2026-10-12T08:00:00.000000Z'; DROP TABLE jobs;--"), createdAsc, "not a valid cursor"},

		{"money not a number", cursorWith("invoice_value", "abc"), valueAsc, "not a valid cursor"},
		{"money with injection", cursorWith("invoice_value", "1; DROP TABLE jobs"), valueAsc, "not a valid cursor"},
		{"money empty", cursorWith("invoice_value", ""), valueAsc, "not a valid cursor"},

		{"text with NUL", encode(`{"k":"customer","d":false,"v":"a\u0000b","id":1}`), customerAsc, "not a valid cursor"},
		{"text too long", cursorWith("customer", strings.Repeat("x", 1001)), customerAsc, "not a valid cursor"},
		{"cursor too long", strings.Repeat("A", 4097), customerAsc, "too long"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.DecodeJobCursor(tt.raw, tt.sort)

			require.Nil(t, got)
			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
			require.Contains(t, ce.Reason, tt.wantReason)
		})
	}
}

func TestJobCursorAcceptsBoundaryValues(t *testing.T) {
	t.Run("the earliest and latest instants", func(t *testing.T) {
		sort := model.JobSort{Key: model.JobSortCreatedAt}
		for _, v := range []string{"0001-01-01T00:00:00.000000Z", "9999-12-31T23:59:59.999999Z", "2026-10-10T23:59:59.999999Z"} {
			raw := service.EncodeJobCursor(sort, model.JobRow{ID: 1, SortValue: v})
			got, err := service.DecodeJobCursor(raw, sort)
			require.NoError(t, err, v)
			require.Equal(t, v, got.SortValue)
		}
	})

	t.Run("money values", func(t *testing.T) {
		sort := model.JobSort{Key: model.JobSortInvoiceValue}
		for _, v := range []string{"0.00", "10.33", "99999999.99"} {
			raw := service.EncodeJobCursor(sort, model.JobRow{ID: 1, SortValue: v})
			got, err := service.DecodeJobCursor(raw, sort)
			require.NoError(t, err, v)
			require.Equal(t, v, got.SortValue)
		}
	})
}

func TestJobCursorsDoNotCrossBetweenReports(t *testing.T) {
	job := service.EncodeJobCursor(model.JobSort{Key: model.JobSortCreatedAt},
		model.JobRow{ID: 3, SortValue: "2026-10-12T08:00:00.000000Z"})
	_, err := service.DecodeDocumentCursor(job, model.DocumentSort{Key: model.DocumentSortCreatedAt})
	require.NoError(t, err, "same key name and shape: a job cursor is interchangeable with a document one by design")

	document := service.EncodeDocumentCursor(model.DocumentSort{Key: model.DocumentSortNumber}, model.DocumentRow{ID: 3, SortValue: "5979"})
	_, err = service.DecodeJobCursor(document, model.JobSort{Key: model.JobSortAccountNumber})
	require.Error(t, err)
}
