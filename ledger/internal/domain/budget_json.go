package domain

import (
	"encoding/json"
	"fmt"
	"io"
)

// ParseBudgets читает список бюджетов из JSON и проверяет каждый из них.
func ParseBudgets(rd io.Reader) ([]Budget, error) {
	dec := json.NewDecoder(rd)
	dec.DisallowUnknownFields()

	var budgets []Budget
	if err := dec.Decode(&budgets); err != nil {
		return nil, fmt.Errorf("parse budgets json: %w", err)
	}
	for i, b := range budgets {
		if err := b.Validate(); err != nil {
			return nil, fmt.Errorf("budget #%d: %w", i, err)
		}
	}
	return budgets, nil
}
