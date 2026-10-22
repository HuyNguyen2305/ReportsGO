package repository

import (
	"context"
	"fmt"

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
	stmt, err := BuildPaymentListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := r.conn.Query(ctx, stmt.SQL, stmt.Args...)
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

// Summary counts and totals the whole filtered set (not just one page).
func (r *PaymentRepository) Summary(ctx context.Context, f model.PaymentFilter) (model.PaymentSummary, error) {
	stmt, err := BuildPaymentSummaryQuery(f)
	if err != nil {
		return model.PaymentSummary{}, err
	}

	var (
		s     model.PaymentSummary
		total int64
	)
	if err := r.conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count, &total); err != nil {
		return model.PaymentSummary{}, fmt.Errorf("query payment summary: %w", err)
	}
	s.Total = model.Money(total)
	return s, nil
}
