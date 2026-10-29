package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger/entity"
)

var testDate = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func tx(category string, amount float64) entity.Transaction {
	return entity.Transaction{Category: category, Amount: amount, Date: testDate}
}

func newRepoWithBudget(t *testing.T, category string, limit float64) (*TransactionRepository, *BudgetRepository) {
	t.Helper()
	budgets := NewBudgetRepository()
	if err := budgets.SetBudget(entity.Budget{Category: category, Limit: limit}); err != nil {
		t.Fatalf("SetBudget: %v", err)
	}
	return NewTransactionRepository(budgets), budgets
}

func TestAddTransaction_WithinLimit(t *testing.T) {
	repo, _ := newRepoWithBudget(t, "food", 5000)

	if err := repo.AddTransaction(tx("food", 1000)); err != nil {
		t.Fatalf("AddTransaction: %v", err)
	}
	if err := repo.AddTransaction(tx("food", 4000)); err != nil {
		t.Fatalf("AddTransaction up to exact limit: %v", err)
	}
	if n := len(repo.ListTransactions()); n != 2 {
		t.Fatalf("transactions = %d, want 2", n)
	}
}

func TestAddTransaction_FloatSumAtLimit(t *testing.T) {
	repo, _ := newRepoWithBudget(t, "food", 0.3)

	if err := repo.AddTransaction(tx("food", 0.1)); err != nil {
		t.Fatalf("AddTransaction: %v", err)
	}
	if err := repo.AddTransaction(tx("food", 0.2)); err != nil {
		t.Fatalf("0.1+0.2 must fit limit 0.3: %v", err)
	}
}

func TestAddTransaction_ExceedsLimitIsNotSaved(t *testing.T) {
	repo, _ := newRepoWithBudget(t, "food", 5000)

	if err := repo.AddTransaction(tx("food", 4500)); err != nil {
		t.Fatalf("AddTransaction: %v", err)
	}

	over := tx("food", 600)
	over.Description = "over"
	err := repo.AddTransaction(over)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("err = %v, want ErrBudgetExceeded", err)
	}

	txs := repo.ListTransactions()
	if len(txs) != 1 {
		t.Fatalf("transactions = %d, want 1", len(txs))
	}
	for _, got := range txs {
		if got.Description == "over" {
			t.Fatal("rejected transaction was saved")
		}
	}
}

func TestAddTransaction_NoBudgetForCategory(t *testing.T) {
	repo, _ := newRepoWithBudget(t, "food", 100)

	if err := repo.AddTransaction(tx("health", 1_000_000)); err != nil {
		t.Fatalf("AddTransaction without budget: %v", err)
	}
}

func TestAddTransaction_AfterLimitIncrease(t *testing.T) {
	repo, budgets := newRepoWithBudget(t, "food", 5000)
	big := tx("food", 5500)

	if err := repo.AddTransaction(big); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("err = %v, want ErrBudgetExceeded", err)
	}

	if err := budgets.SetBudget(entity.Budget{Category: "food", Limit: 6000}); err != nil {
		t.Fatalf("SetBudget: %v", err)
	}
	if err := repo.AddTransaction(big); err != nil {
		t.Fatalf("AddTransaction after limit increase: %v", err)
	}
}

func TestAddTransaction_AssignsSequentialIDs(t *testing.T) {
	repo, _ := newRepoWithBudget(t, "food", 100)

	_ = repo.AddTransaction(tx("other", 1))
	_ = repo.AddTransaction(tx("food", 500))
	_ = repo.AddTransaction(tx("other", 2))

	txs := repo.ListTransactions()
	if len(txs) != 2 {
		t.Fatalf("transactions = %d, want 2", len(txs))
	}
	for i, got := range txs {
		if got.ID != i+1 {
			t.Errorf("txs[%d].ID = %d, want %d", i, got.ID, i+1)
		}
	}
}

func TestAddTransaction_InvalidIsNotSaved(t *testing.T) {
	tests := []struct {
		name string
		tx   entity.Transaction
		want string
	}{
		{"zero amount", entity.Transaction{Category: "food", Date: testDate}, "amount must be positive"},
		{"negative amount", tx("food", -10), "amount must be positive"},
		{"empty category", tx("  ", 10), "category must not be empty"},
		{"zero date", entity.Transaction{Category: "food", Amount: 10}, "date must be set"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, _ := newRepoWithBudget(t, "food", 5000)
			err := repo.AddTransaction(tt.tx)

			var ve *entity.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("err = %v, want *ValidationError", err)
			}
			if err.Error() != tt.want {
				t.Fatalf("err = %q, want %q", err.Error(), tt.want)
			}
			if n := len(repo.ListTransactions()); n != 0 {
				t.Fatalf("transactions = %d, want 0", n)
			}
		})
	}
}
