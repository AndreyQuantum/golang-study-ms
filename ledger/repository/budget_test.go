package repository

import (
	"errors"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/AndreyQuantum/golang-study-ms/ledger/entity"
)

func TestSetBudget_AddsAndUpdates(t *testing.T) {
	r := NewBudgetRepository()

	r.SetBudget(entity.Budget{Category: "food", Limit: 5000})
	if b, ok := r.GetBudget("food"); !ok || b.Limit != 5000 {
		t.Fatalf("after add: got %+v, ok=%v; want limit 5000", b, ok)
	}

	r.SetBudget(entity.Budget{Category: "food", Limit: 7000})
	if b, ok := r.GetBudget("food"); !ok || b.Limit != 7000 {
		t.Fatalf("after update: got %+v, ok=%v; want limit 7000", b, ok)
	}
	if n := len(r.ListBudgets()); n != 1 {
		t.Fatalf("budgets count = %d, want 1", n)
	}
}

func TestLoadBudgets_Valid(t *testing.T) {
	r := NewBudgetRepository()
	input := `[{"category": "food", "limit": 5000}, {"category": "transport", "limit": 3000}]`

	if err := r.LoadBudgets(strings.NewReader(input)); err != nil {
		t.Fatalf("LoadBudgets: %v", err)
	}

	want := []entity.Budget{
		{Category: "food", Limit: 5000},
		{Category: "transport", Limit: 3000},
	}
	got := r.ListBudgets()
	if len(got) != len(want) {
		t.Fatalf("got %d budgets, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("budget[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestLoadBudgets_Errors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"invalid json", `[{"category": "food",`, "parse budgets json"},
		{"not an array", `{"category": "food", "limit": 5000}`, "parse budgets json"},
		{"wrong type", `[{"category": "food", "limit": "5000"}]`, "parse budgets json"},
		{"unknown field", `[{"category": "food", "limt": 5000}]`, "unknown field"},
		{"empty category", `[{"category": "", "limit": 5000}]`, "empty category"},
		{"negative limit", `[{"category": "food", "limit": -1}]`, "negative limit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewBudgetRepository()
			err := r.LoadBudgets(strings.NewReader(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadBudgets_ReadError(t *testing.T) {
	readErr := errors.New("disk failure")
	r := NewBudgetRepository()

	err := r.LoadBudgets(iotest.ErrReader(readErr))
	if !errors.Is(err, readErr) {
		t.Fatalf("err = %v, want wrapping %v", err, readErr)
	}
}

func TestLoadBudgets_InvalidDoesNotModifyStorage(t *testing.T) {
	r := NewBudgetRepository()
	r.SetBudget(entity.Budget{Category: "food", Limit: 5000})

	input := `[{"category": "food", "limit": 100}, {"category": "", "limit": 1}]`
	if err := r.LoadBudgets(strings.NewReader(input)); err == nil {
		t.Fatal("expected error")
	}

	if b, _ := r.GetBudget("food"); b.Limit != 5000 {
		t.Fatalf("food limit = %.2f, want unchanged 5000", b.Limit)
	}
}
