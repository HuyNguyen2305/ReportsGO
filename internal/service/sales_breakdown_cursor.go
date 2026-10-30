package service

import "reportsgo/internal/model"

// EncodeSalesBreakdownCursor returns the opaque cursor that resumes after row, for a grid ordered by sort.
func EncodeSalesBreakdownCursor(sort model.SalesBreakdownSort, row model.SalesBreakdownRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.ID)
}

// DecodeSalesBreakdownCursor validates a cursor received from a client for a grid ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodeSalesBreakdownCursor(raw string, sort model.SalesBreakdownSort) (*model.SalesBreakdownCursor, error) {
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, salesBreakdownValueKind(sort.Key))
	if err != nil {
		return nil, err
	}
	return &model.SalesBreakdownCursor{SortValue: value, ID: id}, nil
}

func salesBreakdownValueKind(key model.SalesBreakdownSortKey) valueKind {
	if key == model.SalesBreakdownSortName || key == model.SalesBreakdownSortAccountNumber {
		return kindText
	}
	return kindMoney
}
