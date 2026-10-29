package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// PaymentsCollectedRepository reads the Payments Collected report. It works on a pool or inside a transaction.
type PaymentsCollectedRepository struct {
	conn db.DBTX
	q    *db.Queries
}

// NewPaymentsCollectedRepository returns a repository using conn, typically the shared pool.
func NewPaymentsCollectedRepository(conn db.DBTX) *PaymentsCollectedRepository {
	return &PaymentsCollectedRepository{conn: conn, q: db.New(conn)}
}

// Page returns one page of the report (up to q.Limit+1 rows, so the caller can tell whether more follow) and,
// when withSummary is set, the totals of the whole filtered set. The rows and the totals come from one snapshot,
// so the summary always describes the rows' set even while other connections commit. On a pool that is a
// read-only REPEATABLE READ transaction; a repository already bound to a transaction uses the caller's, which
// is its snapshot.
func (r *PaymentsCollectedRepository) Page(ctx context.Context, q model.PaymentCollectedQuery, withSummary bool) (model.PaymentCollectedPage, error) {
	if !withSummary {
		rows, err := listPaymentsCollected(ctx, r.conn, q)
		if err != nil {
			return model.PaymentCollectedPage{}, err
		}
		return model.PaymentCollectedPage{Rows: rows}, nil
	}

	var page model.PaymentCollectedPage
	read := func(ctx context.Context, conn db.DBTX) error {
		rows, err := listPaymentsCollected(ctx, conn, q)
		if err != nil {
			return err
		}
		summary, err := summarizePaymentsCollected(ctx, conn, q.Filter)
		if err != nil {
			return err
		}
		page = model.PaymentCollectedPage{Rows: rows, Summary: &summary}
		return nil
	}

	if pool, ok := r.conn.(TxOptionsBeginner); ok {
		err := WithReadSnapshot(ctx, pool, func(ctx context.Context, tx pgx.Tx) error { return read(ctx, tx) })
		return page, err
	}
	return page, read(ctx, r.conn)
}

func listPaymentsCollected(ctx context.Context, conn db.DBTX, q model.PaymentCollectedQuery) ([]model.PaymentCollectedRow, error) {
	stmt, err := BuildPaymentsCollectedListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query payments collected report: %w", err)
	}
	defer rows.Close()

	out := make([]model.PaymentCollectedRow, 0, q.Limit+1)
	for rows.Next() {
		var (
			row                model.PaymentCollectedRow
			method             string
			amount, withoutTax int64
		)
		if err := rows.Scan(
			&row.ID, &row.CustomerID, &row.CustomerName, &row.InvoiceID, &row.InvoiceNumber,
			&method, &row.CardBrand, &row.CardLast4, &row.ServiceState,
			&row.StaffID, &row.StaffName, &row.Date, &amount, &withoutTax, &row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan payments collected report row: %w", err)
		}
		row.Method = model.PaymentMethod(method)
		row.Amount = model.Money(amount)
		row.WithoutTax = model.Money(withoutTax)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read payments collected report rows: %w", err)
	}
	return out, nil
}

func summarizePaymentsCollected(ctx context.Context, conn db.DBTX, f model.PaymentCollectedFilter) (model.PaymentCollectedSummary, error) {
	stmt, err := BuildPaymentsCollectedSummaryQuery(f)
	if err != nil {
		return model.PaymentCollectedSummary{}, err
	}

	var (
		s                  model.PaymentCollectedSummary
		amount, withoutTax int64
	)
	if err := conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count, &amount, &withoutTax); err != nil {
		return model.PaymentCollectedSummary{}, fmt.Errorf("query payments collected summary: %w", err)
	}
	s.Amount = model.Money(amount)
	s.WithoutTax = model.Money(withoutTax)
	return s, nil
}

// Staff lists the options for the Staff filter.
func (r *PaymentsCollectedRepository) Staff(ctx context.Context) ([]model.StaffOption, error) {
	rows, err := r.q.ListStaff(ctx)
	if err != nil {
		return nil, fmt.Errorf("list staff: %w", err)
	}
	out := make([]model.StaffOption, len(rows))
	for i, row := range rows {
		out[i] = model.StaffOption{ID: row.ID, Name: row.FullName}
	}
	return out, nil
}
