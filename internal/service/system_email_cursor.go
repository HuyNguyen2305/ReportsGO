package service

import "reportsgo/internal/model"

// EncodeSystemEmailCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
func EncodeSystemEmailCursor(sort model.SystemEmailSort, row model.SystemEmailRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.ID)
}

// DecodeSystemEmailCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodeSystemEmailCursor(raw string, sort model.SystemEmailSort) (*model.SystemEmailCursor, error) {
	kind := kindText
	if sort.Key == model.SystemEmailSortTimeStamp {
		kind = kindTimestamp
	}
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, kind)
	if err != nil {
		return nil, err
	}
	return &model.SystemEmailCursor{SortValue: value, ID: id}, nil
}
