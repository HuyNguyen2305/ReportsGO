//go:build integration

package invoicerepo_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/repository"
	"reportsgo/tests/integration/testdb"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// env is a migrated, seeded throwaway database with a repository on top.
type env struct {
	pool *pgxpool.Pool
	repo *repository.InvoiceRepository
}

// newEnv seeds the database. With withNulls it also adds October invoices that have no job, no billing
// email, no payment terms and a tied date, to exercise NULL sort keys and ties.
func newEnv(t *testing.T, withNulls bool) env {
	t.Helper()
	ctx := context.Background()

	pool := testdb.SeededPool(t)

	if withNulls {
		_, err := pool.Exec(ctx, `
			INSERT INTO billing_documents (document_number, customer_id, document_date, total, discount, status)
			SELECT 'N' || g, c.id, '2026-10-10', 10 * g, g, 'draft'
			FROM generate_series(1, 3) g
			JOIN customers c ON c.account_number = '63716'`)
		require.NoError(t, err)
	}
	return env{pool: pool, repo: repository.NewInvoiceRepository(pool)}
}

func octoberActive() model.InvoiceFilter {
	return model.InvoiceFilter{
		Lifecycle: model.LifecycleActive,
		DateFrom:  day(2026, 10, 1),
		DateTo:    day(2026, 10, 31),
	}
}

func query(f model.InvoiceFilter, key model.InvoiceSortKey, desc bool, limit int) model.InvoiceQuery {
	return model.InvoiceQuery{Filter: f, Sort: model.InvoiceSort{Key: key, Desc: desc}, Limit: limit}
}

// listAll fetches everything matching the filter in one page.
func (e env) listAll(t *testing.T, f model.InvoiceFilter, key model.InvoiceSortKey, desc bool) []model.InvoiceRow {
	t.Helper()
	rows, err := e.repo.List(context.Background(), query(f, key, desc, 1000))
	require.NoError(t, err)
	return rows
}

func numbers(rows []model.InvoiceRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.InvoiceNumber
	}
	return out
}

