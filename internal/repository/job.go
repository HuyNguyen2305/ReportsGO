package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// JobRepository reads the new jobs report. It works on a pool or inside a transaction.
type JobRepository struct {
	conn db.DBTX
	q    *db.Queries
}

// NewJobRepository returns a repository using conn, typically the shared pool.
func NewJobRepository(conn db.DBTX) *JobRepository {
	return &JobRepository{conn: conn, q: db.New(conn)}
}

// List returns one page of the report: up to q.Limit+1 rows, so the caller can tell whether more follow.
func (r *JobRepository) List(ctx context.Context, q model.JobQuery) ([]model.JobRow, error) {
	return listJobs(ctx, r.conn, q)
}

// Summary counts and totals the whole filtered set (not just one page).
func (r *JobRepository) Summary(ctx context.Context, f model.JobFilter) (model.JobSummary, error) {
	return summarizeJobs(ctx, r.conn, f)
}

// Page returns one page of the report and, when withSummary is set, the count and total of the whole filtered
// set. The rows and the summary come from one snapshot, so the summary always describes the rows' set even while
// other connections commit. On a pool that is a read-only REPEATABLE READ transaction; a repository already
// bound to a transaction uses the caller's, which is its snapshot.
func (r *JobRepository) Page(ctx context.Context, q model.JobQuery, withSummary bool) (model.JobPage, error) {
	if !withSummary {
		rows, err := listJobs(ctx, r.conn, q)
		if err != nil {
			return model.JobPage{}, err
		}
		return model.JobPage{Rows: rows}, nil
	}

	var page model.JobPage
	read := func(ctx context.Context, conn db.DBTX) error {
		rows, err := listJobs(ctx, conn, q)
		if err != nil {
			return err
		}
		summary, err := summarizeJobs(ctx, conn, q.Filter)
		if err != nil {
			return err
		}
		page = model.JobPage{Rows: rows, Summary: &summary}
		return nil
	}

	if pool, ok := r.conn.(TxOptionsBeginner); ok {
		err := WithReadSnapshot(ctx, pool, func(ctx context.Context, tx pgx.Tx) error { return read(ctx, tx) })
		return page, err
	}
	return page, read(ctx, r.conn)
}

func listJobs(ctx context.Context, conn db.DBTX, q model.JobQuery) ([]model.JobRow, error) {
	stmt, err := BuildJobListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query new jobs report: %w", err)
	}
	defer rows.Close()

	out := make([]model.JobRow, 0, q.Limit+1)
	for rows.Next() {
		var (
			row    model.JobRow
			value  int64
			status string
		)
		if err := rows.Scan(
			&row.ID, &row.CustomerID, &row.CustomerName, &row.AccountNumber, &row.ServiceTypeID, &row.ServiceType,
			&row.SourceID, &row.Source, &value, &row.CreatedAt, &status, &row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan new jobs report row: %w", err)
		}
		row.CreatedAt = row.CreatedAt.UTC()
		row.InvoiceValue = model.Money(value)
		row.Status = model.JobStatus(status)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read new jobs report rows: %w", err)
	}
	return out, nil
}

func summarizeJobs(ctx context.Context, conn db.DBTX, f model.JobFilter) (model.JobSummary, error) {
	stmt, err := BuildJobSummaryQuery(f)
	if err != nil {
		return model.JobSummary{}, err
	}

	var (
		s     model.JobSummary
		total int64
	)
	if err := conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count, &total); err != nil {
		return model.JobSummary{}, fmt.Errorf("query new jobs summary: %w", err)
	}
	s.Total = model.Money(total)
	return s, nil
}

// ServiceTypes lists the options for the Service Type filter.
func (r *JobRepository) ServiceTypes(ctx context.Context) ([]model.ServiceType, error) {
	rows, err := r.q.ListServiceTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list service types: %w", err)
	}
	out := make([]model.ServiceType, len(rows))
	for i, row := range rows {
		out[i] = model.ServiceType{ID: row.ID, Name: row.Name}
	}
	return out, nil
}

// LeadSources lists the options for the Source filter.
func (r *JobRepository) LeadSources(ctx context.Context) ([]model.LeadSource, error) {
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
