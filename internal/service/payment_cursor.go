package service

import "reportsgo/internal/model"

// EncodePaymentCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
func EncodePaymentCursor(sort model.PaymentSort, row model.PaymentRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.ID)
}

// DecodePaymentCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodePaymentCursor(raw string, sort model.PaymentSort) (*model.PaymentCursor, error) {
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, paymentValueKind(sort.Key))
	if err != nil {
		return nil, err
	}
	return &model.PaymentCursor{SortValue: value, ID: id}, nil
}

func paymentValueKind(key model.PaymentSortKey) valueKind {
	switch key {
	case model.PaymentSortDate:
		return kindDate
	case model.PaymentSortAmount:
		return kindMoney
	default:
		return kindText
	}
}
