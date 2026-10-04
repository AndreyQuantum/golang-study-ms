package repository

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/AndreyQuantum/golang-study-ms/ledger/entity"
)

type BudgetRepository struct {
	budgets map[string]entity.Budget
}

func NewBudgetRepository() *BudgetRepository {
	return &BudgetRepository{
		budgets: map[string]entity.Budget{},
	}
}
func (r *BudgetRepository) SetBudget(b entity.Budget) {
	r.budgets[b.Category] = b
}

func (r *BudgetRepository) GetBudget(category string) (entity.Budget, bool) {
	b, ok := r.budgets[category]
	return b, ok
}

func (r *BudgetRepository) ListBudgets() []entity.Budget {
	result := make([]entity.Budget, 0, len(r.budgets))
	for _, category := range slices.Sorted(maps.Keys(r.budgets)) {
		result = append(result, r.budgets[category])
	}
	return result
}

func (r *BudgetRepository) LoadBudgets(rd io.Reader) error {
	dec := json.NewDecoder(rd)
	dec.DisallowUnknownFields()

	var budgets []entity.Budget
	if err := dec.Decode(&budgets); err != nil {
		return fmt.Errorf("parse budgets json: %w", err)
	}

	for i, b := range budgets {
		if b.Category == "" {
			return fmt.Errorf("budget #%d: empty category", i)
		}
		if b.Limit < 0 {
			return fmt.Errorf("budget #%d (%q): negative limit %.2f", i, b.Category, b.Limit)
		}
	}

	for _, b := range budgets {
		r.SetBudget(b)
	}
	return nil
}
