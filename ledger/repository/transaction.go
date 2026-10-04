package repository

import (
	"errors"
	"fmt"

	"github.com/AndreyQuantum/golang-study-ms/ledger/entity"
)

var ErrBudgetExceeded = errors.New("budget exceeded")

type TransactionRepository struct {
	transactions []entity.Transaction
	budgets      *BudgetRepository
}

func NewTransactionRepository(budgets *BudgetRepository) *TransactionRepository {
	return &TransactionRepository{
		transactions: []entity.Transaction{},
		budgets:      budgets,
	}
}

func (r *TransactionRepository) AddTransaction(t entity.Transaction) error {
	if b, ok := r.budgets.GetBudget(t.Category); ok {
		spent := r.totalByCategory(t.Category)
		if spent+t.Amount > b.Limit {
			return fmt.Errorf("%w: category %q, limit %.2f, spent %.2f, new %.2f",
				ErrBudgetExceeded, t.Category, b.Limit, spent, t.Amount)
		}
	}

	t.ID = len(r.transactions) + 1
	r.transactions = append(r.transactions, t)
	return nil
}

func (r *TransactionRepository) ListTransactions() []entity.Transaction {
	return r.transactions
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
