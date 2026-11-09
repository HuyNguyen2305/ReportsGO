package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"reportsgo/internal/repository/db"
)

// The queries behind the reports' filter dropdowns. Each selects the columns of its model type, in field order.
//
// shortcut: no LIMIT, so listClientsSQL and listStaffSQL return every customer and user. A cap would make the
// rows past it impossible to pick; add a search box (a q filter) when customers pass a few thousand.
const (
	listServiceTypesSQL  = `SELECT id, name FROM service_types ORDER BY name, id`
	listInvoiceTagsSQL   = `SELECT id, name FROM invoice_tags ORDER BY name, id`
	listPaymentTermsSQL  = `SELECT id, name, description FROM payment_terms ORDER BY name, id`
	listDocumentTypesSQL = `SELECT id, name FROM document_types ORDER BY name, id`
	listLeadSourcesSQL   = `SELECT id, name FROM lead_sources ORDER BY name, id`
	listStaffSQL         = `SELECT id, full_name FROM users ORDER BY full_name, id`
	listTaxRatesSQL      = `SELECT id, name, rate::text, active FROM tax_rates ORDER BY name, id`
	listJobTagsSQL       = `SELECT id, name FROM job_tags ORDER BY name, id`
	listItemsSQL         = `SELECT id, name FROM items ORDER BY name, id`
	listMaterialsSQL     = `SELECT id, name FROM materials ORDER BY name, id`
	listClientsSQL       = `SELECT id, display_name FROM customers ORDER BY display_name, id`
)

// lookup runs one of the queries above and returns its rows as T, whose fields follow the select list in order.
// what names the list in the error, for example "service types". It returns an empty slice, never nil.
func lookup[T any](ctx context.Context, conn db.DBTX, what, sql string) ([]T, error) {
	rows, err := conn.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", what, err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[T])
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", what, err)
	}
	return out, nil
}
