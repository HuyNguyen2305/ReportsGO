package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

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
	return listRecurring(ctx, r.conn, q)
}

// Summary counts and totals the whole filtered set (not just one page).
func (r *RecurringRepository) Summary(ctx context.Context, f model.RecurringFilter) (model.RecurringSummary, error) {
	return summarizeRecurring(ctx, r.conn, f)
}

// Page returns one page of the report and, when withSummary is set, the count and total of the whole filtered
// set. The rows and the summary come from one snapshot, so the summary always describes the rows' set even while
// other connections commit. On a pool that is a read-only REPEATABLE READ transaction; a repository already
// bound to a transaction uses the caller's, which is its snapshot.
func (r *RecurringRepository) Page(ctx context.Context, q model.RecurringQuery, withSummary bool) (model.RecurringPage, error) {
	if !withSummary {
		rows, err := listRecurring(ctx, r.conn, q)
		if err != nil {
			return model.RecurringPage{}, err
		}
		return model.RecurringPage{Rows: rows}, nil
	}

	var page model.RecurringPage
	read := func(ctx context.Context, conn db.DBTX) error {
		rows, err := listRecurring(ctx, conn, q)
		if err != nil {
			return err
		}
		summary, err := summarizeRecurring(ctx, conn, q.Filter)
		if err != nil {
			return err
		}
		page = model.RecurringPage{Rows: rows, Summary: &summary}
		return nil
	}

	if pool, ok := r.conn.(TxOptionsBeginner); ok {
		err := WithReadSnapshot(ctx, pool, func(ctx context.Context, tx pgx.Tx) error { return read(ctx, tx) })
		return page, err
	}
	return page, read(ctx, r.conn)
}

func listRecurring(ctx context.Context, conn db.DBTX, q model.RecurringQuery) ([]model.RecurringRow, error) {
	stmt, err := BuildRecurringListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(ctx, stmt.SQL, stmt.Args...)
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

func summarizeRecurring(ctx context.Context, conn db.DBTX, f model.RecurringFilter) (model.RecurringSummary, error) {
	stmt, err := BuildRecurringSummaryQuery(f)
	if err != nil {
		return model.RecurringSummary{}, err
	}

	var (
		s     model.RecurringSummary
		total int64
	)
	if err := conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count, &total); err != nil {
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
