package service_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

// customerSortValue is what the repository's sort_value column looks like for each key.
var customerSortValue = map[model.CustomerSortKey]string{
	model.CustomerSortCustomer:      "ada lovelace",
	model.CustomerSortAccountNumber: "70001",
	model.CustomerSortEmail:         "ada@example.com",
	model.CustomerSortPhone:         "555-0101",
	model.CustomerSortSource:        "google",
	model.CustomerSortCreatedAt:     "2026-10-22T08:15:30.123456Z",
}

func TestCustomerCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	require.Len(t, customerSortValue, len(model.CustomerSortKeys), "every sort key needs a sample value")

	for _, key := range model.CustomerSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.CustomerSort{Key: key, Desc: desc}
				r := model.CustomerRow{ID: 4242, SortValue: customerSortValue[key]}

				raw := service.EncodeCustomerCursor(sort, r)
				got, err := service.DecodeCustomerCursor(raw, sort)

				require.NoError(t, err)
				require.Equal(t, &model.CustomerCursor{SortValue: customerSortValue[key], ID: 4242}, got)
			})
		}
	}
}

func TestCustomerCursorAcceptsEmptyValues(t *testing.T) {
	for _, key := range []model.CustomerSortKey{model.CustomerSortEmail, model.CustomerSortPhone, model.CustomerSortSource} {
		sort := model.CustomerSort{Key: key}
		raw := service.EncodeCustomerCursor(sort, model.CustomerRow{ID: 5, SortValue: ""})

		got, err := service.DecodeCustomerCursor(raw, sort)

		require.NoError(t, err, string(key))
		require.Equal(t, "", got.SortValue, "customers without a %s sort as an empty value", key)
	}
}

func TestCustomerCursorRejectsInvalidCursors(t *testing.T) {
	createdAsc := model.CustomerSort{Key: model.CustomerSortCreatedAt}
	customerAsc := model.CustomerSort{Key: model.CustomerSortCustomer}

	cursorWith := func(key, value string) string {
		return encode(`{"k":"` + key + `","d":false,"v":"` + value + `","id":1}`)
	}

	tests := []struct {
		name       string
		raw        string
		sort       model.CustomerSort
		wantReason string
	}{
		{"empty", "", createdAsc, "not a valid cursor"},
		{"not base64", "!!!not-base64!!!", createdAsc, "not a valid cursor"},
		{"id zero", encode(`{"k":"created_at","d":false,"v":"2026-10-12T08:00:00.000000Z","id":0}`), createdAsc, "not a valid cursor"},

		{"other sort key", cursorWith("email", "a@b.c"), createdAsc, "does not match the requested sort"},
		{"other direction", encode(`{"k":"created_at","d":true,"v":"2026-10-12T08:00:00.000000Z","id":1}`), createdAsc, "does not match the requested sort"},
		{"a job-only key", cursorWith("invoice_value", "65.00"), customerAsc, "does not match the requested sort"},

		{"timestamp without microseconds", cursorWith("created_at", "2026-10-12T08:00:00Z"), createdAsc, "not a valid cursor"},
		{"timestamp with an offset", cursorWith("created_at", "2026-10-12T08:00:00.000000+07:00"), createdAsc, "not a valid cursor"},
		{"timestamp date only", cursorWith("created_at", "2026-10-12"), createdAsc, "not a valid cursor"},
		{"timestamp with injection", cursorWith("created_at", "2026-10-12T08:00:00.000000Z'; DROP TABLE customers;--"), createdAsc, "not a valid cursor"},

		{"text with NUL", encode(`{"k":"customer","d":false,"v":"a\u0000b","id":1}`), customerAsc, "not a valid cursor"},
		{"text too long", cursorWith("customer", strings.Repeat("x", 1001)), customerAsc, "not a valid cursor"},
		{"cursor too long", strings.Repeat("A", 4097), customerAsc, "too long"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.DecodeCustomerCursor(tt.raw, tt.sort)

			require.Nil(t, got)
			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
			require.Contains(t, ce.Reason, tt.wantReason)
		})
	}
}

func TestCustomerCursorAcceptsBoundaryValues(t *testing.T) {
	t.Run("the earliest and latest instants", func(t *testing.T) {
		sort := model.CustomerSort{Key: model.CustomerSortCreatedAt}
		for _, v := range []string{"0001-01-01T00:00:00.000000Z", "9999-12-31T23:59:59.999999Z", "2026-10-10T23:59:59.999999Z"} {
			raw := service.EncodeCustomerCursor(sort, model.CustomerRow{ID: 1, SortValue: v})
			got, err := service.DecodeCustomerCursor(raw, sort)
			require.NoError(t, err, v)
			require.Equal(t, v, got.SortValue)
		}
	})

	t.Run("text of exactly 1000 characters", func(t *testing.T) {
		sort := model.CustomerSort{Key: model.CustomerSortEmail}
		long := strings.Repeat("é", 1000)
		raw := service.EncodeCustomerCursor(sort, model.CustomerRow{ID: 1, SortValue: long})
		got, err := service.DecodeCustomerCursor(raw, sort)
		require.NoError(t, err)
		require.Equal(t, long, got.SortValue)
	})
}

func TestCustomerCursorsDoNotCrossBetweenReportsWithDifferentKeys(t *testing.T) {
	job := service.EncodeJobCursor(model.JobSort{Key: model.JobSortInvoiceValue}, model.JobRow{ID: 3, SortValue: "65.00"})
	_, err := service.DecodeCustomerCursor(job, model.CustomerSort{Key: model.CustomerSortAccountNumber})
	require.Error(t, err)

	customer := service.EncodeCustomerCursor(model.CustomerSort{Key: model.CustomerSortEmail}, model.CustomerRow{ID: 3, SortValue: "a@b.c"})
	_, err = service.DecodeJobCursor(customer, model.JobSort{Key: model.JobSortCustomer})
	require.Error(t, err)
}
