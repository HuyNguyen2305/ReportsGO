package service

import "reportsgo/internal/model"

// EncodeRecurringCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
func EncodeRecurringCursor(sort model.RecurringSort, row model.RecurringRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.ID)
}

// DecodeRecurringCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodeRecurringCursor(raw string, sort model.RecurringSort) (*model.RecurringCursor, error) {
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, recurringValueKind(sort.Key))
	if err != nil {
		return nil, err
	}
	return &model.RecurringCursor{SortValue: value, ID: id}, nil
}

func recurringValueKind(key model.RecurringSortKey) valueKind {
	switch key {
	case model.RecurringSortStartedOn, model.RecurringSortEndsOn, model.RecurringSortNextInvoiceDate:
		return kindDate
	case model.RecurringSortTotalValue:
		return kindMoney
	default:
		return kindText
	}
}
