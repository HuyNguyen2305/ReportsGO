package service

import "reportsgo/internal/model"

// EncodeCreditCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
func EncodeCreditCursor(sort model.CreditSort, row model.CreditRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.ID)
}

// DecodeCreditCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodeCreditCursor(raw string, sort model.CreditSort) (*model.CreditCursor, error) {
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, creditValueKind(sort.Key))
	if err != nil {
		return nil, err
	}
	return &model.CreditCursor{SortValue: value, ID: id}, nil
}

func creditValueKind(key model.CreditSortKey) valueKind {
	switch key {
	case model.CreditSortDate:
		return kindDate
	case model.CreditSortAmount:
		return kindMoney
	default:
		return kindText
	}
}
