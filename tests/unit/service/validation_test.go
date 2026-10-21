package service_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
)

func TestListRejectsInvalidInputPerField(t *testing.T) {
	manyIDs := make([]string, service.MaxIDsPerFilter+1)
	for i := range manyIDs {
		manyIDs[i] = "1"
	}
	manyStatuses := make([]string, service.MaxEnumValues+1)
	for i := range manyStatuses {
		manyStatuses[i] = "paid"
	}

	tests := []struct {
		name    string
		params  service.ListInvoicesParams
		field   string
		message string
	}{
		{"from wrong separator", service.ListInvoicesParams{From: "2026/10/01"}, "from", "YYYY-MM-DD"},
		{"from US format", service.ListInvoicesParams{From: "10/01/2026"}, "from", "YYYY-MM-DD"},
		{"from not a date", service.ListInvoicesParams{From: "yesterday"}, "from", "YYYY-MM-DD"},
		{"from impossible day", service.ListInvoicesParams{From: "2026-02-30"}, "from", "YYYY-MM-DD"},
		{"from impossible month", service.ListInvoicesParams{From: "2026-13-01"}, "from", "YYYY-MM-DD"},
		{"from not zero padded", service.ListInvoicesParams{From: "2026-1-1"}, "from", "YYYY-MM-DD"},
		{"from with time", service.ListInvoicesParams{From: "2026-10-01T00:00:00Z"}, "from", "YYYY-MM-DD"},
		{"to not a date", service.ListInvoicesParams{To: "soon"}, "to", "YYYY-MM-DD"},
		{"to before from", service.ListInvoicesParams{From: "2026-10-10", To: "2026-10-09"}, "to", "must not be before from"},

		{"lifecycle unknown", service.ListInvoicesParams{Lifecycle: "everything"}, "lifecycle", "active, archived, deleted"},

		{"status unknown", service.ListInvoicesParams{Status: []string{"paid", "overdue"}}, "status", "draft, sent, paid, void"},
		{"status too many", service.ListInvoicesParams{Status: manyStatuses}, "status", "at most 10 values"},
		{"signature status unknown", service.ListInvoicesParams{SignatureStatus: []string{"maybe"}}, "signature_status", "draft, pending, signed, declined"},

		{"service id not a number", service.ListInvoicesParams{ServiceIDs: []string{"abc"}}, "service_id", "positive whole numbers"},
		{"service id zero", service.ListInvoicesParams{ServiceIDs: []string{"0"}}, "service_id", "positive whole numbers"},
		{"tag id negative", service.ListInvoicesParams{TagIDs: []string{"-1"}}, "tag_id", "positive whole numbers"},
		{"tag id decimal", service.ListInvoicesParams{TagIDs: []string{"1.5"}}, "tag_id", "positive whole numbers"},
		{"payment term id exponent", service.ListInvoicesParams{PaymentTermIDs: []string{"1e3"}}, "payment_term_id", "positive whole numbers"},
		{"payment term id hex", service.ListInvoicesParams{PaymentTermIDs: []string{"0x10"}}, "payment_term_id", "positive whole numbers"},
		{"id overflows int64", service.ListInvoicesParams{ServiceIDs: []string{"99999999999999999999"}}, "service_id", "positive whole numbers"},
		{"one bad id among good", service.ListInvoicesParams{ServiceIDs: []string{"1", "2", "x"}}, "service_id", "positive whole numbers"},
		{"too many ids", service.ListInvoicesParams{TagIDs: manyIDs}, "tag_id", "at most 100 values"},

		{"search with NUL", service.ListInvoicesParams{Search: "abc\x00def"}, "q", "NUL"},
		{"search invalid UTF-8", service.ListInvoicesParams{Search: "abc\xff"}, "q", "UTF-8"},
		{"search too long", service.ListInvoicesParams{Search: strings.Repeat("a", 101)}, "q", "at most 100 characters"},
		{"search too long in multibyte characters", service.ListInvoicesParams{Search: strings.Repeat("é", 101)}, "q", "at most 100 characters"},

		{"sort unknown", service.ListInvoicesParams{Sort: "id"}, "sort", "account_number"},
		{"sort injection attempt", service.ListInvoicesParams{Sort: "date; DROP TABLE invoices"}, "sort", "must be one of"},
		{"order unknown", service.ListInvoicesParams{Order: "up"}, "order", "asc or desc"},

		{"limit zero", service.ListInvoicesParams{Limit: "0"}, "limit", "between 1 and 200"},
		{"limit negative", service.ListInvoicesParams{Limit: "-5"}, "limit", "between 1 and 200"},
		{"limit above maximum", service.ListInvoicesParams{Limit: "201"}, "limit", "between 1 and 200"},
		{"limit not a number", service.ListInvoicesParams{Limit: "many"}, "limit", "between 1 and 200"},
		{"limit decimal", service.ListInvoicesParams{Limit: "1.5"}, "limit", "between 1 and 200"},
		{"limit overflows", service.ListInvoicesParams{Limit: "99999999999999999999"}, "limit", "between 1 and 200"},

		{"cursor garbage", service.ListInvoicesParams{Cursor: "not-a-cursor!!"}, "cursor", "not a valid cursor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rejects(t, tt.params)
			require.Contains(t, got, tt.field)
			require.Contains(t, got[tt.field], tt.message)
			require.Len(t, got, 1, "only the offending field should be reported")
		})
	}
}

