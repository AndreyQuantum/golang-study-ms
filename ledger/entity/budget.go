package entity

import (
	"math"
	"strings"
)

type Budget struct {
	Category string  `json:"category"`
	Limit    float64 `json:"limit"`
}

var _ Validatable = Budget{}

func (b Budget) Validate() error {
	if math.IsNaN(b.Limit) || math.IsInf(b.Limit, 0) || b.Limit <= 0 {
		return &ValidationError{Field: "limit", Message: "limit must be positive"}
	}
	if strings.TrimSpace(b.Category) == "" {
		return &ValidationError{Field: "category", Message: "category must not be empty"}
	}
	return nil
}
