package service

import "reportsgo/internal/model"

// EncodeAgingCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
func EncodeAgingCursor(sort model.AgingSort, row model.AgingRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.ID)
}

// DecodeAgingCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodeAgingCursor(raw string, sort model.AgingSort) (*model.AgingCursor, error) {
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, agingValueKind(sort.Key))
	if err != nil {
		return nil, err
	}
	return &model.AgingCursor{SortValue: value, ID: id}, nil
}

func agingValueKind(key model.AgingSortKey) valueKind {
	if key == model.AgingSortCustomer {
		return kindText
	}
	return kindMoney
}
