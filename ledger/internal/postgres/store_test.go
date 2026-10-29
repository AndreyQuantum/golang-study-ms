package postgres

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AndreyQuantum/golang-study-ms/ledger/entity"
	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/db"
	"github.com/AndreyQuantum/golang-study-ms/ledger/repository"
)

func day(m time.Month, d int) time.Time {
	return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC)
}

func newStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("LEDGER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("LEDGER_TEST_DATABASE_URL is not set")
	}
	pool, err := db.Open(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	_, err = pool.Exec(`TRUNCATE budgets, expenses RESTART IDENTITY`)
	require.NoError(t, err)
	return New(pool), pool
}

func TestSetBudgetUpsert(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	require.NoError(t, s.SetBudget(ctx, entity.Budget{Category: "food", Limit: 5000}))
	require.NoError(t, s.SetBudget(ctx, entity.Budget{Category: "food", Limit: 7000.5}))

	got, err := s.ListBudgets(ctx)
	require.NoError(t, err)
	assert.Equal(t, []entity.Budget{{Category: "food", Limit: 7000.5}}, got)
}

func TestListBudgetsSortedAndEmpty(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	got, err := s.ListBudgets(ctx)
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)

	for _, c := range []string{"transport", "food", "health"} {
		require.NoError(t, s.SetBudget(ctx, entity.Budget{Category: c, Limit: 10}))
	}
	got, err = s.ListBudgets(ctx)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, []string{"food", "health", "transport"}, []string{got[0].Category, got[1].Category, got[2].Category})
}

func TestAddTransactionWithinAndExceedingLimit(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	require.NoError(t, s.SetBudget(ctx, entity.Budget{Category: "food", Limit: 5000}))

	first, err := s.AddTransaction(ctx, entity.Transaction{Amount: 3000, Category: "food", Description: "a", Date: day(10, 1)})
	require.NoError(t, err)
	assert.Equal(t, 1, first.ID)

	_, err = s.AddTransaction(ctx, entity.Transaction{Amount: 2000.01, Category: "food", Date: day(10, 2)})
	require.Error(t, err)
	assert.ErrorIs(t, err, repository.ErrBudgetExceeded)

	list, err := s.ListTransactions(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	exact, err := s.AddTransaction(ctx, entity.Transaction{Amount: 2000, Category: "food", Date: day(10, 2)})
	require.NoError(t, err)
	assert.Equal(t, 2, exact.ID)

	_, err = s.AddTransaction(ctx, entity.Transaction{Amount: 0.01, Category: "food", Date: day(10, 3)})
	assert.ErrorIs(t, err, repository.ErrBudgetExceeded)
}

func TestAddTransactionFloatNoise(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	require.NoError(t, s.SetBudget(ctx, entity.Budget{Category: "food", Limit: 0.3}))

	_, err := s.AddTransaction(ctx, entity.Transaction{Amount: 0.1, Category: "food", Date: day(10, 1)})
	require.NoError(t, err)
	_, err = s.AddTransaction(ctx, entity.Transaction{Amount: 0.2, Category: "food", Date: day(10, 1)})
	assert.NoError(t, err)
}

func TestAddTransactionWithoutBudget(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	saved, err := s.AddTransaction(ctx, entity.Transaction{Amount: 1e6, Category: "health", Date: day(10, 1)})
	require.NoError(t, err)
	assert.Equal(t, 1, saved.ID)
}

func TestListTransactionsOrder(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	for _, tx := range []entity.Transaction{
		{Amount: 1, Category: "a", Description: "first", Date: day(10, 1)},
		{Amount: 2, Category: "a", Date: day(10, 3)},
		{Amount: 3, Category: "b", Description: "third", Date: day(10, 3)},
		{Amount: 4, Category: "b", Date: day(10, 2)},
	} {
		_, err := s.AddTransaction(ctx, tx)
		require.NoError(t, err)
	}

	got, err := s.ListTransactions(ctx)
	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.Equal(t, []int{3, 2, 4, 1}, []int{got[0].ID, got[1].ID, got[2].ID, got[3].ID})
	assert.Equal(t, "third", got[0].Description)
	assert.Equal(t, "", got[1].Description)
	assert.Equal(t, day(10, 3), got[0].Date)
	assert.Equal(t, time.UTC, got[0].Date.Location())
	assert.Equal(t, 3.0, got[0].Amount)
}

func TestReportSummaryInterval(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	for _, tx := range []entity.Transaction{
		{Amount: 100.10, Category: "food", Date: day(10, 1)},
		{Amount: 50.20, Category: "food", Date: day(10, 10)},
		{Amount: 30, Category: "transport", Date: day(10, 5)},
		{Amount: 999, Category: "food", Date: day(9, 30)},
		{Amount: 999, Category: "health", Date: day(10, 11)},
	} {
		_, err := s.AddTransaction(ctx, tx)
		require.NoError(t, err)
	}

	got, err := s.ReportSummary(ctx, day(10, 1), day(10, 10))
	require.NoError(t, err)
	assert.Equal(t, entity.ReportSummary{
		From:  "2026-10-01",
		To:    "2026-10-10",
		Total: 180.30,
		Categories: []entity.CategoryTotal{
			{Category: "food", Total: 150.30},
			{Category: "transport", Total: 30},
		},
	}, got)
}

func TestReportSummaryEmpty(t *testing.T) {
	s, _ := newStore(t)
	got, err := s.ReportSummary(context.Background(), day(10, 1), day(10, 31))
	require.NoError(t, err)
	assert.Equal(t, 0.0, got.Total)
	assert.NotNil(t, got.Categories)
	assert.Empty(t, got.Categories)
}

func TestAddTransactionConcurrentRespectsLimit(t *testing.T) {
	s, pool := newStore(t)
	ctx := context.Background()
	require.NoError(t, s.SetBudget(ctx, entity.Budget{Category: "food", Limit: 100}))

	const workers = 10
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.AddTransaction(ctx, entity.Transaction{Amount: 30, Category: "food", Date: day(10, 1)})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	ok := 0
	for err := range errs {
		if err == nil {
			ok++
			continue
		}
		assert.ErrorIs(t, err, repository.ErrBudgetExceeded)
	}
	assert.Equal(t, 3, ok)

	var sum float64
	require.NoError(t, pool.QueryRow(`SELECT COALESCE(SUM(amount), 0)::float8 FROM expenses`).Scan(&sum))
	assert.LessOrEqual(t, sum, 100.0)
	assert.Equal(t, 90.0, sum)
}
