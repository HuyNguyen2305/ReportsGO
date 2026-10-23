package service

import "reportsgo/internal/model"

// EncodeJobCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
func EncodeJobCursor(sort model.JobSort, row model.JobRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.ID)
}

// DecodeJobCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodeJobCursor(raw string, sort model.JobSort) (*model.JobCursor, error) {
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, jobValueKind(sort.Key))
	if err != nil {
		return nil, err
	}
	return &model.JobCursor{SortValue: value, ID: id}, nil
}

func jobValueKind(key model.JobSortKey) valueKind {
	switch key {
	case model.JobSortCreatedAt:
		return kindTimestamp
	case model.JobSortInvoiceValue:
		return kindMoney
	default:
		return kindText
	}
}
