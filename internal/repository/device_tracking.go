package repository

import (
	"context"
	"fmt"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// DeviceTrackingRepository reads the Device Tracking report. It works on a pool or inside a transaction.
type DeviceTrackingRepository struct {
	conn db.DBTX
	q    *db.Queries
}

// NewDeviceTrackingRepository returns a repository using conn, typically the shared pool.
func NewDeviceTrackingRepository(conn db.DBTX) *DeviceTrackingRepository {
	return &DeviceTrackingRepository{conn: conn, q: db.New(conn)}
}

// List returns one page of the report: up to q.Limit+1 rows, so the caller can tell whether more follow.
func (r *DeviceTrackingRepository) List(ctx context.Context, q model.DeviceTrackingQuery) ([]model.DeviceTrackingRow, error) {
	stmt, err := BuildDeviceTrackingListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := r.conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query device tracking report: %w", err)
	}
	defer rows.Close()

	out := make([]model.DeviceTrackingRow, 0, q.Limit+1)
	for rows.Next() {
		var row model.DeviceTrackingRow
		if err := rows.Scan(
			&row.ID, &row.CustomerID, &row.CustomerName, &row.ServiceTypeID, &row.ServiceType, &row.Date,
			&row.Address.Line1, &row.Address.Line2, &row.Address.City, &row.Address.State, &row.Address.Zip, &row.Address.County,
			&row.DeviceID, &row.DeviceName, &row.Barcode, &row.Latitude, &row.Longitude, &row.Area, &row.DeviceType,
			&row.CheckedInAt, &row.CheckedOutAt, &row.Status, &row.Activity, &row.Notes, &row.AssignedToID, &row.AssignedTo,
			&row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan device tracking report row: %w", err)
		}
		row.Date = row.Date.UTC()
		if row.CheckedInAt != nil {
			t := row.CheckedInAt.UTC()
			row.CheckedInAt = &t
		}
		if row.CheckedOutAt != nil {
			t := row.CheckedOutAt.UTC()
			row.CheckedOutAt = &t
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read device tracking report rows: %w", err)
	}
	return out, nil
}

// Clients lists the options for the Clients filter.
func (r *DeviceTrackingRepository) Clients(ctx context.Context) ([]model.ClientOption, error) {
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
