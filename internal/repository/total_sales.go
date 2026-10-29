package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// TotalSalesRepository reads the Total Sales report. It works on a pool or inside a transaction.
type TotalSalesRepository struct {
	conn db.DBTX
}

// NewTotalSalesRepository returns a repository using conn, typically the shared pool.
func NewTotalSalesRepository(conn db.DBTX) *TotalSalesRepository {
	return &TotalSalesRepository{conn: conn}
}

// Page returns one page of the report (up to q.Limit+1 rows, so the caller can tell whether more follow) and,
// when withSummary is set, the totals of the whole filtered set. The rows and the totals come from one snapshot,
// so the summary always describes the rows' set even while other connections commit. On a pool that is a
// read-only REPEATABLE READ transaction; a repository already bound to a transaction uses the caller's, which
// is its snapshot.
func (r *TotalSalesRepository) Page(ctx context.Context, q model.TotalSaleQuery, withSummary bool) (model.TotalSalePage, error) {
	if !withSummary {
		rows, err := listTotalSales(ctx, r.conn, q)
		if err != nil {
			return model.TotalSalePage{}, err
		}
		return model.TotalSalePage{Rows: rows}, nil
	}

	var page model.TotalSalePage
	read := func(ctx context.Context, conn db.DBTX) error {
		rows, err := listTotalSales(ctx, conn, q)
		if err != nil {
			return err
		}
		summary, err := summarizeTotalSales(ctx, conn, q.Filter)
		if err != nil {
			return err
		}
		page = model.TotalSalePage{Rows: rows, Summary: &summary}
		return nil
	}

	if pool, ok := r.conn.(TxOptionsBeginner); ok {
		err := WithReadSnapshot(ctx, pool, func(ctx context.Context, tx pgx.Tx) error { return read(ctx, tx) })
		return page, err
	}
	return page, read(ctx, r.conn)
}

func listTotalSales(ctx context.Context, conn db.DBTX, q model.TotalSaleQuery) ([]model.TotalSaleRow, error) {
	stmt, err := BuildTotalSalesListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query total sales report: %w", err)
	}
	defer rows.Close()

	out := make([]model.TotalSaleRow, 0, q.Limit+1)
	for rows.Next() {
		var (
			row                model.TotalSaleRow
			status             string
			amount, withoutTax int64
		)
		if err := rows.Scan(
			&row.ID, &row.InvoiceNumber, &row.CustomerID, &row.CustomerName, &row.Date,
			&status, &amount, &withoutTax, &row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan total sales report row: %w", err)
		}
		row.Status = model.TotalSaleStatus(status)
		row.Amount = model.Money(amount)
		row.WithoutTax = model.Money(withoutTax)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read total sales report rows: %w", err)
	}
	return out, nil
}

func summarizeTotalSales(ctx context.Context, conn db.DBTX, f model.TotalSaleFilter) (model.TotalSaleSummary, error) {
	stmt, err := BuildTotalSalesSummaryQuery(f)
	if err != nil {
		return model.TotalSaleSummary{}, err
	}

	var (
		s                  model.TotalSaleSummary
		amount, withoutTax int64
	)
	if err := conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count, &amount, &withoutTax); err != nil {
		return model.TotalSaleSummary{}, fmt.Errorf("query total sales summary: %w", err)
	}
	s.Amount = model.Money(amount)
	s.WithoutTax = model.Money(withoutTax)
	return s, nil
}
