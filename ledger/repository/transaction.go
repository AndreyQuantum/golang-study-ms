package repository

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"

	"github.com/AndreyQuantum/golang-study-ms/ledger/entity"
)

var ErrBudgetExceeded = errors.New("budget exceeded")

type TransactionRepository struct {
	mu           sync.RWMutex
	transactions []entity.Transaction
	budgets      *BudgetRepository
}

func NewTransactionRepository(budgets *BudgetRepository) *TransactionRepository {
	return &TransactionRepository{
		transactions: []entity.Transaction{},
		budgets:      budgets,
	}
}

func (r *TransactionRepository) AddTransaction(t entity.Transaction) (entity.Transaction, error) {
	if err := t.Validate(); err != nil {
		return entity.Transaction{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.budgets.GetBudget(t.Category); ok {
		spent := r.totalByCategory(t.Category)
		if exceeds(spent+t.Amount, b.Limit) {
			return entity.Transaction{}, fmt.Errorf("%w: category %q, limit %.2f, spent %.2f, new %.2f",
				ErrBudgetExceeded, t.Category, b.Limit, spent, t.Amount)
		}
	}

	t.ID = len(r.transactions) + 1
	r.transactions = append(r.transactions, t)
	return t, nil
}

func (r *TransactionRepository) ListTransactions() []entity.Transaction {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.transactions)
}

func (r *TransactionRepository) totalByCategory(category string) float64 {
	var total float64
	for _, t := range r.transactions {
		if t.Category == category {
			total += t.Amount
		}
	}
	return total
}

func toCents(x float64) int64 {
	return int64(math.Round(x * 100))
}

func exceeds(total, limit float64) bool {
	const maxSafe = 9e16
	if total >= maxSafe || limit >= maxSafe {
		return total > limit
	}
	return toCents(total) > toCents(limit)
}