func ids(rows []model.InvoiceRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// the 21 active October invoices in the seed (84890 is dated in September).
var octoberActiveNumbers = []string{
	"84936", "85030", "85011", "85010", "84942", "85028", "85007", "84937", "85009", "85000", "85005",
	"85004", "84985", "84979", "84984", "84983", "84982", "84975", "85031", "85032", "85033",
}

func TestListDefaultReturnsActiveOctoberInvoicesNewestFirst(t *testing.T) {
	e := newEnv(t, false)

	rows := e.listAll(t, octoberActive(), model.SortDate, true)

	require.ElementsMatch(t, octoberActiveNumbers, numbers(rows))
	require.Equal(t, "84936", rows[0].InvoiceNumber, "10/26 is the newest")
	for i := 1; i < len(rows); i++ {
		prev, cur := rows[i-1], rows[i]
		require.False(t, cur.Date.After(prev.Date), "dates must not increase")
		if cur.Date.Equal(prev.Date) {
			require.Less(t, cur.ID, prev.ID, "ties are broken by id in the same direction")
		}
	}
}

func TestListReturnsAllReportFields(t *testing.T) {
	e := newEnv(t, false)
	f := octoberActive()
	f.Search = "85005"

	rows := e.listAll(t, f, model.SortDate, true)

	require.Len(t, rows, 1)
	r := rows[0]
	require.Equal(t, "85005", r.InvoiceNumber)
	require.Equal(t, "63714", r.AccountNumber)
	require.Equal(t, "#F Last Name", r.CustomerName)
	require.NotZero(t, r.CustomerID)
	require.NotNil(t, r.Job)
	require.Equal(t, "Blank Service 123 (S1 S2)", *r.Job)
	require.Equal(t, "With each job", r.FrequencyLabel)
	require.NotNil(t, r.BillingEmail)
	require.Equal(t, "d@cm.co", *r.BillingEmail)
	require.Equal(t, day(2026, 10, 11), r.Date)
	require.Equal(t, model.Money(15226), r.Total)
	require.Equal(t, model.Money(0), r.Discount)
	require.Equal(t, []string{"a", "b"}, r.Tags)
	require.NotNil(t, r.PaymentTermsName)
	require.Equal(t, "Net 30 Terms", *r.PaymentTermsName)
	require.Contains(t, *r.PaymentTermsDescription, "30 days")
	require.Equal(t, model.InvoicePaid, r.Status)
	require.Equal(t, model.SignatureDraft, r.SignatureStatus)
	require.Equal(t, "2026-10-11", r.SortValue)

	f.Search = "85009"
	rows = e.listAll(t, f, model.SortDate, true)
	require.Len(t, rows, 1)
	require.Equal(t, model.Money(14856), rows[0].Total)
	require.Equal(t, model.Money(123), rows[0].Discount)
	require.Empty(t, rows[0].Tags, "an invoice without tags has an empty list, not nil-vs-NULL trouble")
}

func TestListHandlesMissingJobEmailAndTerms(t *testing.T) {
	e := newEnv(t, true)
	f := octoberActive()
	f.Search = "N2"

	rows := e.listAll(t, f, model.SortDate, true)

	require.Len(t, rows, 1)
	require.Nil(t, rows[0].Job)
	require.Nil(t, rows[0].BillingEmail)
	require.Nil(t, rows[0].PaymentTermsName)
	require.Nil(t, rows[0].PaymentTermsDescription)
}

// sortCast is how each sort key's text value is compared; it mirrors the repository's sort table.
var sortCast = map[model.InvoiceSortKey]string{
	model.SortAccountNumber: "text", model.SortInvoiceNumber: "text", model.SortCustomer: "text",
	model.SortJob: "text", model.SortBillingEmail: "text", model.SortDate: "date",
	model.SortTotal: "numeric", model.SortDiscount: "numeric", model.SortStatus: "text",
	model.SortSignatureStatus: "text", model.SortPaymentTerms: "text",
}

// compare asks Postgres to order two sort values, so text keys use the same collation as ORDER BY.
func (e env) compare(t *testing.T, key model.InvoiceSortKey, a, b string) int {
	t.Helper()
	cast := sortCast[key]
	var c int
	sql := fmt.Sprintf(`SELECT CASE WHEN $1::text::%[1]s < $2::text::%[1]s THEN -1
	                                WHEN $1::text::%[1]s = $2::text::%[1]s THEN 0 ELSE 1 END`, cast)
	require.NoError(t, e.pool.QueryRow(context.Background(), sql, a, b).Scan(&c))
	return c
}

func TestEverySortKeyOrdersCorrectlyInBothDirections(t *testing.T) {
	e := newEnv(t, true)
	require.Len(t, sortCast, len(model.InvoiceSortKeys), "every sort key must be covered")

	for _, key := range model.InvoiceSortKeys {
		for _, desc := range []bool{false, true} {
			name := fmt.Sprintf("%s/desc=%v", key, desc)
			t.Run(name, func(t *testing.T) {
				rows := e.listAll(t, octoberActive(), key, desc)
				require.Len(t, rows, 24)

				ties := 0
				for i := 1; i < len(rows); i++ {
					prev, cur := rows[i-1], rows[i]
					c := e.compare(t, key, prev.SortValue, cur.SortValue)
					if desc {
						require.GreaterOrEqual(t, c, 0, "row %d (%s) must not sort above row %d (%s)", i, cur.SortValue, i-1, prev.SortValue)
					} else {
						require.LessOrEqual(t, c, 0, "row %d (%s) must not sort below row %d (%s)", i, cur.SortValue, i-1, prev.SortValue)
					}
					if c == 0 {
						ties++
						if desc {
							require.Greater(t, prev.ID, cur.ID, "ties are broken by id, same direction")
						} else {
							require.Less(t, prev.ID, cur.ID, "ties are broken by id, same direction")
						}
					}
				}
				if key == model.SortDate || key == model.SortStatus || key == model.SortSignatureStatus {
					require.Positive(t, ties, "this key should have tied values in the data, or the tie-break goes untested")
				}
			})
		}
	}
}

func TestKeysetPagingWalksEveryKeyAndDirectionWithoutGapsOrDuplicates(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()

	for _, key := range model.InvoiceSortKeys {
		for _, desc := range []bool{false, true} {
			for _, limit := range []int{1, 5} {
				t.Run(fmt.Sprintf("%s/desc=%v/limit=%d", key, desc, limit), func(t *testing.T) {
					want := ids(e.listAll(t, octoberActive(), key, desc))

					var got []int64
					q := query(octoberActive(), key, desc, limit)
					for pages := 0; ; pages++ {
						require.Less(t, pages, 100, "paging must terminate")

						rows, err := e.repo.List(ctx, q)
						require.NoError(t, err)
						page := rows
						hasMore := len(rows) > limit
						if hasMore {
							page = rows[:limit]
						}
						got = append(got, ids(page)...)
						if !hasMore {
							break
						}
						last := page[len(page)-1]
						q.After = &model.InvoiceCursor{SortValue: last.SortValue, ID: last.ID}
					}

					require.Equal(t, want, got, "walking the pages must reproduce the full ordered list exactly")
					require.Len(t, got, 24)
				})
			}
		}
	}
}

func TestListFilters(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	serviceTypes, err := e.repo.ServiceTypes(ctx)
	require.NoError(t, err)
	serviceID := map[string]int64{}
	for _, s := range serviceTypes {
		serviceID[s.Name] = s.ID
	}
	tags, err := e.repo.Tags(ctx)
	require.NoError(t, err)
	tagID := map[string]int64{}
	for _, tg := range tags {
		tagID[tg.Name] = tg.ID
	}
	terms, err := e.repo.PaymentTerms(ctx)
	require.NoError(t, err)
	termID := map[string]int64{}
	for _, p := range terms {
		termID[p.Name] = p.ID
	}

	// Invoices by customer: the report's "Job" and service filter follow each customer's latest job.
	saliba := []string{"84936", "84942", "85007", "84937", "85000", "84985", "84979", "84975", "85031"}
	aaa1 := []string{"85011", "85010", "85009", "85004", "85032"}
	fLast := []string{"85030", "85028", "85005"}
	saka := []string{"84984", "84983", "84982", "85033"}
	concat := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}

	with := func(mutate func(*model.InvoiceFilter)) model.InvoiceFilter {
		f := octoberActive()
		mutate(&f)
		return f
	}

	tests := []struct {
		name   string
		filter model.InvoiceFilter
		want   []string
	}{
		{"no extra filters", octoberActive(), octoberActiveNumbers},

		{"status paid", with(func(f *model.InvoiceFilter) { f.Statuses = []model.InvoiceStatus{model.InvoicePaid} }),
			[]string{"85011", "85010", "85009", "85000", "85005", "85004", "84985", "84979", "84975"}},
		{"status draft", with(func(f *model.InvoiceFilter) { f.Statuses = []model.InvoiceStatus{model.InvoiceDraft} }),
			[]string{"84936", "85030", "84942", "85028", "84937", "84984", "84983", "84982"}},
		{"status sent or void", with(func(f *model.InvoiceFilter) {
			f.Statuses = []model.InvoiceStatus{model.InvoiceSent, model.InvoiceVoid}
		}), []string{"85031", "85032", "85033", "85007"}},

		{"signature signed", with(func(f *model.InvoiceFilter) { f.SignatureStatus = []model.SignatureStatus{model.SignatureSigned} }),
			[]string{"85032"}},
		{"signature pending or signed", with(func(f *model.InvoiceFilter) {
			f.SignatureStatus = []model.SignatureStatus{model.SignaturePending, model.SignatureSigned}
		}), []string{"85031", "85032"}},
		{"signature declined matches nothing", with(func(f *model.InvoiceFilter) {
			f.SignatureStatus = []model.SignatureStatus{model.SignatureDeclined}
		}), nil},

		{"service Bi-Monthly", with(func(f *model.InvoiceFilter) { f.ServiceTypeIDs = []int64{serviceID["Bi-Monthly Service"]} }),
			concat(saliba, aaa1)},
		{"service Blank", with(func(f *model.InvoiceFilter) { f.ServiceTypeIDs = []int64{serviceID["Blank Service 123"]} }), fLast},
		{"service Blank or Initial", with(func(f *model.InvoiceFilter) {
			f.ServiceTypeIDs = []int64{serviceID["Blank Service 123"], serviceID["Initial Service"]}
		}), concat(fLast, saka)},

		{"tag a", with(func(f *model.InvoiceFilter) { f.TagIDs = []int64{tagID["a"]} }), []string{"85030", "85028", "85005"}},
		{"tag c", with(func(f *model.InvoiceFilter) { f.TagIDs = []int64{tagID["c"]} }), []string{"85030", "85028"}},
		{"tags b or c is any-of, no duplicates", with(func(f *model.InvoiceFilter) { f.TagIDs = []int64{tagID["b"], tagID["c"]} }),
			[]string{"85030", "85028", "85005"}},

		{"terms Net 30", with(func(f *model.InvoiceFilter) { f.PaymentTermIDs = []int64{termID["Net 30 Terms"]} }), []string{"85005"}},

		{"single day is inclusive at both ends", with(func(f *model.InvoiceFilter) {
			f.DateFrom, f.DateTo = day(2026, 10, 12), day(2026, 10, 12)
		}), []string{"84937", "85009", "85000"}},
		{"two days", with(func(f *model.InvoiceFilter) {
			f.DateFrom, f.DateTo = day(2026, 10, 11), day(2026, 10, 12)
		}), []string{"84937", "85009", "85000", "85005", "85004", "84985"}},
		{"open start", with(func(f *model.InvoiceFilter) { f.DateFrom = time.Time{}; f.DateTo = day(2026, 10, 9) }),
			[]string{"84984", "84983", "84982", "84975", "84890"}},
		{"from 10/22", with(func(f *model.InvoiceFilter) { f.DateFrom = day(2026, 10, 22) }), []string{"84936", "85031"}},
		{"range outside the data", with(func(f *model.InvoiceFilter) {
			f.DateFrom, f.DateTo = day(2025, 1, 1), day(2025, 12, 31)
		}), nil},

		{"archived tab", with(func(f *model.InvoiceFilter) { f.Lifecycle = model.LifecycleArchived }), []string{"85034"}},
		{"deleted tab", with(func(f *model.InvoiceFilter) { f.Lifecycle = model.LifecycleDeleted }), []string{"85035"}},

		{"search invoice number", with(func(f *model.InvoiceFilter) { f.Search = "85030" }), []string{"85030"}},
		{"search invoice number prefix", with(func(f *model.InvoiceFilter) { f.Search = "8503" }),
			[]string{"85030", "85031", "85032", "85033"}},
		{"search account number", with(func(f *model.InvoiceFilter) { f.Search = "63706" }), aaa1},
		{"search customer name, case-insensitive", with(func(f *model.InvoiceFilter) { f.Search = "SaLiBa" }), saliba},
		{"search customer name part", with(func(f *model.InvoiceFilter) { f.Search = "ccc1" }), aaa1},
		{"search billing email", with(func(f *model.InvoiceFilter) { f.Search = "d@cm.co" }), fLast},
		{"search email domain", with(func(f *model.InvoiceFilter) { f.Search = "yopmail" }), concat(saliba, aaa1, saka)},
		{"search with no match", with(func(f *model.InvoiceFilter) { f.Search = "zzz-nothing" }), nil},
		{"percent is literal", with(func(f *model.InvoiceFilter) { f.Search = "%" }), nil},
		{"underscore is literal", with(func(f *model.InvoiceFilter) { f.Search = "8503_" }), nil},
		{"backslash is literal", with(func(f *model.InvoiceFilter) { f.Search = `\` }), nil},
		{"sql text is just text", with(func(f *model.InvoiceFilter) { f.Search = `'; DROP TABLE invoices; --` }), nil},

		{"combination: paid and Net 30", with(func(f *model.InvoiceFilter) {
			f.Statuses = []model.InvoiceStatus{model.InvoicePaid}
			f.PaymentTermIDs = []int64{termID["Net 30 Terms"]}
		}), []string{"85005"}},
		{"combination: search and status", with(func(f *model.InvoiceFilter) {
			f.Search = "saliba"
			f.Statuses = []model.InvoiceStatus{model.InvoicePaid}
		}), []string{"85000", "84985", "84979", "84975"}},
		{"combination that excludes everything", with(func(f *model.InvoiceFilter) {
			f.Statuses = []model.InvoiceStatus{model.InvoiceVoid}
			f.Search = "ccc1"
		}), nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := e.listAll(t, tt.filter, model.SortInvoiceNumber, false)
			require.ElementsMatch(t, tt.want, numbers(rows))

			// The summary must describe exactly the same set of invoices.
			summary, err := e.repo.Summary(ctx, tt.filter)
			require.NoError(t, err)
			require.EqualValues(t, len(tt.want), summary.Count)
		})
	}

	// the database is still intact after the hostile search above
	var n int
	require.NoError(t, e.pool.QueryRow(ctx, "SELECT count(*) FROM billing_documents WHERE parent_type = 'invoice'").Scan(&n))
	require.Equal(t, 24, n)
}

func TestSummary(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	t.Run("totals the whole filtered set, with outstanding of $264.00", func(t *testing.T) {
		rows := e.listAll(t, octoberActive(), model.SortDate, true)
		var wantTotal, wantDiscount model.Money
		for _, r := range rows {
			wantTotal += r.Total
			wantDiscount += r.Discount
		}

		s, err := e.repo.Summary(ctx, octoberActive())
		require.NoError(t, err)

		require.EqualValues(t, 21, s.Count)
		require.Equal(t, wantTotal, s.Total, "must equal the sum of the listed rows")
		require.Equal(t, wantDiscount, s.Discount)
		require.Equal(t, model.Money(123), s.Discount)
		require.Equal(t, model.Money(26400), s.Outstanding, "95.00 + (100.00 - 30.00) + 99.00")
		require.Equal(t, "264.00", s.Outstanding.String())
	})

	t.Run("follows the filters", func(t *testing.T) {
		tests := []struct {
			name        string
			mutate      func(*model.InvoiceFilter)
			wantCount   int64
			wantTotal   model.Money
			wantOutstan model.Money
		}{
			{"sent only", func(f *model.InvoiceFilter) { f.Statuses = []model.InvoiceStatus{model.InvoiceSent} }, 3, 29400, 26400},
			{"paid has nothing outstanding", func(f *model.InvoiceFilter) { f.Statuses = []model.InvoiceStatus{model.InvoicePaid} }, 9, 0, 0},
			{"one sent invoice", func(f *model.InvoiceFilter) { f.Search = "85032" }, 1, 10000, 7000},
			{"draft invoices are not outstanding", func(f *model.InvoiceFilter) { f.Statuses = []model.InvoiceStatus{model.InvoiceDraft} }, 8, 0, 0},
			{"empty result", func(f *model.InvoiceFilter) { f.Search = "zzz-nothing" }, 0, 0, 0},
			{"archived tab", func(f *model.InvoiceFilter) { f.Lifecycle = model.LifecycleArchived }, 1, 4000, 0},
			{"deleted tab", func(f *model.InvoiceFilter) { f.Lifecycle = model.LifecycleDeleted }, 1, 2500, 0},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				f := octoberActive()
				tt.mutate(&f)
				s, err := e.repo.Summary(ctx, f)
				require.NoError(t, err)

				require.Equal(t, tt.wantCount, s.Count)
				if tt.wantTotal != 0 {
					require.Equal(t, tt.wantTotal, s.Total)
				}
				require.Equal(t, tt.wantOutstan, s.Outstanding)
			})
		}
	})

	t.Run("totals include void and draft invoices unless a status filter leaves them out", func(t *testing.T) {
		all, err := e.repo.Summary(ctx, octoberActive())
		require.NoError(t, err)

		f := octoberActive()
		f.Statuses = []model.InvoiceStatus{model.InvoiceVoid}
		void, err := e.repo.Summary(ctx, f)
		require.NoError(t, err)
		require.EqualValues(t, 1, void.Count)
		require.Equal(t, model.Money(6500), void.Total, "invoice 85007 is void")

		f.Statuses = []model.InvoiceStatus{model.InvoiceDraft, model.InvoiceSent, model.InvoicePaid}
		live, err := e.repo.Summary(ctx, f)
		require.NoError(t, err)

		require.Equal(t, all.Count, live.Count+void.Count)
		require.Equal(t, all.Total, live.Total+void.Total, "the default total counts the void invoice")
		require.Equal(t, all.Discount, live.Discount+void.Discount)
		require.Equal(t, all.Outstanding, live.Outstanding, "void invoices are never outstanding")
	})

	t.Run("only live, paid payments reduce the outstanding balance", func(t *testing.T) {
		exec := func(sql string) {
			_, err := e.pool.Exec(ctx, sql)
			require.NoError(t, err)
		}
		outstanding := func() model.Money {
			s, err := e.repo.Summary(ctx, octoberActive())
			require.NoError(t, err)
			return s.Outstanding
		}
		const base = `INSERT INTO payments (customer_id, invoice_id, method_type, amount, payment_date, status, deleted_at)
			SELECT customer_id, id, 'cash', %s FROM billing_documents WHERE parent_type = 'invoice' AND document_number = '85033'`

		exec(fmt.Sprintf(base, `50, '2026-10-19', 'paid', now()`)) // deleted payment: ignored
		require.Equal(t, model.Money(26400), outstanding())

		exec(fmt.Sprintf(base, `20, '2026-10-19', 'pending', NULL`)) // not settled: ignored
		require.Equal(t, model.Money(26400), outstanding())

		exec(fmt.Sprintf(base, `200, '2026-10-19', 'paid', NULL`)) // overpayment: balance floors at zero
		require.Equal(t, model.Money(16500), outstanding(), "95.00 + 70.00 + 0.00, never negative")
	})
}

