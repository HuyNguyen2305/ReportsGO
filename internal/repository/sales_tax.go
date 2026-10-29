package repository

import (
	"context"
	"fmt"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// SalesTaxRepository reads the Sales Tax Summary. It works on a pool or inside a transaction.
type SalesTaxRepository struct {
	conn db.DBTX
	q    *db.Queries
}

// NewSalesTaxRepository returns a repository using conn, typically the shared pool.
func NewSalesTaxRepository(conn db.DBTX) *SalesTaxRepository {
	return &SalesTaxRepository{conn: conn, q: db.New(conn)}
}

// Summary returns one row per tax rate with its totals for the filter's basis.
func (r *SalesTaxRepository) Summary(ctx context.Context, f model.SalesTaxFilter, s model.SalesTaxSort) ([]model.SalesTaxRow, error) {
	stmt, err := BuildSalesTaxQuery(f, s)
	if err != nil {
		return nil, err
	}

	rows, err := r.conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query sales tax summary: %w", err)
	}
	defer rows.Close()

	var out []model.SalesTaxRow
	for rows.Next() {
		var (
			row                     model.SalesTaxRow
			taxable, tax, surcharge int64
		)
		if err := rows.Scan(&row.ID, &row.Name, &row.Rate, &taxable, &tax, &surcharge); err != nil {
			return nil, fmt.Errorf("scan sales tax summary row: %w", err)
		}
		row.TaxableSales = model.Money(taxable)
		row.TaxCollected = model.Money(tax)
		row.SurchargeTax = model.Money(surcharge)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read sales tax summary rows: %w", err)
	}
	return out, nil
}

// Rates lists every tax rate, for the Tax Rate tab.
func (r *SalesTaxRepository) Rates(ctx context.Context) ([]model.TaxRateOption, error) {
	rows, err := r.q.ListTaxRates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tax rates: %w", err)
	}
	out := make([]model.TaxRateOption, len(rows))
	for i, row := range rows {
		out[i] = model.TaxRateOption{ID: row.ID, Name: row.Name, Rate: row.Rate, Active: row.Active}
	}
	return out, nil
}
