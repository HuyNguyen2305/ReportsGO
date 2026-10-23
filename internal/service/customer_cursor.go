package service

import "reportsgo/internal/model"

// EncodeCustomerCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
func EncodeCustomerCursor(sort model.CustomerSort, row model.CustomerRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.ID)
}

// DecodeCustomerCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodeCustomerCursor(raw string, sort model.CustomerSort) (*model.CustomerCursor, error) {
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, customerValueKind(sort.Key))
	if err != nil {
		return nil, err
	}
	return &model.CustomerCursor{SortValue: value, ID: id}, nil
}

func customerValueKind(key model.CustomerSortKey) valueKind {
	if key == model.CustomerSortCreatedAt {
		return kindTimestamp
	}
	return kindText
}
