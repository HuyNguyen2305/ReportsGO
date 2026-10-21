// Package model holds domain types and errors. It has no HTTP or database dependencies.
package model

import (
	"errors"
	"strings"
)

// Domain errors. Services wrap them with context using %w; the handler layer maps them to HTTP codes.
var (
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrInvalidInput = errors.New("invalid input")
)

// FieldError describes one invalid input field.
type FieldError struct {
	Field   string
	Message string
}

// InvalidInputError is an ErrInvalidInput that carries the offending fields.
type InvalidInputError struct {
	Fields []FieldError
}

// InvalidInput returns an error matching ErrInvalidInput that lists the invalid fields.
func InvalidInput(fields ...FieldError) error {
	return &InvalidInputError{Fields: fields}
}

func (e *InvalidInputError) Error() string {
	if len(e.Fields) == 0 {
		return ErrInvalidInput.Error()
	}
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Field + ": " + f.Message
	}
	return ErrInvalidInput.Error() + ": " + strings.Join(parts, "; ")
}

// Is makes errors.Is(err, ErrInvalidInput) true.
func (e *InvalidInputError) Is(target error) bool {
	return target == ErrInvalidInput
}
