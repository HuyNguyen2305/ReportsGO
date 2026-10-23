package repository

import (
	"context"
	"fmt"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// CustomerRepository reads the new customers report. It works on a pool or inside a transaction.
type CustomerRepository struct {
	conn db.DBTX
	q    *db.Queries
}

// NewCustomerRepository returns a repository using conn, typically the shared pool.
func NewCustomerRepository(conn db.DBTX) *CustomerRepository {
	return &CustomerRepository{conn: conn, q: db.New(conn)}
}

// List returns one page of the report: up to q.Limit+1 rows, so the caller can tell whether more follow.
func (r *CustomerRepository) List(ctx context.Context, q model.CustomerQuery) ([]model.CustomerRow, error) {
	stmt, err := BuildCustomerListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := r.conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query new customers report: %w", err)
	}
	defer rows.Close()

	out := make([]model.CustomerRow, 0, q.Limit+1)
	for rows.Next() {
		var row model.CustomerRow
		if err := rows.Scan(
			&row.ID, &row.Name, &row.AccountNumber, &row.Email, &row.Phone, &row.SourceID, &row.Source,
			&row.CreatedAt, &row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan new customers report row: %w", err)
		}
		row.CreatedAt = row.CreatedAt.UTC()
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read new customers report rows: %w", err)
	}
	return out, nil
}

// Summary counts the whole filtered set (not just one page).
func (r *CustomerRepository) Summary(ctx context.Context, f model.CustomerFilter) (model.CustomerSummary, error) {
	stmt, err := BuildCustomerSummaryQuery(f)
	if err != nil {
		return model.CustomerSummary{}, err
	}

	var s model.CustomerSummary
	if err := r.conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count); err != nil {
		return model.CustomerSummary{}, fmt.Errorf("query new customers summary: %w", err)
	}
	return s, nil
}

// LeadSources lists the options for the Source filter.
func (r *CustomerRepository) LeadSources(ctx context.Context) ([]model.LeadSource, error) {
	rows, err := r.q.ListLeadSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("list lead sources: %w", err)
	}
	out := make([]model.LeadSource, len(rows))
	for i, row := range rows {
		out[i] = model.LeadSource{ID: row.ID, Name: row.Name}
	}
	return out, nil
}
