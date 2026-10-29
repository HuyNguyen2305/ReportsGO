package service

import "reportsgo/internal/model"

// EncodeServiceLookupCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
func EncodeServiceLookupCursor(sort model.ServiceLookupSort, row model.ServiceLookupRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.ID)
}

// DecodeServiceLookupCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodeServiceLookupCursor(raw string, sort model.ServiceLookupSort) (*model.ServiceLookupCursor, error) {
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, serviceLookupValueKind(sort.Key))
	if err != nil {
		return nil, err
	}
	return &model.ServiceLookupCursor{SortValue: value, ID: id}, nil
}

func serviceLookupValueKind(key model.ServiceLookupSortKey) valueKind {
	switch key {
	case model.ServiceLookupSortScheduledAt:
		return kindTimestamp
	case model.ServiceLookupSortInvoiceValue, model.ServiceLookupSortWithoutTax:
		return kindMoney
	default:
		return kindText
	}
}
