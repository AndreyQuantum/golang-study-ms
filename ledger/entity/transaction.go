package entity

import (
	"math"
	"strings"
	"time"
)

type Transaction struct {
	ID          int
	Amount      float64
	Category    string
	Description string
	Date        time.Time
}

var _ Validatable = Transaction{}

func (t Transaction) Validate() error {
	if math.IsNaN(t.Amount) || math.IsInf(t.Amount, 0) || t.Amount <= 0 {
		return &ValidationError{Field: "amount", Message: "amount must be positive"}
	}
	if t.Amount < MinMoney || t.Amount > MaxMoney {
		return &ValidationError{Field: "amount", Message: "amount must be between 0.01 and 999999999999.99"}
	}
	if strings.TrimSpace(t.Category) == "" {
		return &ValidationError{Field: "category", Message: "category must not be empty"}
	}
	if t.Date.IsZero() {
		return &ValidationError{Field: "date", Message: "date must be set"}
	}
	return nil
}
