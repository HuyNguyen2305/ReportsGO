package service

import "reportsgo/internal/model"

// EncodeNoActiveJobsCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
func EncodeNoActiveJobsCursor(sort model.NoActiveJobsSort, row model.NoActiveJobsRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.ID)
}

// DecodeNoActiveJobsCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodeNoActiveJobsCursor(raw string, sort model.NoActiveJobsSort) (*model.NoActiveJobsCursor, error) {
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, noActiveJobsValueKind(sort.Key))
	if err != nil {
		return nil, err
	}
	return &model.NoActiveJobsCursor{SortValue: value, ID: id}, nil
}

func noActiveJobsValueKind(key model.NoActiveJobsSortKey) valueKind {
	if key == model.NoActiveJobsSortLastServiceDate {
		return kindTimestamp
	}
	return kindText
}
