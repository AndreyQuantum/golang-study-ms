package repository

import (
	"io"
	"maps"
	"slices"
	"sync"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
)

type BudgetRepository struct {
	mu      sync.RWMutex
	budgets map[string]domain.Budget
}

func NewBudgetRepository() *BudgetRepository {
	return &BudgetRepository{
		budgets: map[string]domain.Budget{},
	}
}

func (r *BudgetRepository) SetBudget(b domain.Budget) error {
	if err := b.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.budgets[b.Category] = b
	return nil
}

func (r *BudgetRepository) GetBudget(category string) (domain.Budget, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.budgets[category]
	return b, ok
}

func (r *BudgetRepository) ListBudgets() []domain.Budget {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.Budget, 0, len(r.budgets))
	for _, category := range slices.Sorted(maps.Keys(r.budgets)) {
		result = append(result, r.budgets[category])
	}
	return result
}

func ParseBudgets(rd io.Reader) ([]domain.Budget, error) {
	return domain.ParseBudgets(rd)
}

func (r *BudgetRepository) LoadBudgets(rd io.Reader) error {
	budgets, err := ParseBudgets(rd)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range budgets {
		r.budgets[b.Category] = b
	}
	return nil
}
