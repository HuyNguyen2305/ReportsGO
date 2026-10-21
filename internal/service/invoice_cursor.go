// Package service holds the business logic. It depends on the domain model and on repository interfaces
// declared here, never on Gin or pgx.
package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"reportsgo/internal/model"
)

const (
	// maxCursorLength bounds the encoded cursor before any decoding work is done.
	maxCursorLength = 4096
	// maxCursorTextValue bounds a text sort value (lower-cased names, job labels, emails) in characters.
	maxCursorTextValue = 1000
	dateLayout         = "2006-01-02"
)

// InvalidCursorError explains why a cursor was rejected. Reason is safe to show to API clients.
type InvalidCursorError struct {
	Reason string
}

func (e *InvalidCursorError) Error() string { return "invalid cursor: " + e.Reason }

func invalidCursor(reason string) error { return &InvalidCursorError{Reason: reason} }

// cursorPayload is the JSON inside a cursor. The sort key and direction travel with it so a cursor cannot be
// replayed against a different ordering, where its position would be meaningless.
type cursorPayload struct {
	Key  string `json:"k"`
	Desc bool   `json:"d"`
	Val  string `json:"v"`
	ID   int64  `json:"id"`
}

// EncodeInvoiceCursor returns the opaque cursor that resumes after row, for a list ordered by sort.
// Cursors are not signed: a forged one can only point at another position of the same result set, and its
// value is validated and bound as a parameter, so there is nothing to protect.
func EncodeInvoiceCursor(sort model.InvoiceSort, row model.InvoiceRow) string {
	// Marshalling a struct of plain strings, a bool and an int cannot fail.
	b, _ := json.Marshal(cursorPayload{Key: string(sort.Key), Desc: sort.Desc, Val: row.SortValue, ID: row.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeInvoiceCursor validates a cursor received from a client for a list ordered by sort.
// Every failure is an *InvalidCursorError.
func DecodeInvoiceCursor(raw string, sort model.InvoiceSort) (*model.InvoiceCursor, error) {
	if len(raw) > maxCursorLength {
		return nil, invalidCursor("is too long")
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, invalidCursor("is not a valid cursor")
	}

	var p cursorPayload
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, invalidCursor("is not a valid cursor")
	}
	if dec.More() {
		return nil, invalidCursor("is not a valid cursor")
	}

	if p.Key != string(sort.Key) || p.Desc != sort.Desc {
		return nil, invalidCursor("does not match the requested sort")
	}
	if p.ID <= 0 {
		return nil, invalidCursor("is not a valid cursor")
	}
	if err := validateSortValue(sort.Key, p.Val); err != nil {
		return nil, err
	}
	return &model.InvoiceCursor{SortValue: p.Val, ID: p.ID}, nil
}

// validateSortValue checks that v has the shape the key's SQL type expects, so a bad value is rejected
// here as a client error instead of failing in the database.
func validateSortValue(key model.InvoiceSortKey, v string) error {
	switch key {
	case model.SortDate:
		t, err := time.Parse(dateLayout, v)
		if err != nil || t.Format(dateLayout) != v {
			return invalidCursor("is not a valid cursor")
		}
	case model.SortTotal, model.SortDiscount:
		if _, err := model.ParseMoney(v); err != nil {
			if errors.Is(err, model.ErrInvalidMoney) {
				return invalidCursor("is not a valid cursor")
			}
			return fmt.Errorf("validate cursor amount: %w", err)
		}
	default:
		if !utf8.ValidString(v) || strings.ContainsRune(v, 0) || utf8.RuneCountInString(v) > maxCursorTextValue {
			return invalidCursor("is not a valid cursor")
		}
	}
	return nil
}
