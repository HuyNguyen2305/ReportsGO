package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

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
	return listCustomers(ctx, r.conn, q)
}

// Summary counts the whole filtered set (not just one page).
func (r *CustomerRepository) Summary(ctx context.Context, f model.CustomerFilter) (model.CustomerSummary, error) {
	return summarizeCustomers(ctx, r.conn, f)
}

// Page returns one page of the report and, when withSummary is set, the count of the whole filtered set. The
// rows and the count come from one snapshot, so the summary always describes the rows' set even while other
// connections commit. On a pool that is a read-only REPEATABLE READ transaction; a repository already bound to
// a transaction uses the caller's, which is its snapshot.
func (r *CustomerRepository) Page(ctx context.Context, q model.CustomerQuery, withSummary bool) (model.CustomerPage, error) {
	if !withSummary {
		rows, err := listCustomers(ctx, r.conn, q)
		if err != nil {
			return model.CustomerPage{}, err
		}
		return model.CustomerPage{Rows: rows}, nil
	}

	var page model.CustomerPage
	read := func(ctx context.Context, conn db.DBTX) error {
		rows, err := listCustomers(ctx, conn, q)
		if err != nil {
			return err
		}
		summary, err := summarizeCustomers(ctx, conn, q.Filter)
		if err != nil {
			return err
		}
		page = model.CustomerPage{Rows: rows, Summary: &summary}
		return nil
	}

	if pool, ok := r.conn.(TxOptionsBeginner); ok {
		err := WithReadSnapshot(ctx, pool, func(ctx context.Context, tx pgx.Tx) error { return read(ctx, tx) })
		return page, err
	}
	return page, read(ctx, r.conn)
}

func listCustomers(ctx context.Context, conn db.DBTX, q model.CustomerQuery) ([]model.CustomerRow, error) {
	stmt, err := BuildCustomerListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(ctx, stmt.SQL, stmt.Args...)
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

func summarizeCustomers(ctx context.Context, conn db.DBTX, f model.CustomerFilter) (model.CustomerSummary, error) {
	stmt, err := BuildCustomerSummaryQuery(f)
	if err != nil {
		return model.CustomerSummary{}, err
	}

	var s model.CustomerSummary
	if err := conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count); err != nil {
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
