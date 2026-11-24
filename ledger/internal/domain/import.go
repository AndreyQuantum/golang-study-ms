package domain

import "fmt"

const (
	DefaultImportWorkers = 4    // воркеров по умолчанию
	MaxImportWorkers     = 32   // верхняя граница workers
	MaxImportItems       = 1000 // максимум транзакций в одном импорте
)

// ImportError - причина отказа по одному элементу импорта; Index - позиция в исходном массиве.
type ImportError struct {
	Index int
	Err   error
}

func (e ImportError) Error() string { return fmt.Sprintf("item %d: %v", e.Index, e.Err) }

// Unwrap нужен, чтобы работали errors.Is(ErrBudgetExceeded) и errors.As(*ValidationError).
func (e ImportError) Unwrap() error { return e.Err }

// ImportSummary - итог импорта. Rejected == len(Errors); Errors никогда не nil, отсортирован по Index.
// При отмене содержит частичный результат: необработанные элементы не входят ни в Accepted, ни в Rejected.
type ImportSummary struct {
	Accepted int
	Rejected int
	Errors   []ImportError
}
