package ledger

import (
	"sync"

	"github.com/AndreyQuantum/golang-study-ms/ledger/entity"
	"github.com/AndreyQuantum/golang-study-ms/ledger/repository"
)

type (
	Transaction     = entity.Transaction
	Budget          = entity.Budget
	Validatable     = entity.Validatable
	ValidationError = entity.ValidationError
)

var ErrBudgetExceeded = repository.ErrBudgetExceeded

var (
	mu           sync.RWMutex
	budgetRepo   = repository.NewBudgetRepository()
	transactions = repository.NewTransactionRepository(budgetRepo)
)

func AddTransaction(tx Transaction) (Transaction, error) {
	mu.RLock()
	defer mu.RUnlock()
	return transactions.AddTransaction(tx)
}

func ListTransactions() []Transaction {
	mu.RLock()
	defer mu.RUnlock()
	return transactions.ListTransactions()
}

func SetBudget(b Budget) error {
	mu.RLock()
	defer mu.RUnlock()
	return budgetRepo.SetBudget(b)
}

func ListBudgets() []Budget {
	mu.RLock()
	defer mu.RUnlock()
	return budgetRepo.ListBudgets()
}

func CheckValid(v Validatable) error {
	return entity.CheckValid(v)
}
