package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

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
	return listInvoices(ctx, r.conn, q)
}

// Summary totals the whole filtered set (not just one page).
func (r *InvoiceRepository) Summary(ctx context.Context, f model.InvoiceFilter) (model.InvoiceSummary, error) {
	return summarizeInvoices(ctx, r.conn, f)
}

// Page returns one page of the report and, when withSummary is set, the totals of the whole filtered set. The
// rows and the totals come from one snapshot, so the summary always describes the rows' set even while other
// connections commit. On a pool that is a read-only REPEATABLE READ transaction; a repository already bound to
// a transaction uses the caller's, which is its snapshot.
func (r *InvoiceRepository) Page(ctx context.Context, q model.InvoiceQuery, withSummary bool) (model.InvoicePage, error) {
	if !withSummary {
		rows, err := listInvoices(ctx, r.conn, q)
		if err != nil {
			return model.InvoicePage{}, err
		}
		return model.InvoicePage{Rows: rows}, nil
	}

	var page model.InvoicePage
	read := func(ctx context.Context, conn db.DBTX) error {
		rows, err := listInvoices(ctx, conn, q)
		if err != nil {
			return err
		}
		summary, err := summarizeInvoices(ctx, conn, q.Filter)
		if err != nil {
			return err
		}
		page = model.InvoicePage{Rows: rows, Summary: &summary}
		return nil
	}

	if pool, ok := r.conn.(TxOptionsBeginner); ok {
		err := WithReadSnapshot(ctx, pool, func(ctx context.Context, tx pgx.Tx) error { return read(ctx, tx) })
		return page, err
	}
	return page, read(ctx, r.conn)
}

func listInvoices(ctx context.Context, conn db.DBTX, q model.InvoiceQuery) ([]model.InvoiceRow, error) {
	stmt, err := BuildInvoiceListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(ctx, stmt.SQL, stmt.Args...)
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

func summarizeInvoices(ctx context.Context, conn db.DBTX, f model.InvoiceFilter) (model.InvoiceSummary, error) {
	stmt, err := BuildInvoiceSummaryQuery(f)
	if err != nil {
		return model.InvoiceSummary{}, err
	}

	var (
		s                               model.InvoiceSummary
		total, discount, outstandingSum int64
	)
	if err := conn.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&s.Count, &total, &discount, &outstandingSum); err != nil {
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
