package ledger_test

import (
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AndreyQuantum/golang-study-ms/ledger"
)

var testDate = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func TestTransactionValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		tx      ledger.Transaction
		wantMsg string
	}{
		{"valid", ledger.Transaction{Amount: 10, Category: "food", Date: testDate}, ""},
		{"zero amount", ledger.Transaction{Amount: 0, Category: "food", Date: testDate}, "amount must be positive"},
		{"negative amount", ledger.Transaction{Amount: -5, Category: "food", Date: testDate}, "amount must be positive"},
		{"nan amount", ledger.Transaction{Amount: math.NaN(), Category: "food", Date: testDate}, "amount must be positive"},
		{"empty category", ledger.Transaction{Amount: 10, Category: "", Date: testDate}, "category must not be empty"},
		{"blank category", ledger.Transaction{Amount: 10, Category: "   ", Date: testDate}, "category must not be empty"},
		{"zero date", ledger.Transaction{Amount: 10, Category: "food"}, "date must be set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.tx.Validate()
			if tt.wantMsg == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			var ve *ledger.ValidationError
			require.True(t, errors.As(err, &ve))
			assert.Equal(t, tt.wantMsg, ve.Message)
			assert.Equal(t, tt.wantMsg, err.Error())
		})
	}
}

func TestBudgetValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		budget  ledger.Budget
		wantMsg string
	}{
		{"valid", ledger.Budget{Category: "food", Limit: 100}, ""},
		{"zero limit", ledger.Budget{Category: "food", Limit: 0}, "limit must be positive"},
		{"negative limit", ledger.Budget{Category: "food", Limit: -1}, "limit must be positive"},
		{"empty category", ledger.Budget{Category: "", Limit: 100}, "category must not be empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.budget.Validate()
			if tt.wantMsg == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			var ve *ledger.ValidationError
			require.True(t, errors.As(err, &ve))
			assert.Equal(t, tt.wantMsg, ve.Message)
		})
	}
}

func TestCheckValid(t *testing.T) {
	t.Parallel()
	items := []struct {
		name  string
		v     ledger.Validatable
		valid bool
	}{
		{"valid transaction", ledger.Transaction{Amount: 1, Category: "a", Date: testDate}, true},
		{"invalid transaction", ledger.Transaction{Amount: -1, Category: "a", Date: testDate}, false},
		{"valid budget", ledger.Budget{Category: "a", Limit: 1}, true},
		{"invalid budget", ledger.Budget{Category: "a", Limit: 0}, false},
	}
	for _, it := range items {
		t.Run(it.name, func(t *testing.T) {
			t.Parallel()
			err := ledger.CheckValid(it.v)
			if it.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestBudgetFlow(t *testing.T) {
	ledger.Reset()
	t.Cleanup(ledger.Reset)

	require.NoError(t, ledger.SetBudget(ledger.Budget{Category: "еда", Limit: 5000}))

	_, err := ledger.AddTransaction(ledger.Transaction{Amount: 3000, Category: "еда", Date: testDate})
	require.NoError(t, err)
	before := len(ledger.ListTransactions())

	_, err = ledger.AddTransaction(ledger.Transaction{Amount: 2500, Category: "еда", Date: testDate})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ledger.ErrBudgetExceeded))
	assert.Len(t, ledger.ListTransactions(), before)

	t.Run("exactly at limit", func(t *testing.T) {
		ledger.Reset()
		require.NoError(t, ledger.SetBudget(ledger.Budget{Category: "еда", Limit: 5000}))
		_, err := ledger.AddTransaction(ledger.Transaction{Amount: 5000, Category: "еда", Date: testDate})
		assert.NoError(t, err)
		_, err = ledger.AddTransaction(ledger.Transaction{Amount: 0.01, Category: "еда", Date: testDate})
		assert.True(t, errors.Is(err, ledger.ErrBudgetExceeded))
	})

	t.Run("category without budget", func(t *testing.T) {
		ledger.Reset()
		_, err := ledger.AddTransaction(ledger.Transaction{Amount: 1e6, Category: "other", Date: testDate})
		assert.NoError(t, err)
	})

	t.Run("invalid transaction not added", func(t *testing.T) {
		ledger.Reset()
		_, err := ledger.AddTransaction(ledger.Transaction{Amount: -1, Category: "еда", Date: testDate})
		var ve *ledger.ValidationError
		assert.True(t, errors.As(err, &ve))
		assert.Empty(t, ledger.ListTransactions())
	})

	t.Run("invalid budget not saved", func(t *testing.T) {
		ledger.Reset()
		err := ledger.SetBudget(ledger.Budget{Category: "еда", Limit: 0})
		var ve *ledger.ValidationError
		assert.True(t, errors.As(err, &ve))
		assert.Empty(t, ledger.ListBudgets())
	})

	t.Run("raise limit", func(t *testing.T) {
		ledger.Reset()
		require.NoError(t, ledger.SetBudget(ledger.Budget{Category: "еда", Limit: 100}))
		_, err := ledger.AddTransaction(ledger.Transaction{Amount: 100, Category: "еда", Date: testDate})
		require.NoError(t, err)
		_, err = ledger.AddTransaction(ledger.Transaction{Amount: 50, Category: "еда", Date: testDate})
		require.True(t, errors.Is(err, ledger.ErrBudgetExceeded))
		require.NoError(t, ledger.SetBudget(ledger.Budget{Category: "еда", Limit: 200}))
		_, err = ledger.AddTransaction(ledger.Transaction{Amount: 50, Category: "еда", Date: testDate})
		assert.NoError(t, err)
		assert.Len(t, ledger.ListBudgets(), 1)
	})

	t.Run("budgets sorted", func(t *testing.T) {
		ledger.Reset()
		for _, c := range []string{"яблоки", "авто", "быт"} {
			require.NoError(t, ledger.SetBudget(ledger.Budget{Category: c, Limit: 1}))
		}
		got := ledger.ListBudgets()
		require.Len(t, got, 3)
		assert.Equal(t, []string{"авто", "быт", "яблоки"}, []string{got[0].Category, got[1].Category, got[2].Category})
	})

	t.Run("reset clears all", func(t *testing.T) {
		ledger.Reset()
		require.NoError(t, ledger.SetBudget(ledger.Budget{Category: "еда", Limit: 10}))
		_, err := ledger.AddTransaction(ledger.Transaction{Amount: 1, Category: "еда", Date: testDate})
		require.NoError(t, err)
		ledger.Reset()
		assert.Empty(t, ledger.ListBudgets())
		assert.Empty(t, ledger.ListTransactions())
	})

	t.Run("sequential ids", func(t *testing.T) {
		ledger.Reset()
		for want := 1; want <= 3; want++ {
			saved, err := ledger.AddTransaction(ledger.Transaction{Amount: 1, Category: "x", Date: testDate})
			require.NoError(t, err)
			assert.Equal(t, want, saved.ID)
		}
	})
}

func TestConcurrentAdd(t *testing.T) {
	ledger.Reset()
	t.Cleanup(ledger.Reset)

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = ledger.AddTransaction(ledger.Transaction{Amount: 1, Category: "conc", Date: testDate})
			_ = ledger.ListTransactions()
		}()
	}
	wg.Wait()
	assert.Len(t, ledger.ListTransactions(), 50)
}
