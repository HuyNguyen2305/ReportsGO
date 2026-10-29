package service

import "reportsgo/internal/model"

// EncodeTotalSaleCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
func EncodeTotalSaleCursor(sort model.TotalSaleSort, row model.TotalSaleRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.ID)
}

// DecodeTotalSaleCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodeTotalSaleCursor(raw string, sort model.TotalSaleSort) (*model.TotalSaleCursor, error) {
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, totalSaleValueKind(sort.Key))
	if err != nil {
		return nil, err
	}
	return &model.TotalSaleCursor{SortValue: value, ID: id}, nil
}

func totalSaleValueKind(key model.TotalSaleSortKey) valueKind {
	switch key {
	case model.TotalSaleSortDate:
		return kindDate
	case model.TotalSaleSortAmount, model.TotalSaleSortWithoutTax:
		return kindMoney
	default:
		return kindText
	}
}
