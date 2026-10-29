package entity

import (
	"errors"
	"math"
	"testing"
	"time"
)

var testDate = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

type validateCase struct {
	name      string
	v         Validatable
	wantField string
	wantMsg   string
}

func runValidateCases(t *testing.T, tests []validateCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.v.Validate()
			if tt.wantMsg == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("Validate() = %v, want *ValidationError", err)
			}
			if ve.Field != tt.wantField || ve.Message != tt.wantMsg || err.Error() != tt.wantMsg {
				t.Fatalf("got field=%q msg=%q, want field=%q msg=%q", ve.Field, ve.Message, tt.wantField, tt.wantMsg)
			}
		})
	}
}

func TestTransactionValidate(t *testing.T) {
	t.Parallel()
	runValidateCases(t, []validateCase{
		{"valid", Transaction{Amount: 10, Category: "food", Date: testDate}, "", ""},
		{"zero amount", Transaction{Amount: 0, Category: "food", Date: testDate}, "amount", "amount must be positive"},
		{"negative amount", Transaction{Amount: -5, Category: "food", Date: testDate}, "amount", "amount must be positive"},
		{"nan amount", Transaction{Amount: math.NaN(), Category: "food", Date: testDate}, "amount", "amount must be positive"},
		{"inf amount", Transaction{Amount: math.Inf(1), Category: "food", Date: testDate}, "amount", "amount must be positive"},
		{"empty category", Transaction{Amount: 10, Category: "", Date: testDate}, "category", "category must not be empty"},
		{"blank category", Transaction{Amount: 10, Category: "   ", Date: testDate}, "category", "category must not be empty"},
		{"zero date", Transaction{Amount: 10, Category: "food"}, "date", "date must be set"},
		{"first error wins", Transaction{Amount: -1}, "amount", "amount must be positive"},
	})
}

func TestBudgetValidate(t *testing.T) {
	t.Parallel()
	runValidateCases(t, []validateCase{
		{"valid", Budget{Category: "food", Limit: 100}, "", ""},
		{"zero limit", Budget{Category: "food", Limit: 0}, "limit", "limit must be positive"},
		{"negative limit", Budget{Category: "food", Limit: -1}, "limit", "limit must be positive"},
		{"nan limit", Budget{Category: "food", Limit: math.NaN()}, "limit", "limit must be positive"},
		{"inf limit", Budget{Category: "food", Limit: math.Inf(-1)}, "limit", "limit must be positive"},
		{"empty category", Budget{Category: "", Limit: 100}, "category", "category must not be empty"},
		{"blank category", Budget{Category: " \t", Limit: 100}, "category", "category must not be empty"},
	})
}

func TestCheckValid(t *testing.T) {
	t.Parallel()
	items := []Validatable{
		Transaction{Amount: 10, Category: "food", Date: testDate},
		Budget{Category: "food", Limit: 100},
	}
	for _, v := range items {
		if err := CheckValid(v); err != nil {
			t.Errorf("CheckValid(%T) = %v, want nil", v, err)
		}
	}
	bad := []struct {
		v   Validatable
		msg string
	}{
		{Transaction{}, "amount must be positive"},
		{Budget{}, "limit must be positive"},
	}
	for _, tt := range bad {
		var ve *ValidationError
		err := CheckValid(tt.v)
		if !errors.As(err, &ve) {
			t.Errorf("CheckValid(%T) = %v, want *ValidationError", tt.v, err)
			continue
		}
		if ve.Message != tt.msg {
			t.Errorf("CheckValid(%T) message = %q, want %q", tt.v, ve.Message, tt.msg)
		}
	}
}
