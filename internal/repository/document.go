package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// DocumentRepository reads the document report. It works on a pool or inside a transaction.
type DocumentRepository struct {
	conn db.DBTX
	q    *db.Queries
}

// NewDocumentRepository returns a repository using conn, typically the shared pool.
func NewDocumentRepository(conn db.DBTX) *DocumentRepository {
	return &DocumentRepository{conn: conn, q: db.New(conn)}
}

// List returns one page of the report: up to q.Limit+1 rows, so the caller can tell whether more follow.
func (r *DocumentRepository) List(ctx context.Context, q model.DocumentQuery) ([]model.DocumentRow, error) {
	return listDocuments(ctx, r.conn, q)
}

// Summary counts the whole filtered set (not just one page).
func (r *DocumentRepository) Summary(ctx context.Context, f model.DocumentFilter) (model.DocumentSummary, error) {
	return summarizeDocuments(ctx, r.conn, f)
}

// Page returns one page of the report and, when withSummary is set, the count of the whole filtered set. The
// rows and the count come from one snapshot, so the summary always describes the rows' set even while other
// connections commit. On a pool that is a read-only REPEATABLE READ transaction; a repository already bound to
// a transaction uses the caller's, which is its snapshot.
func (r *DocumentRepository) Page(ctx context.Context, q model.DocumentQuery, withSummary bool) (model.DocumentPage, error) {
	if !withSummary {
		rows, err := listDocuments(ctx, r.conn, q)
		if err != nil {
			return model.DocumentPage{}, err
		}
		return model.DocumentPage{Rows: rows}, nil
	}

	var page model.DocumentPage
	read := func(ctx context.Context, conn db.DBTX) error {
		rows, err := listDocuments(ctx, conn, q)
		if err != nil {
			return err
		}
		summary, err := summarizeDocuments(ctx, conn, q.Filter)
		if err != nil {
			return err
		}
		page = model.DocumentPage{Rows: rows, Summary: &summary}
		return nil
	}

	if pool, ok := r.conn.(TxOptionsBeginner); ok {
		err := WithReadSnapshot(ctx, pool, func(ctx context.Context, tx pgx.Tx) error { return read(ctx, tx) })
		return page, err
	}
	return page, read(ctx, r.conn)
}

func listDocuments(ctx context.Context, conn db.DBTX, q model.DocumentQuery) ([]model.DocumentRow, error) {
	stmt, err := BuildDocumentListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query document report: %w", err)
	}
	defer rows.Close()

	out := make([]model.DocumentRow, 0, q.Limit+1)
	for rows.Next() {
		var (
			row    model.DocumentRow
			status string
		)
		if err := rows.Scan(
			&row.ID, &row.DocumentNumber, &row.TypeID, &row.TypeName, &row.CustomerID, &row.CustomerName,
			&row.AccountNumber, &row.CreatedBy, &row.CreatedAt, &status, &row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan document report row: %w", err)
		}
		row.CreatedAt = row.CreatedAt.UTC()
		row.Status = model.DocumentStatus(status)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read document report rows: %w", err)
	}
	return out, nil
}

func summarizeDocuments(ctx context.Context, conn db.DBTX, f model.DocumentFilter) (model.DocumentSummary, error) {
	stmt, err := BuildDocumentSummaryQuery(f)
	if err != nil {
		return model.DocumentSummary{}, err
	}

	var s model.DocumentSummary
	if err := conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count); err != nil {
		return model.DocumentSummary{}, fmt.Errorf("query document summary: %w", err)
	}
	return s, nil
}

// DocumentTypes lists the options for the Type filter.
func (r *DocumentRepository) DocumentTypes(ctx context.Context) ([]model.DocumentType, error) {
	rows, err := r.q.ListDocumentTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list document types: %w", err)
	}
	out := make([]model.DocumentType, len(rows))
	for i, row := range rows {
		out[i] = model.DocumentType{ID: row.ID, Name: row.Name}
	}
	return out, nil
}
