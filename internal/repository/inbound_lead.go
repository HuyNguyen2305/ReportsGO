package repository

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// InboundLeadRepository reads the Inbound Leads report. It works on a pool or inside a transaction.
type InboundLeadRepository struct {
	conn db.DBTX
}

// NewInboundLeadRepository returns a repository using conn, typically the shared pool.
func NewInboundLeadRepository(conn db.DBTX) *InboundLeadRepository {
	return &InboundLeadRepository{conn: conn}
}

// List returns one page of the report: up to q.Limit+1 rows, so the caller can tell whether more follow.
func (r *InboundLeadRepository) List(ctx context.Context, q model.InboundLeadQuery) ([]model.InboundLeadRow, error) {
	stmt, err := BuildInboundLeadListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := r.conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query inbound leads report: %w", err)
	}
	defer rows.Close()

	out := make([]model.InboundLeadRow, 0, q.Limit+1)
	for rows.Next() {
		var (
			row           model.InboundLeadRow
			serviceTypeID *int64
			serviceType   *string
			scheduledAt   *time.Time
		)
		if err := rows.Scan(
			&row.ID, &row.CustomerName, &row.SourceID, &row.Source, &row.Email, &row.Phone, &row.Notes, &row.DateAdded,
			&serviceTypeID, &serviceType, &scheduledAt, &row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan inbound leads report row: %w", err)
		}
		row.DateAdded = row.DateAdded.UTC()
		if serviceTypeID != nil && serviceType != nil && scheduledAt != nil {
			row.ActiveJob = &model.InboundLeadJob{
				ServiceTypeID: *serviceTypeID, ServiceType: *serviceType, ScheduledAt: scheduledAt.UTC(),
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read inbound leads report rows: %w", err)
	}
	return out, nil
}

// LeadSources lists the options for the Source filter.
func (r *InboundLeadRepository) LeadSources(ctx context.Context) ([]model.LeadSource, error) {
	return lookup[model.LeadSource](ctx, r.conn, "lead sources", listLeadSourcesSQL)
}
