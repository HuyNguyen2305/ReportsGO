package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// AccountsAgingRepository reads the Accounts Aging report. It works on a pool or inside a transaction.
type AccountsAgingRepository struct {
	conn db.DBTX
	q    *db.Queries
}

// NewAccountsAgingRepository returns a repository using conn, typically the shared pool.
func NewAccountsAgingRepository(conn db.DBTX) *AccountsAgingRepository {
	return &AccountsAgingRepository{conn: conn, q: db.New(conn)}
}

// Page returns one page of the report (up to q.Limit+1 rows, so the caller can tell whether more follow) and,
// when withSummary is set, the totals of the whole filtered set. The rows and the totals come from one snapshot,
// so the summary always describes the rows' set even while other connections commit. On a pool that is a
// read-only REPEATABLE READ transaction; a repository already bound to a transaction uses the caller's, which
// is its snapshot.
func (r *AccountsAgingRepository) Page(ctx context.Context, q model.AgingQuery, withSummary bool) (model.AgingPage, error) {
	if !withSummary {
		rows, err := listAging(ctx, r.conn, q)
		if err != nil {
			return model.AgingPage{}, err
		}
		return model.AgingPage{Rows: rows}, nil
	}

	var page model.AgingPage
	read := func(ctx context.Context, conn db.DBTX) error {
		rows, err := listAging(ctx, conn, q)
		if err != nil {
			return err
		}
		summary, err := summarizeAging(ctx, conn, q.Filter)
		if err != nil {
			return err
		}
		page = model.AgingPage{Rows: rows, Summary: &summary}
		return nil
	}

	if pool, ok := r.conn.(TxOptionsBeginner); ok {
		err := WithReadSnapshot(ctx, pool, func(ctx context.Context, tx pgx.Tx) error { return read(ctx, tx) })
		return page, err
	}
	return page, read(ctx, r.conn)
}

func listAging(ctx context.Context, conn db.DBTX, q model.AgingQuery) ([]model.AgingRow, error) {
	stmt, err := BuildAccountsAgingListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query accounts aging report: %w", err)
	}
	defer rows.Close()

	out := make([]model.AgingRow, 0, q.Limit+1)
	for rows.Next() {
		var (
			row                          model.AgingRow
			processor, last4             *string
			expMonth, expYear            *int16
			d0, d31, d61, d90, tot, cred int64
		)
		if err := rows.Scan(
			&row.ID, &row.CustomerID, &row.LocationID, &row.CustomerName, &row.Location, &row.BillingEmail, &row.Phone,
			&processor, &last4, &expMonth, &expYear,
			&d0, &d31, &d61, &d90, &tot, &cred, &row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan accounts aging report row: %w", err)
		}
		if processor != nil && last4 != nil && expMonth != nil && expYear != nil {
			row.Card = &model.AgingCard{Processor: *processor, Last4: *last4, ExpMonth: int(*expMonth), ExpYear: int(*expYear)}
		}
		row.D0to30, row.D31to60, row.D61to90, row.D90Plus = model.Money(d0), model.Money(d31), model.Money(d61), model.Money(d90)
		row.Total, row.AvailableCredit = model.Money(tot), model.Money(cred)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read accounts aging report rows: %w", err)
	}
	return out, nil
}

func summarizeAging(ctx context.Context, conn db.DBTX, f model.AgingFilter) (model.AgingSummary, error) {
	stmt, err := BuildAccountsAgingSummaryQuery(f)
	if err != nil {
		return model.AgingSummary{}, err
	}

	var (
		s                            model.AgingSummary
		d0, d31, d61, d90, tot, cred int64
	)
	if err := conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count, &d0, &d31, &d61, &d90, &tot, &cred); err != nil {
		return model.AgingSummary{}, fmt.Errorf("query accounts aging summary: %w", err)
	}
	s.D0to30, s.D31to60, s.D61to90, s.D90Plus = model.Money(d0), model.Money(d31), model.Money(d61), model.Money(d90)
	s.Total, s.AvailableCredit = model.Money(tot), model.Money(cred)
	return s, nil
}

// ServiceTypes lists the options for the Service Type filter.
func (r *AccountsAgingRepository) ServiceTypes(ctx context.Context) ([]model.ServiceType, error) {
	rows, err := r.q.ListServiceTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list service types: %w", err)
	}
	out := make([]model.ServiceType, len(rows))
	for i, row := range rows {
		out[i] = model.ServiceType{ID: row.ID, Name: row.Name}
	}
	return out, nil
}
