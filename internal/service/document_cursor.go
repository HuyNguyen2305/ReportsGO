package service

import "reportsgo/internal/model"

// EncodeDocumentCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
func EncodeDocumentCursor(sort model.DocumentSort, row model.DocumentRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.ID)
}

// DecodeDocumentCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodeDocumentCursor(raw string, sort model.DocumentSort) (*model.DocumentCursor, error) {
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, documentValueKind(sort.Key))
	if err != nil {
		return nil, err
	}
	return &model.DocumentCursor{SortValue: value, ID: id}, nil
}

func documentValueKind(key model.DocumentSortKey) valueKind {
	if key == model.DocumentSortCreatedAt {
		return kindTimestamp
	}
	return kindText
}
