package repository

import (
	"context"
	"fmt"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// SystemEmailRepository reads the System Email History report. It works on a pool or inside a transaction.
type SystemEmailRepository struct {
	conn db.DBTX
}

// NewSystemEmailRepository returns a repository using conn, typically the shared pool.
func NewSystemEmailRepository(conn db.DBTX) *SystemEmailRepository {
	return &SystemEmailRepository{conn: conn}
}

// List returns one page of the report: up to q.Limit+1 rows, so the caller can tell whether more follow.
func (r *SystemEmailRepository) List(ctx context.Context, q model.SystemEmailQuery) ([]model.SystemEmailRow, error) {
	stmt, err := BuildSystemEmailListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := r.conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query system email history report: %w", err)
	}
	defer rows.Close()

	out := make([]model.SystemEmailRow, 0, q.Limit+1)
	for rows.Next() {
		var (
			row         model.SystemEmailRow
			emailType   string
			emailStatus string
		)
		if err := rows.Scan(
			&row.ID, &row.CustomerID, &row.CustomerName, &row.Email, &emailType, &emailStatus, &row.SentAt, &row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan system email history report row: %w", err)
		}
		row.SentAt = row.SentAt.UTC()
		row.Type = model.EmailType(emailType)
		row.Status = model.EmailStatus(emailStatus)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read system email history report rows: %w", err)
	}
	return out, nil
}

// Clients lists the options for the Clients filter.
func (r *SystemEmailRepository) Clients(ctx context.Context) ([]model.ClientOption, error) {
	return lookup[model.ClientOption](ctx, r.conn, "clients", listClientsSQL)
}
