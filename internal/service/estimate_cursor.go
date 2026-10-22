package service

import "reportsgo/internal/model"

// EncodeEstimateCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
func EncodeEstimateCursor(sort model.EstimateSort, row model.EstimateRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.ID)
}

// DecodeEstimateCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodeEstimateCursor(raw string, sort model.EstimateSort) (*model.EstimateCursor, error) {
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, estimateValueKind(sort.Key))
	if err != nil {
		return nil, err
	}
	return &model.EstimateCursor{SortValue: value, ID: id}, nil
}

func estimateValueKind(key model.EstimateSortKey) valueKind {
	switch key {
	case model.EstimateSortDate:
		return kindDate
	case model.EstimateSortTotal:
		return kindMoney
	default:
		return kindText
	}
}
