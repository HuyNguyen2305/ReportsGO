package repository

import (
	"context"
	"fmt"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// OnlineBookingRepository reads the Online Bookings report. It works on a pool or inside a transaction.
type OnlineBookingRepository struct {
	conn db.DBTX
}

// NewOnlineBookingRepository returns a repository using conn, typically the shared pool.
func NewOnlineBookingRepository(conn db.DBTX) *OnlineBookingRepository {
	return &OnlineBookingRepository{conn: conn}
}

// List returns one page of the report: up to q.Limit+1 rows, so the caller can tell whether more follow.
func (r *OnlineBookingRepository) List(ctx context.Context, q model.OnlineBookingQuery) ([]model.OnlineBookingRow, error) {
	stmt, err := BuildOnlineBookingListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := r.conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query online bookings report: %w", err)
	}
	defer rows.Close()

	out := make([]model.OnlineBookingRow, 0, q.Limit+1)
	for rows.Next() {
		var (
			row    model.OnlineBookingRow
			status string
		)
		if err := rows.Scan(
			&row.ID, &row.CustomerID, &row.CustomerName, &row.ScheduledAt, &row.ServiceTypeID, &row.ServiceType,
			&row.Email, &row.Phone, &row.Address.Line1, &row.Address.Line2, &row.Address.City, &row.Address.State,
			&row.Address.Zip, &row.Address.County, &row.Notes, &status, &row.SourceID, &row.Source, &row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan online bookings report row: %w", err)
		}
		row.ScheduledAt = row.ScheduledAt.UTC()
		row.Status = model.JobStatus(status)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read online bookings report rows: %w", err)
	}
	return out, nil
}

// LeadSources lists the options for the Source filter.
func (r *OnlineBookingRepository) LeadSources(ctx context.Context) ([]model.LeadSource, error) {
	return lookup[model.LeadSource](ctx, r.conn, "lead sources", listLeadSourcesSQL)
}
