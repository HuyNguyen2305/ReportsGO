package repository

import (
	"context"
	"fmt"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// MaterialUseRepository reads the Material Use report. It works on a pool or inside a transaction.
type MaterialUseRepository struct {
	conn db.DBTX
	q    *db.Queries
}

// NewMaterialUseRepository returns a repository using conn, typically the shared pool.
func NewMaterialUseRepository(conn db.DBTX) *MaterialUseRepository {
	return &MaterialUseRepository{conn: conn, q: db.New(conn)}
}

// List returns one page of the report: up to q.Limit+1 rows, so the caller can tell whether more follow.
func (r *MaterialUseRepository) List(ctx context.Context, q model.MaterialUseQuery) ([]model.MaterialUseRow, error) {
	stmt, err := BuildMaterialUseListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := r.conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query material use report: %w", err)
	}
	defer rows.Close()

	out := make([]model.MaterialUseRow, 0, q.Limit+1)
	for rows.Next() {
		var row model.MaterialUseRow
		if err := rows.Scan(&row.MaterialID, &row.Material, &row.EPA, &row.Qty, &row.Applications, &row.SortValue); err != nil {
			return nil, fmt.Errorf("scan material use report row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read material use report rows: %w", err)
	}
	return out, nil
}

// Materials lists the options for the Materials filter.
func (r *MaterialUseRepository) Materials(ctx context.Context) ([]model.MaterialOption, error) {
	rows, err := r.q.ListMaterials(ctx)
	if err != nil {
		return nil, fmt.Errorf("list materials: %w", err)
	}
	out := make([]model.MaterialOption, len(rows))
	for i, row := range rows {
		out[i] = model.MaterialOption{ID: row.ID, Name: row.Name}
	}
	return out, nil
}

// Clients lists the options for the Clients filter.
func (r *MaterialUseRepository) Clients(ctx context.Context) ([]model.ClientOption, error) {
	rows, err := r.q.ListClients(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clients: %w", err)
	}
	out := make([]model.ClientOption, len(rows))
	for i, row := range rows {
		out[i] = model.ClientOption{ID: row.ID, Name: row.DisplayName}
	}
	return out, nil
}
