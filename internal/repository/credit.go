package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// CreditRepository reads the credit report. It works on a pool or inside a transaction.
type CreditRepository struct {
	conn db.DBTX
}

// NewCreditRepository returns a repository using conn, typically the shared pool.
func NewCreditRepository(conn db.DBTX) *CreditRepository {
	return &CreditRepository{conn: conn}
}

// List returns one page of the report: up to q.Limit+1 rows, so the caller can tell whether more follow.
func (r *CreditRepository) List(ctx context.Context, q model.CreditQuery) ([]model.CreditRow, error) {
	return listCredits(ctx, r.conn, q)
}

// Summary counts and totals the whole filtered set (not just one page).
func (r *CreditRepository) Summary(ctx context.Context, f model.CreditFilter) (model.CreditSummary, error) {
	return summarizeCredits(ctx, r.conn, f)
}

// Page returns one page of the report and, when withSummary is set, the count and total of the whole filtered
// set. The rows and the summary come from one snapshot, so the summary always describes the rows' set even while
// other connections commit. On a pool that is a read-only REPEATABLE READ transaction; a repository already
// bound to a transaction uses the caller's, which is its snapshot.
func (r *CreditRepository) Page(ctx context.Context, q model.CreditQuery, withSummary bool) (model.CreditPage, error) {
	if !withSummary {
		rows, err := listCredits(ctx, r.conn, q)
		if err != nil {
			return model.CreditPage{}, err
		}
		return model.CreditPage{Rows: rows}, nil
	}

	var page model.CreditPage
	read := func(ctx context.Context, conn db.DBTX) error {
		rows, err := listCredits(ctx, conn, q)
		if err != nil {
			return err
		}
		summary, err := summarizeCredits(ctx, conn, q.Filter)
		if err != nil {
			return err
		}
		page = model.CreditPage{Rows: rows, Summary: &summary}
		return nil
	}

	if pool, ok := r.conn.(TxOptionsBeginner); ok {
		err := WithReadSnapshot(ctx, pool, func(ctx context.Context, tx pgx.Tx) error { return read(ctx, tx) })
		return page, err
	}
	return page, read(ctx, r.conn)
}

func listCredits(ctx context.Context, conn db.DBTX, q model.CreditQuery) ([]model.CreditRow, error) {
	stmt, err := BuildCreditListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query credit report: %w", err)
	}
	defer rows.Close()

	out := make([]model.CreditRow, 0, q.Limit+1)
	for rows.Next() {
		var (
			row    model.CreditRow
			method string
			amount int64
		)
		if err := rows.Scan(
			&row.ID, &row.CustomerID, &row.CustomerName, &row.AccountNumber, &method, &row.MethodDetail,
			&amount, &row.Date, &row.CreatedBy, &row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan credit report row: %w", err)
		}
		row.Method = model.PaymentMethod(method)
		row.Amount = model.Money(amount)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read credit report rows: %w", err)
	}
	return out, nil
}

func summarizeCredits(ctx context.Context, conn db.DBTX, f model.CreditFilter) (model.CreditSummary, error) {
	stmt, err := BuildCreditSummaryQuery(f)
	if err != nil {
		return model.CreditSummary{}, err
	}

	var (
		s     model.CreditSummary
		total int64
	)
	if err := conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count, &total); err != nil {
		return model.CreditSummary{}, fmt.Errorf("query credit summary: %w", err)
	}
	s.Total = model.Money(total)
	return s, nil
}
