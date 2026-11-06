package service

import "reportsgo/internal/model"

// EncodeMaterialUseCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
func EncodeMaterialUseCursor(sort model.MaterialUseSort, row model.MaterialUseRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.MaterialID)
}

// DecodeMaterialUseCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodeMaterialUseCursor(raw string, sort model.MaterialUseSort) (*model.MaterialUseCursor, error) {
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, materialUseValueKind(sort.Key))
	if err != nil {
		return nil, err
	}
	return &model.MaterialUseCursor{SortValue: value, ID: id}, nil
}

func materialUseValueKind(key model.MaterialUseSortKey) valueKind {
	switch key {
	case model.MaterialUseSortQty, model.MaterialUseSortApplications:
		return kindNumber
	default:
		return kindText
	}
}
