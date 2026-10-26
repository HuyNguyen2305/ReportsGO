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
	dateLayout      = "2006-01-02"
	// timestampLayout is how the repository renders a created_at sort value (microseconds, always UTC).
	timestampLayout = "2006-01-02T15:04:05.000000Z"
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

// valueKind is the shape of a sort value, which decides how a cursor's value is validated.
type valueKind int

const (
	kindText      valueKind = iota // lower-cased names, labels, emails, numbers, statuses
	kindDate                       // YYYY-MM-DD
	kindTimestamp                  // UTC instant in the repository's fixed form, 2006-01-02T15:04:05.000000Z
	kindMoney                      // decimal amount with two places
)

// encodeCursor returns the opaque cursor that resumes after the row with the given sort value and id.
// Cursors are not signed: a forged one can only point at another position of the same result set, and its
// value is validated and bound as a parameter, so there is nothing to protect.
func encodeCursor(key string, desc bool, value string, id int64) string {
	// Marshalling a struct of plain strings, a bool and an int cannot fail.
	b, _ := json.Marshal(cursorPayload{Key: key, Desc: desc, Val: value, ID: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodeCursor validates a cursor received from a client for a list ordered by key and direction, and returns
// its sort value and id. Every failure is an *InvalidCursorError.
func decodeCursor(raw, key string, desc bool, kind valueKind) (value string, id int64, err error) {
	if len(raw) > maxCursorLength {
		return "", 0, invalidCursor("is too long")
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return "", 0, invalidCursor("is not a valid cursor")
	}

	var p cursorPayload
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return "", 0, invalidCursor("is not a valid cursor")
	}
	if dec.More() {
		return "", 0, invalidCursor("is not a valid cursor")
	}

	if p.Key != key || p.Desc != desc {
		return "", 0, invalidCursor("does not match the requested sort")
	}
	if p.ID <= 0 {
		return "", 0, invalidCursor("is not a valid cursor")
	}
	if err := validateSortValue(kind, p.Val); err != nil {
		return "", 0, err
	}
	return p.Val, p.ID, nil
}

// validateSortValue checks that v has the shape the key's SQL type expects, so a bad value is rejected
// here as a client error instead of failing in the database.
func validateSortValue(kind valueKind, v string) error {
	switch kind {
	case kindDate:
		t, err := time.Parse(dateLayout, v)
		if err != nil || t.Format(dateLayout) != v {
			return invalidCursor("is not a valid cursor")
		}
	case kindTimestamp:
		// The layout is fixed-width, so a successful parse means the text is exactly what the repository renders.
		if _, err := time.Parse(timestampLayout, v); err != nil {
			return invalidCursor("is not a valid cursor")
		}
	case kindMoney:
		if _, err := model.ParseMoney(v); err != nil {
			if errors.Is(err, model.ErrInvalidMoney) {
				return invalidCursor("is not a valid cursor")
			}
			return fmt.Errorf("validate cursor amount: %w", err)
		}
	default:
		if !utf8.ValidString(v) || strings.ContainsRune(v, 0) || utf8.RuneCountInString(v) > model.MaxSortTextLength {
			return invalidCursor("is not a valid cursor")
		}
	}
	return nil
}
