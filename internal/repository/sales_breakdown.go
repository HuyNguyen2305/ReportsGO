package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// SalesBreakdownRepository reads the Revenue and Sales Forecast grids. It works on a pool or inside a transaction.
type SalesBreakdownRepository struct {
	conn db.DBTX
	q    *db.Queries
}

// NewSalesBreakdownRepository returns a repository using conn, typically the shared pool.
func NewSalesBreakdownRepository(conn db.DBTX) *SalesBreakdownRepository {
	return &SalesBreakdownRepository{conn: conn, q: db.New(conn)}
}

// Page returns one page of the grid (up to q.Limit+1 rows, so the caller can tell whether more follow) and, when
// withSummary is set, the summary and the unassigned line. Rows, summary and unassigned line come from one snapshot,
// so they always describe the same data even while other connections commit. On a pool that is a read-only
// REPEATABLE READ transaction; a repository already bound to a transaction uses the caller's, which is its snapshot.
func (r *SalesBreakdownRepository) Page(ctx context.Context, q model.SalesBreakdownQuery, withSummary bool) (model.SalesBreakdownPage, error) {
	if !withSummary {
		rows, err := listSalesBreakdown(ctx, r.conn, q)
		if err != nil {
			return model.SalesBreakdownPage{}, err
		}
		return model.SalesBreakdownPage{Rows: rows}, nil
	}

	var page model.SalesBreakdownPage
	read := func(ctx context.Context, conn db.DBTX) error {
		rows, err := listSalesBreakdown(ctx, conn, q)
		if err != nil {
			return err
		}
		summary, unassigned, err := summarizeSalesBreakdown(ctx, conn, q.Filter)
		if err != nil {
			return err
		}
		page = model.SalesBreakdownPage{Rows: rows, Summary: &summary, Unassigned: unassigned}
		return nil
	}

	if pool, ok := r.conn.(TxOptionsBeginner); ok {
		err := WithReadSnapshot(ctx, pool, func(ctx context.Context, tx pgx.Tx) error { return read(ctx, tx) })
		return page, err
	}
	return page, read(ctx, r.conn)
}

func listSalesBreakdown(ctx context.Context, conn db.DBTX, q model.SalesBreakdownQuery) ([]model.SalesBreakdownRow, error) {
	stmt, err := BuildSalesBreakdownListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query sales breakdown report: %w", err)
	}
	defer rows.Close()

	out := make([]model.SalesBreakdownRow, 0, q.Limit+1)
	for rows.Next() {
		var (
			row    model.SalesBreakdownRow
			months []int64
			total  int64
		)
		if err := rows.Scan(&row.ID, &row.Name, &row.AccountNumber, &months, &total, &row.SortValue); err != nil {
			return nil, fmt.Errorf("scan sales breakdown report row: %w", err)
		}
		row.SalesBreakdownAmounts, err = toAmounts(months, total)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read sales breakdown report rows: %w", err)
	}
	return out, nil
}

// summarizeSalesBreakdown reads the summary and the unassigned line, which is nil when it is zero or when a group or
// search filter is set.
func summarizeSalesBreakdown(ctx context.Context, conn db.DBTX, f model.SalesBreakdownFilter) (model.SalesBreakdownSummary, *model.SalesBreakdownAmounts, error) {
	stmt, err := BuildSalesBreakdownSummaryQuery(f)
	if err != nil {
		return model.SalesBreakdownSummary{}, nil, err
	}

	var (
		s                        model.SalesBreakdownSummary
		months, unassignedMonths []int64
		total                    int64
		unassignedTotal          *int64
	)
	if err := conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count, &months, &total, &unassignedMonths, &unassignedTotal); err != nil {
		return model.SalesBreakdownSummary{}, nil, fmt.Errorf("query sales breakdown summary: %w", err)
	}
	if s.SalesBreakdownAmounts, err = toAmounts(months, total); err != nil {
		return model.SalesBreakdownSummary{}, nil, err
	}
	if unassignedTotal == nil {
		return s, nil, nil
	}
	unassigned, err := toAmounts(unassignedMonths, *unassignedTotal)
	if err != nil {
		return model.SalesBreakdownSummary{}, nil, err
	}
	if unassigned == (model.SalesBreakdownAmounts{}) {
		return s, nil, nil
	}
	return s, &unassigned, nil
}

func toAmounts(months []int64, total int64) (model.SalesBreakdownAmounts, error) {
	if len(months) != 12 {
		return model.SalesBreakdownAmounts{}, fmt.Errorf("sales breakdown: expected 12 months, got %d", len(months))
	}
	var a model.SalesBreakdownAmounts
	for i, m := range months {
		a.Months[i] = model.Money(m)
	}
	a.Total = model.Money(total)
	return a, nil
}

// GroupOptions lists the choices of the group filter for each grouping but clients, which are found by search.
func (r *SalesBreakdownRepository) GroupOptions(ctx context.Context) (model.SalesBreakdownGroupOptions, error) {
	var out model.SalesBreakdownGroupOptions

	types, err := r.q.ListServiceTypes(ctx)
	if err != nil {
		return out, fmt.Errorf("list service types: %w", err)
	}
	for _, t := range types {
		out.ServiceTypes = append(out.ServiceTypes, model.ServiceType{ID: t.ID, Name: t.Name})
	}

	items, err := r.q.ListItems(ctx)
	if err != nil {
		return out, fmt.Errorf("list items: %w", err)
	}
	for _, it := range items {
		out.Items = append(out.Items, model.ItemOption{ID: it.ID, Name: it.Name})
	}

	staff, err := r.q.ListStaff(ctx)
	if err != nil {
		return out, fmt.Errorf("list staff: %w", err)
	}
	for _, s := range staff {
		out.Staff = append(out.Staff, model.StaffOption{ID: s.ID, Name: s.FullName})
	}

	sources, err := r.q.ListLeadSources(ctx)
	if err != nil {
		return out, fmt.Errorf("list lead sources: %w", err)
	}
	for _, s := range sources {
		out.Sources = append(out.Sources, model.LeadSource{ID: s.ID, Name: s.Name})
	}

	tags, err := r.q.ListJobTags(ctx)
	if err != nil {
		return out, fmt.Errorf("list job tags: %w", err)
	}
	for _, t := range tags {
		out.Tags = append(out.Tags, model.JobTag{ID: t.ID, Name: t.Name})
	}
	return out, nil
}