func TestListReportsAllInvalidFieldsTogether(t *testing.T) {
	got := rejects(t, service.ListInvoicesParams{
		From:       "nope",
		To:         "also-nope",
		Lifecycle:  "?",
		Status:     []string{"x"},
		ServiceIDs: []string{"y"},
		Search:     "a\x00",
		Sort:       "bogus",
		Order:      "sideways",
		Limit:      "0",
	})

	require.ElementsMatch(t,
		[]string{"from", "to", "lifecycle", "status", "service_id", "q", "sort", "order", "limit"},
		keys(got))
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestListDoesNotValidateTheCursorAgainstAnInvalidSort(t *testing.T) {
	// A bad sort already makes the request invalid; the cursor must not add a confusing second error.
	got := rejects(t, service.ListInvoicesParams{Sort: "bogus", Cursor: "whatever"})
	require.Equal(t, []string{"sort"}, keys(got))
}

func TestListAcceptsBoundaryValues(t *testing.T) {
	tests := []struct {
		name   string
		params service.ListInvoicesParams
		check  func(t *testing.T, q model.InvoiceQuery)
	}{
		{"limit 1", service.ListInvoicesParams{Limit: "1"}, func(t *testing.T, q model.InvoiceQuery) { require.Equal(t, 1, q.Limit) }},
		{"limit 200", service.ListInvoicesParams{Limit: "200"}, func(t *testing.T, q model.InvoiceQuery) { require.Equal(t, 200, q.Limit) }},
		{"search of exactly 100 multibyte characters", service.ListInvoicesParams{Search: strings.Repeat("é", 100)},
			func(t *testing.T, q model.InvoiceQuery) { require.Equal(t, strings.Repeat("é", 100), q.Filter.Search) }},
		{"from equals to", service.ListInvoicesParams{From: "2026-10-05", To: "2026-10-05"},
			func(t *testing.T, q model.InvoiceQuery) {
				require.Equal(t, date(2026, 10, 5), q.Filter.DateFrom)
				require.Equal(t, date(2026, 10, 5), q.Filter.DateTo)
			}},
		{"leap day", service.ListInvoicesParams{From: "2028-02-29", To: "2028-02-29"},
			func(t *testing.T, q model.InvoiceQuery) { require.Equal(t, date(2028, 2, 29), q.Filter.DateFrom) }},
		{"exactly 100 ids", service.ListInvoicesParams{TagIDs: repeat("7", service.MaxIDsPerFilter)},
			func(t *testing.T, q model.InvoiceQuery) {
				require.Equal(t, []int64{7}, q.Filter.TagIDs, "duplicates collapse")
			}},
		{"exactly 10 status values", service.ListInvoicesParams{Status: repeat("paid", service.MaxEnumValues)},
			func(t *testing.T, q model.InvoiceQuery) {
				require.Equal(t, []model.InvoiceStatus{model.InvoicePaid}, q.Filter.Statuses)
			}},
		{"max int64 id", service.ListInvoicesParams{ServiceIDs: []string{"9223372036854775807"}},
			func(t *testing.T, q model.InvoiceQuery) {
				require.Equal(t, []int64{9223372036854775807}, q.Filter.ServiceTypeIDs)
			}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := run(t, tt.params, nil)
			tt.check(t, q)
		})
	}
}

func repeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

func TestListNormalizesInput(t *testing.T) {
	q, res := run(t, service.ListInvoicesParams{
		From:            " 2026-10-05 ",
		To:              "2026-10-20",
		Lifecycle:       " Archived ",
		Status:          []string{" PAID ", "sent", "paid", "", "  "},
		SignatureStatus: []string{"Signed"},
		ServiceIDs:      []string{"3", " 1", "3", ""},
		TagIDs:          []string{"2"},
		PaymentTermIDs:  []string{"7"},
		Search:          "  Saliba  ",
		Sort:            " Total ",
		Order:           "DESC",
		Limit:           " 25 ",
	}, nil)

	require.Equal(t, model.InvoiceQuery{
		Filter: model.InvoiceFilter{
			DateFrom:        date(2026, 10, 5),
			DateTo:          date(2026, 10, 20),
			Lifecycle:       model.LifecycleArchived,
			Statuses:        []model.InvoiceStatus{model.InvoicePaid, model.InvoiceSent},
			SignatureStatus: []model.SignatureStatus{model.SignatureSigned},
			ServiceTypeIDs:  []int64{3, 1},
			TagIDs:          []int64{2},
			PaymentTermIDs:  []int64{7},
			Search:          "Saliba",
		},
		Sort:  model.InvoiceSort{Key: model.SortTotal, Desc: true},
		Limit: 25,
	}, q)
	require.Equal(t, date(2026, 10, 5), res.From)
	require.Equal(t, date(2026, 10, 20), res.To)
}

func TestListAppliesDefaults(t *testing.T) {
	q, res := run(t, service.ListInvoicesParams{}, nil)

	require.Equal(t, model.InvoiceQuery{
		Filter: model.InvoiceFilter{
			DateFrom:  date(2026, 10, 1),
			DateTo:    date(2026, 10, 31),
			Lifecycle: model.LifecycleActive,
		},
		Sort:  model.InvoiceSort{Key: model.SortDate, Desc: true},
		Limit: service.DefaultPageSize,
	}, q)
	require.Equal(t, date(2026, 10, 1), res.From)
	require.Equal(t, date(2026, 10, 31), res.To)
}

func TestListDefaultDateRangeIsTheCurrentUTCMonth(t *testing.T) {
	tests := []struct {
		name     string
		now      time.Time
		wantFrom time.Time
		wantTo   time.Time
	}{
		{"mid month", time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC), date(2026, 10, 1), date(2026, 10, 31)},
		{"last second of January", time.Date(2026, 1, 31, 23, 59, 59, 0, time.UTC), date(2026, 1, 1), date(2026, 1, 31)},
		{"first instant of a month", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), date(2026, 3, 1), date(2026, 3, 31)},
		{"leap February", time.Date(2028, 2, 29, 12, 0, 0, 0, time.UTC), date(2028, 2, 1), date(2028, 2, 29)},
		{"ordinary February", time.Date(2027, 2, 15, 12, 0, 0, 0, time.UTC), date(2027, 2, 1), date(2027, 2, 28)},
		{"December", time.Date(2026, 12, 31, 23, 0, 0, 0, time.UTC), date(2026, 12, 1), date(2026, 12, 31)},
		{"30-day month", time.Date(2026, 4, 30, 8, 0, 0, 0, time.UTC), date(2026, 4, 1), date(2026, 4, 30)},
		{
			"local time late on Oct 31 is already November in UTC",
			time.Date(2026, 10, 31, 23, 30, 0, 0, time.FixedZone("UTC-5", -5*3600)),
			date(2026, 11, 1), date(2026, 11, 30),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newRepoReturning(t, nil)
			res, err := service.NewInvoiceService(repo, fixedClock(tt.now)).List(t.Context(), service.ListInvoicesParams{})
			require.NoError(t, err)
			require.Equal(t, tt.wantFrom, res.From)
			require.Equal(t, tt.wantTo, res.To)
		})
	}
}

