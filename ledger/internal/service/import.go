package service

import (
	"context"
	"fmt"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
)

// ImportTransactions добавляет пакет транзакций. См. подробности конвейера ниже.
func (s *service) ImportTransactions(ctx context.Context, txs []domain.Transaction, workers int) (domain.ImportSummary, error) {
	empty := domain.ImportSummary{Errors: []domain.ImportError{}}
	if workers < 1 || workers > domain.MaxImportWorkers {
		return empty, &domain.ValidationError{Field: "workers",
			Message: fmt.Sprintf("workers must be an integer between 1 and %d", domain.MaxImportWorkers)}
	}
	if len(txs) > domain.MaxImportItems {
		return empty, &domain.ValidationError{Field: "transactions",
			Message: fmt.Sprintf("too many transactions: at most %d per request", domain.MaxImportItems)}
	}
	if len(txs) == 0 {
		return empty, nil
	}
	if err := ctx.Err(); err != nil {
		return empty, fmt.Errorf("import not started: %w", err)
	}
	return empty, nil
}
