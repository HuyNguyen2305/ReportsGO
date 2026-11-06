package service

import "reportsgo/internal/model"

// EncodeOnlineBookingCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
func EncodeOnlineBookingCursor(sort model.OnlineBookingSort, row model.OnlineBookingRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.ID)
}

// DecodeOnlineBookingCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodeOnlineBookingCursor(raw string, sort model.OnlineBookingSort) (*model.OnlineBookingCursor, error) {
	kind := kindText
	if sort.Key == model.OnlineBookingSortScheduledAt {
		kind = kindTimestamp
	}
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, kind)
	if err != nil {
		return nil, err
	}
	return &model.OnlineBookingCursor{SortValue: value, ID: id}, nil
}