func TestListDefaultRangeAppliesOnlyWhenBothDatesAreMissing(t *testing.T) {
	t.Run("only from leaves the end open", func(t *testing.T) {
		q, res := run(t, service.ListInvoicesParams{From: "2026-09-15"}, nil)
		require.Equal(t, date(2026, 9, 15), q.Filter.DateFrom)
		require.True(t, q.Filter.DateTo.IsZero())
		require.True(t, res.To.IsZero())
	})

	t.Run("only to leaves the start open", func(t *testing.T) {
		q, res := run(t, service.ListInvoicesParams{To: "2026-09-15"}, nil)
		require.True(t, q.Filter.DateFrom.IsZero())
		require.Equal(t, date(2026, 9, 15), q.Filter.DateTo)
		require.True(t, res.From.IsZero())
	})

	t.Run("a blank value counts as missing", func(t *testing.T) {
		q, _ := run(t, service.ListInvoicesParams{From: "  ", To: ""}, nil)
		require.Equal(t, date(2026, 10, 1), q.Filter.DateFrom)
		require.Equal(t, date(2026, 10, 31), q.Filter.DateTo)
	})
}

func TestListSortAndOrderDefaults(t *testing.T) {
	tests := []struct {
		sort, order string
		wantKey     model.InvoiceSortKey
		wantDesc    bool
	}{
		{"", "", model.SortDate, true},
		{"date", "", model.SortDate, false},
		{"", "asc", model.SortDate, false},
		{"", "desc", model.SortDate, true},
		{"total", "desc", model.SortTotal, true},
		{"total", "asc", model.SortTotal, false},
		{"customer", "", model.SortCustomer, false},
		{"CUSTOMER", "ASC", model.SortCustomer, false},
		{" status ", " Desc ", model.SortStatus, true},
	}

	for _, tt := range tests {
		t.Run(tt.sort+"/"+tt.order, func(t *testing.T) {
			q, _ := run(t, service.ListInvoicesParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, model.InvoiceSort{Key: tt.wantKey, Desc: tt.wantDesc}, q.Sort)
		})
	}
}

func TestListAcceptsEveryKnownValue(t *testing.T) {
	for _, key := range model.InvoiceSortKeys {
		t.Run("sort "+string(key), func(t *testing.T) {
			q, _ := run(t, service.ListInvoicesParams{Sort: string(key)}, nil)
			require.Equal(t, key, q.Sort.Key)
		})
	}
	for _, l := range model.Lifecycles {
		t.Run("lifecycle "+string(l), func(t *testing.T) {
			q, _ := run(t, service.ListInvoicesParams{Lifecycle: string(l)}, nil)
			require.Equal(t, l, q.Filter.Lifecycle)
		})
	}
	for _, s := range model.InvoiceStatuses {
		t.Run("status "+string(s), func(t *testing.T) {
			q, _ := run(t, service.ListInvoicesParams{Status: []string{string(s)}}, nil)
			require.Equal(t, []model.InvoiceStatus{s}, q.Filter.Statuses)
		})
	}
	for _, s := range model.SignatureStatuses {
		t.Run("signature "+string(s), func(t *testing.T) {
			q, _ := run(t, service.ListInvoicesParams{SignatureStatus: []string{string(s)}}, nil)
			require.Equal(t, []model.SignatureStatus{s}, q.Filter.SignatureStatus)
		})
	}
}
