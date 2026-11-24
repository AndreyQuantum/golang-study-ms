package domain

import "errors"

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

var (
	ErrBudgetExceeded = errors.New("budget exceeded")
	ErrNotFound       = errors.New("not found")
)
