package repository

import (
	"errors"
	"testing"

	"github.com/AndreyQuantum/golang-study-ms/ledger/entity"
)

func newRepoWithBudget(category string, limit float64) (*TransactionRepository, *BudgetRepository) {
	budgets := NewBudgetRepository()
	budgets.SetBudget(entity.Budget{Category: category, Limit: limit})
	return NewTransactionRepository(budgets), budgets
}

func TestAddTransaction_WithinLimit(t *testing.T) {
	repo, _ := newRepoWithBudget("food", 5000)

	if err := repo.AddTransaction(entity.Transaction{Category: "food", Amount: 1000}); err != nil {
		t.Fatalf("AddTransaction: %v", err)
	}
	if err := repo.AddTransaction(entity.Transaction{Category: "food", Amount: 4000}); err != nil {
		t.Fatalf("AddTransaction up to exact limit: %v", err)
	}
	if n := len(repo.ListTransactions()); n != 2 {
		t.Fatalf("transactions = %d, want 2", n)
	}
}

func TestAddTransaction_ExceedsLimitIsNotSaved(t *testing.T) {
	repo, _ := newRepoWithBudget("food", 5000)

	if err := repo.AddTransaction(entity.Transaction{Category: "food", Amount: 4500}); err != nil {
		t.Fatalf("AddTransaction: %v", err)
	}

	err := repo.AddTransaction(entity.Transaction{Category: "food", Amount: 600, Description: "over"})
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("err = %v, want ErrBudgetExceeded", err)
	}

	txs := repo.ListTransactions()
	if len(txs) != 1 {
		t.Fatalf("transactions = %d, want 1", len(txs))
	}
	for _, tx := range txs {
		if tx.Description == "over" {
			t.Fatal("rejected transaction was saved")
		}
	}
}

func TestAddTransaction_NoBudgetForCategory(t *testing.T) {
	repo, _ := newRepoWithBudget("food", 100)

	if err := repo.AddTransaction(entity.Transaction{Category: "health", Amount: 1_000_000}); err != nil {
		t.Fatalf("AddTransaction without budget: %v", err)
	}
}

func TestAddTransaction_AfterLimitIncrease(t *testing.T) {
	repo, budgets := newRepoWithBudget("food", 5000)
	tx := entity.Transaction{Category: "food", Amount: 5500}

	if err := repo.AddTransaction(tx); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("err = %v, want ErrBudgetExceeded", err)
	}

	budgets.SetBudget(entity.Budget{Category: "food", Limit: 6000})
	if err := repo.AddTransaction(tx); err != nil {
		t.Fatalf("AddTransaction after limit increase: %v", err)
	}
}

func TestAddTransaction_AssignsSequentialIDs(t *testing.T) {
	repo, _ := newRepoWithBudget("food", 100)

	_ = repo.AddTransaction(entity.Transaction{Category: "other", Amount: 1})
	_ = repo.AddTransaction(entity.Transaction{Category: "food", Amount: 500}) // отклонена
	_ = repo.AddTransaction(entity.Transaction{Category: "other", Amount: 2})

	txs := repo.ListTransactions()
	for i, tx := range txs {
		if tx.ID != i+1 {
			t.Errorf("txs[%d].ID = %d, want %d", i, tx.ID, i+1)
		}
	}
}
