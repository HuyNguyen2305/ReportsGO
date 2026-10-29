package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// ServiceLookupRepository reads the Service Lookup report. It works on a pool or inside a transaction.
type ServiceLookupRepository struct {
	conn db.DBTX
	q    *db.Queries
}

// NewServiceLookupRepository returns a repository using conn, typically the shared pool.
func NewServiceLookupRepository(conn db.DBTX) *ServiceLookupRepository {
	return &ServiceLookupRepository{conn: conn, q: db.New(conn)}
}

// Page returns one page of the report (up to q.Limit+1 rows, so the caller can tell whether more follow) and,
// when withSummary is set, the totals of the whole filtered set. The rows and the totals come from one snapshot,
// so the summary always describes the rows' set even while other connections commit. On a pool that is a
// read-only REPEATABLE READ transaction; a repository already bound to a transaction uses the caller's, which
// is its snapshot.
func (r *ServiceLookupRepository) Page(ctx context.Context, q model.ServiceLookupQuery, withSummary bool) (model.ServiceLookupPage, error) {
	if !withSummary {
		rows, err := listServiceLookup(ctx, r.conn, q)
		if err != nil {
			return model.ServiceLookupPage{}, err
		}
		return model.ServiceLookupPage{Rows: rows}, nil
	}

	var page model.ServiceLookupPage
	read := func(ctx context.Context, conn db.DBTX) error {
		rows, err := listServiceLookup(ctx, conn, q)
		if err != nil {
			return err
		}
		summary, err := summarizeServiceLookup(ctx, conn, q.Filter)
		if err != nil {
			return err
		}
		page = model.ServiceLookupPage{Rows: rows, Summary: &summary}
		return nil
	}

	if pool, ok := r.conn.(TxOptionsBeginner); ok {
		err := WithReadSnapshot(ctx, pool, func(ctx context.Context, tx pgx.Tx) error { return read(ctx, tx) })
		return page, err
	}
	return page, read(ctx, r.conn)
}

func listServiceLookup(ctx context.Context, conn db.DBTX, q model.ServiceLookupQuery) ([]model.ServiceLookupRow, error) {
	stmt, err := BuildServiceLookupListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query service lookup report: %w", err)
	}
	defer rows.Close()

	out := make([]model.ServiceLookupRow, 0, q.Limit+1)
	for rows.Next() {
		var (
			row               model.ServiceLookupRow
			scheduled         *time.Time
			value, withoutTax int64
			status            string
			tagIDs            []int64
			tagNames          []string
		)
		if err := rows.Scan(
			&row.ID, &row.CustomerID, &row.CustomerName, &row.Phone, &row.ServiceTypeID, &row.ServiceType, &scheduled,
			&row.Address.Line1, &row.Address.Line2, &row.Address.City, &row.Address.State, &row.Address.Zip, &row.Address.County,
			&value, &withoutTax, &status, &row.AssignedToID, &row.AssignedTo, &tagIDs, &tagNames, &row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan service lookup report row: %w", err)
		}
		row.ScheduledAt = scheduled
		row.InvoiceValue = model.Money(value)
		row.WithoutTax = model.Money(withoutTax)
		row.Status = model.JobStatus(status)
		row.Tags = make([]model.JobTag, len(tagIDs))
		for i := range tagIDs {
			row.Tags[i] = model.JobTag{ID: tagIDs[i], Name: tagNames[i]}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read service lookup report rows: %w", err)
	}
	return out, nil
}

func summarizeServiceLookup(ctx context.Context, conn db.DBTX, f model.ServiceLookupFilter) (model.ServiceLookupSummary, error) {
	stmt, err := BuildServiceLookupSummaryQuery(f)
	if err != nil {
		return model.ServiceLookupSummary{}, err
	}

	var (
		s                 model.ServiceLookupSummary
		value, withoutTax int64
	)
	if err := conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(
		&s.Count, &value, &withoutTax, &s.Tabs.Active, &s.Tabs.WorkPool, &s.Tabs.Deleted,
	); err != nil {
		return model.ServiceLookupSummary{}, fmt.Errorf("query service lookup summary: %w", err)
	}
	s.InvoiceValue = model.Money(value)
	s.WithoutTax = model.Money(withoutTax)
	return s, nil
}

// ServiceTypes lists the options for the Service Type filter.
func (r *ServiceLookupRepository) ServiceTypes(ctx context.Context) ([]model.ServiceType, error) {
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

// Staff lists the options for the Assigned To filter.
func (r *ServiceLookupRepository) Staff(ctx context.Context) ([]model.StaffOption, error) {
	rows, err := r.q.ListStaff(ctx)
	if err != nil {
		return nil, fmt.Errorf("list staff: %w", err)
	}
	out := make([]model.StaffOption, len(rows))
	for i, row := range rows {
		out[i] = model.StaffOption{ID: row.ID, Name: row.FullName}
	}
	return out, nil
}

// Tags lists the options for the Tags filter.
func (r *ServiceLookupRepository) Tags(ctx context.Context) ([]model.JobTag, error) {
	rows, err := r.q.ListJobTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("list job tags: %w", err)
	}
	out := make([]model.JobTag, len(rows))
	for i, row := range rows {
		out[i] = model.JobTag{ID: row.ID, Name: row.Name}
	}
	return out, nil
}
