package repository

import (
	"context"
	"fmt"

	"reportsgo/internal/model"
	"reportsgo/internal/repository/db"
)

// InvoiceRepository reads the invoice report. It works on a pool or inside a transaction.
type InvoiceRepository struct {
	conn db.DBTX
	q    *db.Queries
}

// NewInvoiceRepository returns a repository using conn, typically the shared pool.
func NewInvoiceRepository(conn db.DBTX) *InvoiceRepository {
	return &InvoiceRepository{conn: conn, q: db.New(conn)}
}

// List returns one page of the report: up to q.Limit+1 rows, so the caller can tell whether more follow.
func (r *InvoiceRepository) List(ctx context.Context, q model.InvoiceQuery) ([]model.InvoiceRow, error) {
	stmt, err := BuildInvoiceListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := r.conn.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, fmt.Errorf("query invoice report: %w", err)
	}
	defer rows.Close()

	out := make([]model.InvoiceRow, 0, q.Limit+1)
	for rows.Next() {
		var (
			row               model.InvoiceRow
			total, discount   int64
			status, signature string
		)
		if err := rows.Scan(
			&row.ID, &row.InvoiceNumber, &row.AccountNumber, &row.CustomerID, &row.CustomerName,
			&row.Job, &row.FrequencyLabel, &row.BillingEmail, &row.Date,
			&total, &discount, &row.Tags,
			&row.PaymentTermsName, &row.PaymentTermsDescription, &status, &signature, &row.SortValue,
		); err != nil {
			return nil, fmt.Errorf("scan invoice report row: %w", err)
		}
		row.Total = model.Money(total)
		row.Discount = model.Money(discount)
		row.Status = model.InvoiceStatus(status)
		row.SignatureStatus = model.SignatureStatus(signature)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read invoice report rows: %w", err)
	}
	return out, nil
}

// Summary totals the whole filtered set (not just one page).
func (r *InvoiceRepository) Summary(ctx context.Context, f model.InvoiceFilter) (model.InvoiceSummary, error) {
	stmt, err := BuildInvoiceSummaryQuery(f)
	if err != nil {
		return model.InvoiceSummary{}, err
	}

	var (
		s                               model.InvoiceSummary
		total, discount, outstandingSum int64
	)
	if err := r.conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count, &total, &discount, &outstandingSum); err != nil {
		return model.InvoiceSummary{}, fmt.Errorf("query invoice summary: %w", err)
	}
	s.Total = model.Money(total)
	s.Discount = model.Money(discount)
	s.Outstanding = model.Money(outstandingSum)
	return s, nil
}

// ServiceTypes lists the options for the Service filter.
func (r *InvoiceRepository) ServiceTypes(ctx context.Context) ([]model.ServiceType, error) {
	rows, err := r.q.ListServiceTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list service types: %w", err)
	}
	out := make([]model.ServiceType, len(rows))
	for i, row := range rows {
		out[i] = model.ServiceType{ID: row.ID, Name: row.Name}
	}
	return out, nil
}

// Tags lists the options for the Invoice Tags filter.
func (r *InvoiceRepository) Tags(ctx context.Context) ([]model.InvoiceTag, error) {
	rows, err := r.q.ListInvoiceTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("list invoice tags: %w", err)
	}
	out := make([]model.InvoiceTag, len(rows))
	for i, row := range rows {
		out[i] = model.InvoiceTag{ID: row.ID, Name: row.Name}
	}
	return out, nil
}

// PaymentTerms lists the options for the Payment Terms filter.
func (r *InvoiceRepository) PaymentTerms(ctx context.Context) ([]model.PaymentTerm, error) {
	rows, err := r.q.ListPaymentTerms(ctx)
	if err != nil {
		return nil, fmt.Errorf("list payment terms: %w", err)
	}
	out := make([]model.PaymentTerm, len(rows))
	for i, row := range rows {
		out[i] = model.PaymentTerm{ID: row.ID, Name: row.Name, Description: row.Description}
	}
	return out, nil
}
