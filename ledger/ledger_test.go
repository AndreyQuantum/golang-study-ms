package ledger_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger"
)

func TestFacadeBudgetFlow(t *testing.T) {
	date := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := ledger.SetBudget(ledger.Budget{Category: "facade-food", Limit: 1000}); err != nil {
		t.Fatalf("SetBudget: %v", err)
	}

	saved, err := ledger.AddTransaction(ledger.Transaction{Amount: 600, Category: "facade-food", Date: date})
	if err != nil || saved.ID == 0 {
		t.Fatalf("AddTransaction: id=%d err=%v", saved.ID, err)
	}

	_, err = ledger.AddTransaction(ledger.Transaction{Amount: 600, Category: "facade-food", Date: date})
	if !errors.Is(err, ledger.ErrBudgetExceeded) {
		t.Fatalf("err = %v, want ErrBudgetExceeded", err)
	}

	var ve *ledger.ValidationError
	_, err = ledger.AddTransaction(ledger.Transaction{Amount: -1, Category: "facade-food", Date: date})
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want ValidationError", err)
	}

	if len(ledger.ListBudgets()) == 0 {
		t.Fatal("ListBudgets is empty")
	}
}

func TestFacadeConcurrent(t *testing.T) {
	date := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	before := len(ledger.ListTransactions())

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = ledger.AddTransaction(ledger.Transaction{Amount: 1, Category: "facade-conc", Date: date})
			_ = ledger.ListTransactions()
		}()
	}
	wg.Wait()

	if got := len(ledger.ListTransactions()) - before; got != 50 {
		t.Fatalf("added = %d, want 50", got)
	}
}
