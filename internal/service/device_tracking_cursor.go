package service

import "reportsgo/internal/model"

// EncodeDeviceTrackingCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
func EncodeDeviceTrackingCursor(sort model.DeviceTrackingSort, row model.DeviceTrackingRow) string {
	return encodeCursor(string(sort.Key), sort.Desc, row.SortValue, row.ID)
}

// DecodeDeviceTrackingCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodeDeviceTrackingCursor(raw string, sort model.DeviceTrackingSort) (*model.DeviceTrackingCursor, error) {
	value, id, err := decodeCursor(raw, string(sort.Key), sort.Desc, deviceTrackingValueKind(sort.Key))
	if err != nil {
		return nil, err
	}
	return &model.DeviceTrackingCursor{SortValue: value, ID: id}, nil
}

// deviceTrackingValueKind is the shape of each key's sort value: the date is a timestamp, check in and out are epoch
// milliseconds (a number, 0 when the device was not checked), and everything else is text.
func deviceTrackingValueKind(key model.DeviceTrackingSortKey) valueKind {
	switch key {
	case model.DeviceTrackingSortDate:
		return kindTimestamp
	case model.DeviceTrackingSortCheckIn, model.DeviceTrackingSortCheckOut:
		return kindNumber
	default:
		return kindText
	}
}
