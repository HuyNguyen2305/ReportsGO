package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// EstimateRepository reads the estimate report. It works on a pool or inside a transaction.
type EstimateRepository struct {
	conn db.DBTX
}

// NewEstimateRepository returns a repository using conn, typically the shared pool.
func NewEstimateRepository(conn db.DBTX) *EstimateRepository {
	return &EstimateRepository{conn: conn}
}

// List returns one page of the report: up to q.Limit+1 rows, so the caller can tell whether more follow.
func (r *EstimateRepository) List(ctx context.Context, q model.EstimateQuery) ([]model.EstimateRow, error) {
	return listEstimates(ctx, r.conn, q)
}

// Summary totals the whole filtered set (not just one page).
func (r *EstimateRepository) Summary(ctx context.Context, f model.EstimateFilter) (model.EstimateSummary, error) {
	return summarizeEstimates(ctx, r.conn, f)
}

// Page returns one page of the report and, when withSummary is set, the totals of the whole filtered set. The
// rows and the totals come from one snapshot, so the summary always describes the rows' set even while other
// connections commit. On a pool that is a read-only REPEATABLE READ transaction; a repository already bound to
// a transaction uses the caller's, which is its snapshot.
func (r *EstimateRepository) Page(ctx context.Context, q model.EstimateQuery, withSummary bool) (model.EstimatePage, error) {
	if !withSummary {
		rows, err := listEstimates(ctx, r.conn, q)
		if err != nil {
			return model.EstimatePage{}, err
		}
		return model.EstimatePage{Rows: rows}, nil
	}

	var page model.EstimatePage
	read := func(ctx context.Context, conn db.DBTX) error {
		rows, err := listEstimates(ctx, conn, q)
		if err != nil {
			return err
		}
		summary, err := summarizeEstimates(ctx, conn, q.Filter)
		if err != nil {
			return err
		}
		page = model.EstimatePage{Rows: rows, Summary: &summary}
		return nil
	}

	if pool, ok := r.conn.(TxOptionsBeginner); ok {
		err := WithReadSnapshot(ctx, pool, func(ctx context.Context, tx pgx.Tx) error { return read(ctx, tx) })
		return page, err
	}
	return page, read(ctx, r.conn)
}

func listEstimates(ctx context.Context, conn db.DBTX, q model.EstimateQuery) ([]model.EstimateRow, error) {
	stmt, err := BuildEstimateListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query estimate report: %w", err)
	}
	defer rows.Close()

	out := make([]model.EstimateRow, 0, q.Limit+1)
	for rows.Next() {
		var (
			row               model.EstimateRow
			total             int64
			status, signature string
		)
		if err := rows.Scan(
			&row.ID, &row.EstimateNumber, &row.AccountNumber, &row.CustomerID, &row.CustomerName,
			&row.Phone, &row.BillingEmail, &row.CreatedBy, &row.Date,
			&total, &status, &signature, &row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan estimate report row: %w", err)
		}
		row.Total = model.Money(total)
		row.Status = model.EstimateStatus(status)
		row.Signature = model.SignatureStatus(signature)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read estimate report rows: %w", err)
	}
	return out, nil
}

func summarizeEstimates(ctx context.Context, conn db.DBTX, f model.EstimateFilter) (model.EstimateSummary, error) {
	stmt, err := BuildEstimateSummaryQuery(f)
	if err != nil {
		return model.EstimateSummary{}, err
	}

	var (
		s     model.EstimateSummary
		total int64
	)
	if err := conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count, &total); err != nil {
		return model.EstimateSummary{}, fmt.Errorf("query estimate summary: %w", err)
	}
	s.Total = model.Money(total)
	return s, nil
}
