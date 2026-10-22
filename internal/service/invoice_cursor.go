package service

import "reportsgo/internal/model"

// EncodeInvoiceCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
func EncodeInvoiceCursor(sort model.InvoiceSort, row model.InvoiceRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.ID)
}

// DecodeInvoiceCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodeInvoiceCursor(raw string, sort model.InvoiceSort) (*model.InvoiceCursor, error) {
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, invoiceValueKind(sort.Key))
	if err != nil {
		return nil, err
	}
	return &model.InvoiceCursor{SortValue: value, ID: id}, nil
}

func invoiceValueKind(key model.InvoiceSortKey) valueKind {
	switch key {
	case model.SortDate:
		return kindDate
	case model.SortTotal, model.SortDiscount:
		return kindMoney
	default:
		return kindText
	}
}
