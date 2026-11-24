package domain

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelErrors(t *testing.T) {
	t.Parallel()
	if !errors.Is(fmt.Errorf("x: %w", ErrBudgetExceeded), ErrBudgetExceeded) {
		t.Error("wrapped ErrBudgetExceeded is not recognised")
	}
	if errors.Is(ErrNotFound, ErrBudgetExceeded) {
		t.Error("ErrNotFound must differ from ErrBudgetExceeded")
	}
}
