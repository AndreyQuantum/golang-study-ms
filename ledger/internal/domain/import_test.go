package domain

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportErrorUnwrap(t *testing.T) {
	e := ImportError{Index: 3, Err: ErrBudgetExceeded}
	assert.True(t, errors.Is(e, ErrBudgetExceeded))
	assert.Contains(t, e.Error(), "3")
	assert.Contains(t, e.Error(), "budget exceeded")

	ve := &ValidationError{Field: "amount", Message: "amount must be positive"}
	var got *ValidationError
	require.True(t, errors.As(ImportError{Index: 0, Err: ve}, &got))
	assert.Equal(t, "amount", got.Field)
}

func TestImportLimits(t *testing.T) {
	assert.Equal(t, 4, DefaultImportWorkers)
	assert.Equal(t, 32, MaxImportWorkers)
	assert.Equal(t, 1000, MaxImportItems)
	assert.LessOrEqual(t, DefaultImportWorkers, MaxImportWorkers)
}
