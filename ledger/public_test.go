package ledger_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AndreyQuantum/golang-study-ms/ledger"
)

var svcDate = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

var _ ledger.Service = ledger.NewInMemory()

func svcTransactions(t *testing.T, svc ledger.Service) []ledger.Transaction {
	t.Helper()
	got, err := svc.ListTransactions(context.Background())
	require.NoError(t, err)
	return got
}

func svcBudgets(t *testing.T, svc ledger.Service) []ledger.Budget {
	t.Helper()
	got, err := svc.ListBudgets(context.Background())
	require.NoError(t, err)
	return got
}

func TestServiceBudgetFlow(t *testing.T) {
	ctx := context.Background()

	t.Run("exceeded", func(t *testing.T) {
		svc := ledger.NewInMemory()
		require.NoError(t, svc.SetBudget(ctx, ledger.Budget{Category: "еда", Limit: 5000}))
		_, err := svc.AddTransaction(ctx, ledger.Transaction{Amount: 3000, Category: "еда", Date: svcDate})
		require.NoError(t, err)

		_, err = svc.AddTransaction(ctx, ledger.Transaction{Amount: 2500, Category: "еда", Date: svcDate})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ledger.ErrBudgetExceeded))
		assert.Len(t, svcTransactions(t, svc), 1)
	})

	t.Run("exactly at limit", func(t *testing.T) {
		svc := ledger.NewInMemory()
		require.NoError(t, svc.SetBudget(ctx, ledger.Budget{Category: "еда", Limit: 5000}))
		_, err := svc.AddTransaction(ctx, ledger.Transaction{Amount: 5000, Category: "еда", Date: svcDate})
		assert.NoError(t, err)
		_, err = svc.AddTransaction(ctx, ledger.Transaction{Amount: 0.01, Category: "еда", Date: svcDate})
		assert.True(t, errors.Is(err, ledger.ErrBudgetExceeded))
	})

	t.Run("category without budget", func(t *testing.T) {
		svc := ledger.NewInMemory()
		_, err := svc.AddTransaction(ctx, ledger.Transaction{Amount: 1e6, Category: "other", Date: svcDate})
		assert.NoError(t, err)
	})

	t.Run("invalid transaction not added", func(t *testing.T) {
		svc := ledger.NewInMemory()
		_, err := svc.AddTransaction(ctx, ledger.Transaction{Amount: -1, Category: "еда", Date: svcDate})
		var ve *ledger.ValidationError
		assert.True(t, errors.As(err, &ve))
		assert.Empty(t, svcTransactions(t, svc))
	})

	t.Run("invalid budget not saved", func(t *testing.T) {
		svc := ledger.NewInMemory()
		err := svc.SetBudget(ctx, ledger.Budget{Category: "еда", Limit: 0})
		var ve *ledger.ValidationError
		assert.True(t, errors.As(err, &ve))
		assert.Empty(t, svcBudgets(t, svc))
	})

	t.Run("raise limit", func(t *testing.T) {
		svc := ledger.NewInMemory()
		require.NoError(t, svc.SetBudget(ctx, ledger.Budget{Category: "еда", Limit: 100}))
		_, err := svc.AddTransaction(ctx, ledger.Transaction{Amount: 100, Category: "еда", Date: svcDate})
		require.NoError(t, err)
		_, err = svc.AddTransaction(ctx, ledger.Transaction{Amount: 50, Category: "еда", Date: svcDate})
		require.True(t, errors.Is(err, ledger.ErrBudgetExceeded))
		require.NoError(t, svc.SetBudget(ctx, ledger.Budget{Category: "еда", Limit: 200}))
		_, err = svc.AddTransaction(ctx, ledger.Transaction{Amount: 50, Category: "еда", Date: svcDate})
		assert.NoError(t, err)
		assert.Len(t, svcBudgets(t, svc), 1)
	})

	t.Run("budgets sorted", func(t *testing.T) {
		svc := ledger.NewInMemory()
		for _, c := range []string{"яблоки", "авто", "быт"} {
			require.NoError(t, svc.SetBudget(ctx, ledger.Budget{Category: c, Limit: 1}))
		}
		got := svcBudgets(t, svc)
		require.Len(t, got, 3)
		assert.Equal(t, []string{"авто", "быт", "яблоки"}, []string{got[0].Category, got[1].Category, got[2].Category})
	})

	t.Run("sequential ids", func(t *testing.T) {
		svc := ledger.NewInMemory()
		for want := 1; want <= 3; want++ {
			saved, err := svc.AddTransaction(ctx, ledger.Transaction{Amount: 1, Category: "x", Date: svcDate})
			require.NoError(t, err)
			assert.Equal(t, want, saved.ID)
		}
	})

	t.Run("services are independent", func(t *testing.T) {
		a, b := ledger.NewInMemory(), ledger.NewInMemory()
		require.NoError(t, a.SetBudget(ctx, ledger.Budget{Category: "еда", Limit: 10}))
		assert.Empty(t, svcBudgets(t, b))
	})
}

