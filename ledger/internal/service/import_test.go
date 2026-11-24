package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
)

func importTxs(n int, category string, amount float64) []domain.Transaction {
	txs := make([]domain.Transaction, n)
	for i := range txs {
		txs[i] = domain.Transaction{Amount: amount, Category: category, Date: day(10, 1)}
	}
	return txs
}

func TestImportArgumentValidation(t *testing.T) {
	cases := []struct {
		name    string
		txs     []domain.Transaction
		workers int
		field   string
		message string
	}{
		{"zero workers", importTxs(1, "food", 1), 0, "workers", "workers must be an integer between 1 and 32"},
		{"negative workers", importTxs(1, "food", 1), -1, "workers", "workers must be an integer between 1 and 32"},
		{"too many workers", importTxs(1, "food", 1), 33, "workers", "workers must be an integer between 1 and 32"},
		{"too many items", importTxs(1001, "food", 1), 4, "transactions", "too many transactions: at most 1000 per request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sp := newSpy()
			summary, err := sp.service().ImportTransactions(context.Background(), tc.txs, tc.workers)
			var ve *domain.ValidationError
			require.ErrorAs(t, err, &ve)
			assert.Equal(t, tc.field, ve.Field)
			assert.Equal(t, tc.message, ve.Message)
			assert.Empty(t, sp.names())
			assert.Zero(t, summary.Accepted+summary.Rejected)
			assert.NotNil(t, summary.Errors)
		})
	}
}

func TestImportEmpty(t *testing.T) {
	svc := newService()
	for _, txs := range [][]domain.Transaction{nil, {}} {
		summary, err := svc.ImportTransactions(context.Background(), txs, 4)
		require.NoError(t, err)
		assert.Equal(t, domain.ImportSummary{Errors: []domain.ImportError{}}, summary)
	}
}

func TestImportCanceledBeforeStart(t *testing.T) {
	sp := newSpy()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	summary, err := sp.service().ImportTransactions(ctx, importTxs(5, "food", 1), 4)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
	assert.Zero(t, summary.Accepted+summary.Rejected)
	assert.NotNil(t, summary.Errors)
	assert.Empty(t, sp.names())
}

func TestImportAcceptsAll(t *testing.T) {
	for _, workers := range []int{1, 4, 32} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			svc := newService()
			txs := importTxs(50, "food", 10.005)
			for i := range txs {
				txs[i].Date = time.Date(2026, 10, 1, 15, 30, 0, 0, time.UTC)
			}
			summary, err := svc.ImportTransactions(context.Background(), txs, workers)
			require.NoError(t, err)
			assert.Equal(t, 50, summary.Accepted)
			assert.Zero(t, summary.Rejected)
			assert.NotNil(t, summary.Errors)
			assert.Empty(t, summary.Errors)

			stored, err := svc.ListTransactions(context.Background())
			require.NoError(t, err)
			require.Len(t, stored, 50)
			for _, s := range stored {
				assert.Equal(t, 10.01, s.Amount, "сумма округляется до копеек")
				assert.Equal(t, day(10, 1), s.Date, "дата усечена до дня UTC")
			}
		})
	}
}

func TestImportMoreWorkersThanItems(t *testing.T) {
	svc := newService()
	summary, err := svc.ImportTransactions(context.Background(), importTxs(3, "food", 5), 32)
	require.NoError(t, err)
	assert.Equal(t, 3, summary.Accepted)
	stored, _ := svc.ListTransactions(context.Background())
	assert.Len(t, stored, 3)
}

func TestImportSingleWorkerKeepsOrder(t *testing.T) {
	svc := newService()
	txs := importTxs(10, "food", 1)
	for i := range txs {
		txs[i].Description = fmt.Sprintf("item-%d", i)
	}
	_, err := svc.ImportTransactions(context.Background(), txs, 1)
	require.NoError(t, err)
	stored, _ := svc.ListTransactions(context.Background())
	require.Len(t, stored, 10)
	for i, s := range stored {
		assert.Equal(t, i+1, s.ID)
		assert.Equal(t, fmt.Sprintf("item-%d", i), s.Description)
	}
}
