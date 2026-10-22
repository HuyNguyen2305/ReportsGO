package repository

import (
	"context"
	"fmt"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// EstimateRepository reads the estimate report. It works on a pool or inside a transaction.
type EstimateRepository struct {
	conn db.DBTX
}

// NewEstimateRepository returns a repository using conn, typically the shared pool.
func NewEstimateRepository(conn db.DBTX) *EstimateRepository {
	return &EstimateRepository{conn: conn}
}

// List returns one page of the report: up to q.Limit+1 rows, so the caller can tell whether more follow.
func (r *EstimateRepository) List(ctx context.Context, q model.EstimateQuery) ([]model.EstimateRow, error) {
	stmt, err := BuildEstimateListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := r.conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query estimate report: %w", err)
	}
	defer rows.Close()

	out := make([]model.EstimateRow, 0, q.Limit+1)
	for rows.Next() {
		var (
			row               model.EstimateRow
			total             int64
			status, signature string
		)
		if err := rows.Scan(
			&row.ID, &row.EstimateNumber, &row.AccountNumber, &row.CustomerID, &row.CustomerName,
			&row.Phone, &row.BillingEmail, &row.CreatedBy, &row.Date,
			&total, &status, &signature, &row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan estimate report row: %w", err)
		}
		row.Total = model.Money(total)
		row.Status = model.EstimateStatus(status)
		row.Signature = model.SignatureStatus(signature)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read estimate report rows: %w", err)
	}
	return out, nil
}

// Summary totals the whole filtered set (not just one page).
func (r *EstimateRepository) Summary(ctx context.Context, f model.EstimateFilter) (model.EstimateSummary, error) {
	stmt, err := BuildEstimateSummaryQuery(f)
	if err != nil {
		return model.EstimateSummary{}, err
	}

	var (
		s     model.EstimateSummary
		total int64
	)
	if err := r.conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count, &total); err != nil {
		return model.EstimateSummary{}, fmt.Errorf("query estimate summary: %w", err)
	}
	s.Total = model.Money(total)
	return s, nil
}