func TestServiceConcurrentAdd(t *testing.T) {
	svc := ledger.NewInMemory()
	ctx := context.Background()

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.AddTransaction(ctx, ledger.Transaction{Amount: 1, Category: "conc", Date: svcDate})
			_, _ = svc.ListTransactions(ctx)
		}()
	}
	wg.Wait()
	assert.Len(t, svcTransactions(t, svc), 50)
}

func TestServiceReport(t *testing.T) {
	svc := ledger.NewInMemory()
	ctx := context.Background()
	_, err := svc.AddTransaction(ctx, ledger.Transaction{Amount: 10, Category: "food", Date: svcDate})
	require.NoError(t, err)

	got, err := svc.GetReportSummary(ctx, svcDate, svcDate)
	require.NoError(t, err)
	assert.Equal(t, ledger.ReportSummary{
		From: "2026-10-01", To: "2026-10-01", Total: 10,
		Categories: []ledger.CategoryTotal{{Category: "food", Total: 10}},
	}, got)

	_, err = svc.GetReportSummary(ctx, time.Time{}, svcDate)
	var ve *ledger.ValidationError
	assert.True(t, errors.As(err, &ve))
}

func TestNewWithBadDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x:y@127.0.0.1:1/z?sslmode=disable")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	svc, closeFn, err := ledger.New(ctx)
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Nil(t, closeFn)
}

func TestPublicCheckValid(t *testing.T) {
	assert.NoError(t, ledger.CheckValid(ledger.Transaction{Amount: 1, Category: "a", Date: svcDate}))
	assert.NoError(t, ledger.CheckValid(ledger.Budget{Category: "a", Limit: 1}))
	assert.Error(t, ledger.CheckValid(ledger.Budget{Category: "a", Limit: 0}))
}

func TestServiceImport(t *testing.T) {
	ctx := context.Background()
	svc := ledger.NewInMemory()
	require.NoError(t, svc.SetBudget(ctx, ledger.Budget{Category: "еда", Limit: 25}))

	txs := make([]ledger.Transaction, 5)
	for i := range txs {
		txs[i] = ledger.Transaction{Amount: 10, Category: "еда", Date: svcDate}
	}
	var summary ledger.ImportSummary
	summary, err := svc.ImportTransactions(ctx, txs, ledger.DefaultImportWorkers)
	require.NoError(t, err)
	assert.Equal(t, 5, summary.Accepted+summary.Rejected)
	assert.Equal(t, 2, summary.Accepted)
	for _, e := range summary.Errors {
		var ie ledger.ImportError = e
		assert.True(t, errors.Is(ie, ledger.ErrBudgetExceeded))
	}
	assert.Len(t, svcTransactions(t, svc), 2)

	_, err = svc.ImportTransactions(ctx, txs, ledger.MaxImportWorkers+1)
	var ve *ledger.ValidationError
	assert.True(t, errors.As(err, &ve))
	_, err = svc.ImportTransactions(ctx, make([]ledger.Transaction, ledger.MaxImportItems+1), 1)
	assert.True(t, errors.As(err, &ve))
}