func TestLookups(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	serviceTypes, err := e.repo.ServiceTypes(ctx)
	require.NoError(t, err)
	names := make([]string, len(serviceTypes))
	for i, s := range serviceTypes {
		require.NotZero(t, s.ID)
		names[i] = s.Name
	}
	require.Equal(t, []string{"Bi-Monthly Service", "Blank Service 123", "Call Back Service", "Full service", "Initial Service"}, names)

	tags, err := e.repo.Tags(ctx)
	require.NoError(t, err)
	require.Len(t, tags, 3)
	require.Equal(t, []string{"a", "b", "c"}, []string{tags[0].Name, tags[1].Name, tags[2].Name})

	terms, err := e.repo.PaymentTerms(ctx)
	require.NoError(t, err)
	require.Len(t, terms, 2)
	require.Equal(t, "Due on receipt", terms[0].Name)
	require.Equal(t, "Payment is due on receipt.", terms[0].Description)
	require.Equal(t, "Net 30 Terms", terms[1].Name)
}

func TestRepositoryWorksInsideATransaction(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	tx, err := e.pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `INSERT INTO billing_documents (document_number, customer_id, document_date, total)
		SELECT 'TX1', id, '2026-10-15', 5 FROM customers WHERE account_number = '5001'`)
	require.NoError(t, err)

	inTx, err := repository.NewInvoiceRepository(tx).Summary(ctx, octoberActive())
	require.NoError(t, err)
	outside, err := e.repo.Summary(ctx, octoberActive())
	require.NoError(t, err)

	require.EqualValues(t, 22, inTx.Count, "the transaction sees its own uncommitted row")
	require.EqualValues(t, 21, outside.Count, "other connections do not")

	rows, err := repository.NewInvoiceRepository(tx).List(ctx, query(octoberActive(), model.SortInvoiceNumber, false, 100))
	require.NoError(t, err)
	require.Contains(t, numbers(rows), "TX1")
}

