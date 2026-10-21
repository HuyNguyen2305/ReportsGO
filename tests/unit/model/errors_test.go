package model_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
)

func TestInvalidInput(t *testing.T) {
	fields := []model.FieldError{
		{Field: "from", Message: "must be a date"},
		{Field: "to", Message: "must not be before from"},
	}

	tests := []struct {
		name string
		err  error
	}{
		{"direct", model.InvalidInput(fields...)},
		{"wrapped once", fmt.Errorf("list invoices: %w", model.InvalidInput(fields...))},
		{"wrapped twice", fmt.Errorf("handler: %w", fmt.Errorf("service: %w", model.InvalidInput(fields...)))},
		{"joined", errors.Join(errors.New("other"), model.InvalidInput(fields...))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, tt.err, model.ErrInvalidInput)

			var got *model.InvalidInputError
			require.ErrorAs(t, tt.err, &got)
			require.Equal(t, fields, got.Fields)
		})
	}
}

func TestInvalidInputIsNotOtherDomainErrors(t *testing.T) {
	err := model.InvalidInput(model.FieldError{Field: "x", Message: "bad"})
	require.NotErrorIs(t, err, model.ErrNotFound)
	require.NotErrorIs(t, err, model.ErrConflict)
}

func TestInvalidInputErrorMessage(t *testing.T) {
	tests := []struct {
		name   string
		fields []model.FieldError
		want   string
	}{
		{"no fields", nil, "invalid input"},
		{"one field", []model.FieldError{{Field: "from", Message: "is required"}}, "invalid input: from: is required"},
		{
			"two fields",
			[]model.FieldError{{Field: "from", Message: "is required"}, {Field: "to", Message: "is required"}},
			"invalid input: from: is required; to: is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.EqualError(t, model.InvalidInput(tt.fields...), tt.want)
		})
	}
}
