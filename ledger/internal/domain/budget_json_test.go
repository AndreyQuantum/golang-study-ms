package domain

import (
	"errors"
	"strings"
	"testing"
	"testing/iotest"
)

func TestParseBudgets_Valid(t *testing.T) {
	t.Parallel()
	input := `[{"category": "food", "limit": 5000}, {"category": "transport", "limit": 3000}]`
	got, err := ParseBudgets(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseBudgets: %v", err)
	}
	want := []Budget{{Category: "food", Limit: 5000}, {Category: "transport", Limit: 3000}}
	if len(got) != len(want) {
		t.Fatalf("got %d budgets, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("budget[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseBudgets_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"invalid json", `[{"category": "food",`, "parse budgets json"},
		{"not an array", `{"category": "food", "limit": 5000}`, "parse budgets json"},
		{"wrong type", `[{"category": "food", "limit": "5000"}]`, "parse budgets json"},
		{"unknown field", `[{"category": "food", "limt": 5000}]`, "unknown field"},
		{"empty category", `[{"category": "", "limit": 5000}]`, "budget #0: category must not be empty"},
		{"negative limit", `[{"category": "food", "limit": -1}]`, "budget #0: limit must be positive"},
		{"second is bad", `[{"category": "a", "limit": 1}, {"category": "b", "limit": 0}]`, "budget #1: limit must be positive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseBudgets(strings.NewReader(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseBudgets_ReadError(t *testing.T) {
	t.Parallel()
	readErr := errors.New("disk failure")
	if _, err := ParseBudgets(iotest.ErrReader(readErr)); !errors.Is(err, readErr) {
		t.Fatalf("err = %v, want wrapping %v", err, readErr)
	}
}
