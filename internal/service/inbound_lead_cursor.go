package service

import "reportsgo/internal/model"

// EncodeInboundLeadCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
func EncodeInboundLeadCursor(sort model.InboundLeadSort, row model.InboundLeadRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.ID)
}

// DecodeInboundLeadCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodeInboundLeadCursor(raw string, sort model.InboundLeadSort) (*model.InboundLeadCursor, error) {
	kind := kindText
	if sort.Key == model.InboundLeadSortDateAdded {
		kind = kindTimestamp
	}
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, kind)
	if err != nil {
		return nil, err
	}
	return &model.InboundLeadCursor{SortValue: value, ID: id}, nil
}
