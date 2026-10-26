package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// PaymentRepository reads the payment report. It works on a pool or inside a transaction.
type PaymentRepository struct {
	conn db.DBTX
}

// NewPaymentRepository returns a repository using conn, typically the shared pool.
func NewPaymentRepository(conn db.DBTX) *PaymentRepository {
	return &PaymentRepository{conn: conn}
}

// List returns one page of the report: up to q.Limit+1 rows, so the caller can tell whether more follow.
func (r *PaymentRepository) List(ctx context.Context, q model.PaymentQuery) ([]model.PaymentRow, error) {
	return listPayments(ctx, r.conn, q)
}

// Summary counts and totals the whole filtered set (not just one page).
func (r *PaymentRepository) Summary(ctx context.Context, f model.PaymentFilter) (model.PaymentSummary, error) {
	return summarizePayments(ctx, r.conn, f)
}

// Page returns one page of the report and, when withSummary is set, the count and total of the whole filtered
// set. The rows and the summary come from one snapshot, so the summary always describes the rows' set even while
// other connections commit. On a pool that is a read-only REPEATABLE READ transaction; a repository already
// bound to a transaction uses the caller's, which is its snapshot.
func (r *PaymentRepository) Page(ctx context.Context, q model.PaymentQuery, withSummary bool) (model.PaymentPage, error) {
	if !withSummary {
		rows, err := listPayments(ctx, r.conn, q)
		if err != nil {
			return model.PaymentPage{}, err
		}
		return model.PaymentPage{Rows: rows}, nil
	}

	var page model.PaymentPage
	read := func(ctx context.Context, conn db.DBTX) error {
		rows, err := listPayments(ctx, conn, q)
		if err != nil {
			return err
		}
		summary, err := summarizePayments(ctx, conn, q.Filter)
		if err != nil {
			return err
		}
		page = model.PaymentPage{Rows: rows, Summary: &summary}
		return nil
	}

	if pool, ok := r.conn.(TxOptionsBeginner); ok {
		err := WithReadSnapshot(ctx, pool, func(ctx context.Context, tx pgx.Tx) error { return read(ctx, tx) })
		return page, err
	}
	return page, read(ctx, r.conn)
}

func listPayments(ctx context.Context, conn db.DBTX, q model.PaymentQuery) ([]model.PaymentRow, error) {
	stmt, err := BuildPaymentListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query payment report: %w", err)
	}
	defer rows.Close()

	out := make([]model.PaymentRow, 0, q.Limit+1)
	for rows.Next() {
		var (
			row            model.PaymentRow
			method, status string
			amount         int64
		)
		if err := rows.Scan(
			&row.ID, &row.CustomerID, &row.CustomerName, &row.AccountNumber, &row.InvoiceID, &row.InvoiceNumber,
			&method, &row.CardBrand, &row.CardLast4, &amount, &row.Date, &status, &row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan payment report row: %w", err)
		}
		row.Method = model.PaymentMethod(method)
		row.Amount = model.Money(amount)
		row.Status = model.PaymentStatus(status)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read payment report rows: %w", err)
	}
	return out, nil
}

func summarizePayments(ctx context.Context, conn db.DBTX, f model.PaymentFilter) (model.PaymentSummary, error) {
	stmt, err := BuildPaymentSummaryQuery(f)
	if err != nil {
		return model.PaymentSummary{}, err
	}

	var (
		s     model.PaymentSummary
		total int64
	)
	if err := conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count, &total); err != nil {
		return model.PaymentSummary{}, fmt.Errorf("query payment summary: %w", err)
	}
	s.Total = model.Money(total)
	return s, nil
}
