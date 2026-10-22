package repository

import (
	"context"
	"fmt"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// CreditRepository reads the credit report. It works on a pool or inside a transaction.
type CreditRepository struct {
	conn db.DBTX
}

// NewCreditRepository returns a repository using conn, typically the shared pool.
func NewCreditRepository(conn db.DBTX) *CreditRepository {
	return &CreditRepository{conn: conn}
}

// List returns one page of the report: up to q.Limit+1 rows, so the caller can tell whether more follow.
func (r *CreditRepository) List(ctx context.Context, q model.CreditQuery) ([]model.CreditRow, error) {
	stmt, err := BuildCreditListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := r.conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query credit report: %w", err)
	}
	defer rows.Close()

	out := make([]model.CreditRow, 0, q.Limit+1)
	for rows.Next() {
		var (
			row    model.CreditRow
			method string
			amount int64
		)
		if err := rows.Scan(
			&row.ID, &row.CustomerID, &row.CustomerName, &row.AccountNumber, &method, &row.MethodDetail,
			&amount, &row.Date, &row.CreatedBy, &row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan credit report row: %w", err)
		}
		row.Method = model.PaymentMethod(method)
		row.Amount = model.Money(amount)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read credit report rows: %w", err)
	}
	return out, nil
}

// Summary counts and totals the whole filtered set (not just one page).
func (r *CreditRepository) Summary(ctx context.Context, f model.CreditFilter) (model.CreditSummary, error) {
	stmt, err := BuildCreditSummaryQuery(f)
	if err != nil {
		return model.CreditSummary{}, err
	}

	var (
		s     model.CreditSummary
		total int64
	)
	if err := r.conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count, &total); err != nil {
		return model.CreditSummary{}, fmt.Errorf("query credit summary: %w", err)
	}
	s.Total = model.Money(total)
	return s, nil
}
