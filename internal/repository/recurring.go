package repository

import (
	"context"
	"fmt"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// RecurringRepository reads the recurring invoice report. It works on a pool or inside a transaction.
type RecurringRepository struct {
	conn db.DBTX
}

// NewRecurringRepository returns a repository using conn, typically the shared pool.
func NewRecurringRepository(conn db.DBTX) *RecurringRepository {
	return &RecurringRepository{conn: conn}
}

// List returns one page of the report: up to q.Limit+1 rows, so the caller can tell whether more follow.
func (r *RecurringRepository) List(ctx context.Context, q model.RecurringQuery) ([]model.RecurringRow, error) {
	stmt, err := BuildRecurringListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := r.conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query recurring invoice report: %w", err)
	}
	defer rows.Close()

	out := make([]model.RecurringRow, 0, q.Limit+1)
	for rows.Next() {
		var (
			row                  model.RecurringRow
			unit, action, status string
			interval             int32
			weekday, occurrences *int32
			totalValue           int64
		)
		if err := rows.Scan(
			&row.ID, &row.CustomerID, &row.CustomerName, &row.AccountNumber,
			&unit, &interval, &weekday, &occurrences,
			&action, &status, &totalValue, &row.StartedOn, &row.EndsOn,
			&row.NextInvoiceID, &row.NextInvoiceNumber, &row.NextInvoiceDate, &row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan recurring invoice report row: %w", err)
		}
		row.Unit = model.FrequencyUnit(unit)
		row.Interval = int(interval)
		row.Weekday = intPtr(weekday)
		row.Occurrences = intPtr(occurrences)
		row.Action = model.RecurringAction(action)
		row.Status = model.RecurringStatus(status)
		row.TotalValue = model.Money(totalValue)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read recurring invoice report rows: %w", err)
	}
	return out, nil
}

// Summary counts and totals the whole filtered set (not just one page).
func (r *RecurringRepository) Summary(ctx context.Context, f model.RecurringFilter) (model.RecurringSummary, error) {
	stmt, err := BuildRecurringSummaryQuery(f)
	if err != nil {
		return model.RecurringSummary{}, err
	}

	var (
		s     model.RecurringSummary
		total int64
	)
	if err := r.conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count, &total); err != nil {
		return model.RecurringSummary{}, fmt.Errorf("query recurring invoice summary: %w", err)
	}
	s.Total = model.Money(total)
	return s, nil
}

// intPtr converts a nullable database integer to the *int the model uses.
func intPtr(v *int32) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}