func TestQueriesHonorContextCancellation(t *testing.T) {
	e := newEnv(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.repo.List(ctx, query(octoberActive(), model.SortDate, true, 10))
	require.ErrorIs(t, err, context.Canceled)

	_, err = e.repo.Summary(ctx, octoberActive())
	require.ErrorIs(t, err, context.Canceled)
}

func TestInvalidQueriesAreRejectedBeforeReachingTheDatabase(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	_, err := e.repo.List(ctx, query(octoberActive(), "nope", false, 10))
	require.ErrorIs(t, err, repository.ErrInvalidQuery)

	_, err = e.repo.List(ctx, query(octoberActive(), model.SortDate, false, 0))
	require.ErrorIs(t, err, repository.ErrInvalidQuery)

	bad := octoberActive()
	bad.Lifecycle = "bogus"
	_, err = e.repo.Summary(ctx, bad)
	require.ErrorIs(t, err, repository.ErrInvalidQuery)
}

// billing_documents holds estimates next to invoices; the invoice report must never see them, even when an
// estimate shares an invoice's number.
func TestEstimatesNeverAppearInInvoiceResults(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()

	var estimates int
	require.NoError(t, e.pool.QueryRow(ctx, `SELECT count(*) FROM billing_documents
		WHERE parent_type = 'estimate' AND deleted_at IS NULL AND archived_at IS NULL
		  AND document_date BETWEEN '2026-10-01' AND '2026-10-31'`).Scan(&estimates))
	require.Positive(t, estimates, "the seed has October estimates, so this test is meaningful")

	rows := e.listAll(t, octoberActive(), model.SortDate, true)
	require.Len(t, rows, 21, "estimates do not change the invoice count")
	summary, err := e.repo.Summary(ctx, octoberActive())
	require.NoError(t, err)
	require.EqualValues(t, 21, summary.Count)

	_, err = e.pool.Exec(ctx, `INSERT INTO billing_documents (parent_type, document_number, customer_id, document_date, total)
		SELECT 'estimate', '85033', id, '2026-10-18', 777 FROM customers WHERE account_number = '63689'`)
	require.NoError(t, err)

	f := octoberActive()
	f.Search = "85033"
	got, err := e.repo.List(ctx, query(f, model.SortDate, true, 10))
	require.NoError(t, err)
	require.Len(t, got, 1, "only the invoice with that number")
	require.Equal(t, model.Money(9900), got[0].Total)
	s, err := e.repo.Summary(ctx, f)
	require.NoError(t, err)
	require.EqualValues(t, 1, s.Count)
	require.Equal(t, model.Money(9900), s.Total)
}
