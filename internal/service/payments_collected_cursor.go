package service

import "reportsgo/internal/model"

// EncodePaymentCollectedCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
func EncodePaymentCollectedCursor(sort model.PaymentCollectedSort, row model.PaymentCollectedRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.ID)
}

// DecodePaymentCollectedCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodePaymentCollectedCursor(raw string, sort model.PaymentCollectedSort) (*model.PaymentCollectedCursor, error) {
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, paymentCollectedValueKind(sort.Key))
	if err != nil {
		return nil, err
	}
	return &model.PaymentCollectedCursor{SortValue: value, ID: id}, nil
}

func paymentCollectedValueKind(key model.PaymentCollectedSortKey) valueKind {
	switch key {
	case model.PaymentCollectedSortDate:
		return kindDate
	case model.PaymentCollectedSortAmount, model.PaymentCollectedSortWithoutTax:
		return kindMoney
	default:
		return kindText
	}
}
