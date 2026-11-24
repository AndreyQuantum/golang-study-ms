// Package repotest содержит общий контракт-тест для реализаций domain.Repos и domain.TxManager.
// Используется только из _test.go файлов пакетов memory и pg.
package repotest

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
)

// Factory создаёт чистое хранилище для одного теста.
type Factory func(t *testing.T) (domain.Repos, domain.TxManager)

func day(m time.Month, d int) time.Time {
	return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC)
}

func canceled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func RunBudgetSuite(t *testing.T, newRepos Factory) {
	t.Run("upsert adds and updates", func(t *testing.T) {
		repos, _ := newRepos(t)
		ctx := context.Background()
		require.NoError(t, repos.Budgets.Upsert(ctx, domain.Budget{Category: "food", Limit: 5000}))
		require.NoError(t, repos.Budgets.Upsert(ctx, domain.Budget{Category: "food", Limit: 7000.5}))

		got, err := repos.Budgets.List(ctx)
		require.NoError(t, err)
		assert.Equal(t, []domain.Budget{{Category: "food", Limit: 7000.5}}, got)
	})

	t.Run("get by category", func(t *testing.T) {
		repos, _ := newRepos(t)
		ctx := context.Background()
		require.NoError(t, repos.Budgets.Upsert(ctx, domain.Budget{Category: "food", Limit: 100}))

		got, err := repos.Budgets.GetByCategory(ctx, "food")
		require.NoError(t, err)
		assert.Equal(t, domain.Budget{Category: "food", Limit: 100}, got)

		_, err = repos.Budgets.GetByCategory(ctx, "nope")
		assert.ErrorIs(t, err, domain.ErrNotFound)
	})

	t.Run("list sorted and not nil", func(t *testing.T) {
		repos, _ := newRepos(t)
		ctx := context.Background()
		got, err := repos.Budgets.List(ctx)
		require.NoError(t, err)
		assert.NotNil(t, got)
		assert.Empty(t, got)

		for _, c := range []string{"transport", "food", "health"} {
			require.NoError(t, repos.Budgets.Upsert(ctx, domain.Budget{Category: c, Limit: 10}))
		}
		got, err = repos.Budgets.List(ctx)
		require.NoError(t, err)
		require.Len(t, got, 3)
		assert.Equal(t, []string{"food", "health", "transport"}, []string{got[0].Category, got[1].Category, got[2].Category})
	})

	t.Run("get for update inside tx", func(t *testing.T) {
		repos, tx := newRepos(t)
		ctx := context.Background()
		require.NoError(t, repos.Budgets.Upsert(ctx, domain.Budget{Category: "food", Limit: 100}))

		err := tx.WithinTx(ctx, func(ctx context.Context, r domain.Repos) error {
			b, err := r.Budgets.GetByCategoryForUpdate(ctx, "food")
			require.NoError(t, err)
			assert.Equal(t, domain.Budget{Category: "food", Limit: 100}, b)
			_, err = r.Budgets.GetByCategoryForUpdate(ctx, "nope")
			assert.ErrorIs(t, err, domain.ErrNotFound)
			return nil
		})
		require.NoError(t, err)
	})

	t.Run("canceled context", func(t *testing.T) {
		repos, _ := newRepos(t)
		ctx := canceled()
		assert.ErrorIs(t, repos.Budgets.Upsert(ctx, domain.Budget{Category: "food", Limit: 1}), context.Canceled)
		_, err := repos.Budgets.GetByCategory(ctx, "food")
		assert.ErrorIs(t, err, context.Canceled)
		_, err = repos.Budgets.List(ctx)
		assert.ErrorIs(t, err, context.Canceled)

		got, err := repos.Budgets.List(context.Background())
		require.NoError(t, err)
		assert.Empty(t, got, "отменённый Upsert не должен менять состояние")
	})
}

func RunExpenseSuite(t *testing.T, newRepos Factory) {
	t.Run("add assigns sequential ids", func(t *testing.T) {
		repos, _ := newRepos(t)
		ctx := context.Background()
		for want := 1; want <= 3; want++ {
			saved, err := repos.Expenses.Add(ctx, domain.Transaction{Amount: 1, Category: "x", Description: "d", Date: day(10, 1)})
			require.NoError(t, err)
			assert.Equal(t, want, saved.ID)
		}
		list, err := repos.Expenses.List(ctx)
		require.NoError(t, err)
		ids := []int{}
		for _, tx := range list {
			ids = append(ids, tx.ID)
		}
		sort.Ints(ids)
		assert.Equal(t, []int{1, 2, 3}, ids)
	})

	t.Run("list not nil", func(t *testing.T) {
		repos, _ := newRepos(t)
		got, err := repos.Expenses.List(context.Background())
		require.NoError(t, err)
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})

	t.Run("sum by category", func(t *testing.T) {
		repos, _ := newRepos(t)
		ctx := context.Background()
		sum, err := repos.Expenses.SumByCategory(ctx, "food")
		require.NoError(t, err)
		assert.Equal(t, domain.Money(0), sum)

		for _, tx := range []domain.Transaction{
			{Amount: 0.1, Category: "food", Date: day(10, 1)},
			{Amount: 0.2, Category: "food", Date: day(10, 1)},
			{Amount: 500, Category: "other", Date: day(10, 1)},
		} {
			_, err := repos.Expenses.Add(ctx, tx)
			require.NoError(t, err)
		}
		sum, err = repos.Expenses.SumByCategory(ctx, "food")
		require.NoError(t, err)
		assert.Equal(t, domain.Money(30), sum)
	})

	t.Run("totals by category", func(t *testing.T) {
		repos, _ := newRepos(t)
		ctx := context.Background()
		got, err := repos.Expenses.TotalsByCategory(ctx, day(10, 1), day(10, 31))
		require.NoError(t, err)
		assert.NotNil(t, got)
		assert.Empty(t, got)

		for _, tx := range []domain.Transaction{
			{Amount: 100.10, Category: "food", Date: day(10, 1)},
			{Amount: 50.20, Category: "food", Date: day(10, 10)},
			{Amount: 30, Category: "transport", Date: day(10, 5)},
			{Amount: 999, Category: "food", Date: day(9, 30)},
			{Amount: 999, Category: "health", Date: day(10, 11)},
		} {
			_, err := repos.Expenses.Add(ctx, tx)
			require.NoError(t, err)
		}
		got, err = repos.Expenses.TotalsByCategory(ctx, day(10, 1), day(10, 10))
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "food", got[0].Category)
		assert.InDelta(t, 150.30, got[0].Total, 1e-9)
		assert.Equal(t, "transport", got[1].Category)
		assert.InDelta(t, 30.0, got[1].Total, 1e-9)

		// время суток в границах интервала игнорируется: считаются дни UTC
		got, err = repos.Expenses.TotalsByCategory(ctx, day(10, 10).Add(7*time.Hour), day(10, 10).Add(20*time.Hour))
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "food", got[0].Category)
	})

	t.Run("categories distinct sorted interval", func(t *testing.T) {
		repos, _ := newRepos(t)
		ctx := context.Background()
		for _, tx := range []domain.Transaction{
			{Amount: 10, Category: "transport", Date: day(10, 10)},
			{Amount: 10, Category: "food", Date: day(10, 1)},
			{Amount: 20, Category: "food", Date: day(10, 5)},
			{Amount: 99, Category: "health", Date: day(10, 11)},
			{Amount: 99, Category: "health", Date: day(9, 30)},
		} {
			_, err := repos.Expenses.Add(ctx, tx)
			require.NoError(t, err)
		}
		// границы включительные: расходы в день from и в день to входят в отчёт
		got, err := repos.Expenses.Categories(ctx, day(10, 1), day(10, 10))
		require.NoError(t, err)
		assert.Equal(t, []string{"food", "transport"}, got)
	})

	t.Run("categories empty not nil", func(t *testing.T) {
		repos, _ := newRepos(t)
		got, err := repos.Expenses.Categories(context.Background(), day(10, 1), day(10, 31))
		require.NoError(t, err)
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})

	t.Run("sum between", func(t *testing.T) {
		repos, _ := newRepos(t)
		ctx := context.Background()
		for _, tx := range []domain.Transaction{
			{Amount: 100.10, Category: "food", Date: day(10, 1)},
			{Amount: 50.20, Category: "food", Date: day(10, 10)},
			{Amount: 999, Category: "food", Date: day(9, 30)},
			{Amount: 30, Category: "transport", Date: day(10, 5)},
		} {
			_, err := repos.Expenses.Add(ctx, tx)
			require.NoError(t, err)
		}
		sum, err := repos.Expenses.SumByCategoryBetween(ctx, "food", day(10, 1), day(10, 10))
		require.NoError(t, err)
		assert.Equal(t, domain.Money(15030), sum)

		sum, err = repos.Expenses.SumByCategoryBetween(ctx, "nope", day(10, 1), day(10, 10))
		require.NoError(t, err)
		assert.Equal(t, domain.Money(0), sum)
	})

	t.Run("sum between ignores time of day", func(t *testing.T) {
		repos, _ := newRepos(t)
		ctx := context.Background()
		late := time.Date(2026, 10, 10, 23, 30, 0, 0, time.FixedZone("MSK", 3*3600)) // 20:30 UTC того же дня
		_, err := repos.Expenses.Add(ctx, domain.Transaction{Amount: 7, Category: "food", Date: late})
		require.NoError(t, err)

		from, to := day(10, 10).Add(7*time.Hour), day(10, 10).Add(20*time.Hour)
		sum, err := repos.Expenses.SumByCategoryBetween(ctx, "food", from, to)
		require.NoError(t, err)
		assert.Equal(t, domain.Money(700), sum)

		totals, err := repos.Expenses.TotalsByCategory(ctx, from, to)
		require.NoError(t, err)
		require.Len(t, totals, 1)
		assert.Equal(t, domain.MoneyFromFloat(totals[0].Total), sum)
	})

	t.Run("sum between equals totals", func(t *testing.T) {
		repos, _ := newRepos(t)
		ctx := context.Background()
		for _, tx := range []domain.Transaction{
			{Amount: 0.1, Category: "food", Date: day(10, 1)},
			{Amount: 0.2, Category: "food", Date: day(10, 2)},
			{Amount: 12.34, Category: "transport", Date: day(10, 3)},
			{Amount: 5, Category: "health", Date: day(10, 4)},
		} {
			_, err := repos.Expenses.Add(ctx, tx)
			require.NoError(t, err)
		}
		totals, err := repos.Expenses.TotalsByCategory(ctx, day(10, 1), day(10, 31))
		require.NoError(t, err)
		require.Len(t, totals, 3)
		for _, c := range totals {
			sum, err := repos.Expenses.SumByCategoryBetween(ctx, c.Category, day(10, 1), day(10, 31))
			require.NoError(t, err)
			assert.Equal(t, domain.MoneyFromFloat(c.Total), sum, c.Category)
		}
	})

	t.Run("report queries canceled context", func(t *testing.T) {
		repos, _ := newRepos(t)
		ctx := canceled()
		_, err := repos.Expenses.Categories(ctx, day(10, 1), day(10, 2))
		assert.ErrorIs(t, err, context.Canceled)
		_, err = repos.Expenses.SumByCategoryBetween(ctx, "x", day(10, 1), day(10, 2))
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("canceled context", func(t *testing.T) {
		repos, _ := newRepos(t)
		ctx := canceled()
		_, err := repos.Expenses.Add(ctx, domain.Transaction{Amount: 1, Category: "x", Date: day(10, 1)})
		assert.ErrorIs(t, err, context.Canceled)
		_, err = repos.Expenses.List(ctx)
		assert.ErrorIs(t, err, context.Canceled)
		_, err = repos.Expenses.SumByCategory(ctx, "x")
		assert.ErrorIs(t, err, context.Canceled)
		_, err = repos.Expenses.TotalsByCategory(ctx, day(10, 1), day(10, 2))
		assert.ErrorIs(t, err, context.Canceled)

		got, err := repos.Expenses.List(context.Background())
		require.NoError(t, err)
		assert.Empty(t, got, "отменённый Add не должен менять состояние")
	})
}

func RunTxSuite(t *testing.T, newRepos Factory) {
	t.Run("repos work inside tx", func(t *testing.T) {
		_, tx := newRepos(t)
		ctx := context.Background()
		err := tx.WithinTx(ctx, func(ctx context.Context, r domain.Repos) error {
			require.NoError(t, r.Budgets.Upsert(ctx, domain.Budget{Category: "food", Limit: 10}))
			_, err := r.Expenses.Add(ctx, domain.Transaction{Amount: 2, Category: "food", Date: day(10, 1)})
			require.NoError(t, err)
			sum, err := r.Expenses.SumByCategory(ctx, "food")
			require.NoError(t, err)
			assert.Equal(t, domain.Money(200), sum)
			return nil
		})
		require.NoError(t, err)
	})

	t.Run("fn error is returned as is", func(t *testing.T) {
		_, tx := newRepos(t)
		boom := errors.New("boom")
		err := tx.WithinTx(context.Background(), func(context.Context, domain.Repos) error {
			return boom
		})
		assert.ErrorIs(t, err, boom)
	})

	t.Run("serializes lock-sum-add", func(t *testing.T) {
		repos, tx := newRepos(t)
		ctx := context.Background()
		require.NoError(t, repos.Budgets.Upsert(ctx, domain.Budget{Category: "c", Limit: 100000}))

		const workers = 50
		var (
			wg    sync.WaitGroup
			mu    sync.Mutex
			seen  = map[domain.Money]int{}
			fails []error
		)
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := tx.WithinTx(ctx, func(ctx context.Context, r domain.Repos) error {
					if _, err := r.Budgets.GetByCategoryForUpdate(ctx, "c"); err != nil {
						return err
					}
					sum, err := r.Expenses.SumByCategory(ctx, "c")
					if err != nil {
						return err
					}
					mu.Lock()
					seen[sum]++
					mu.Unlock()
					_, err = r.Expenses.Add(ctx, domain.Transaction{Amount: 1, Category: "c", Date: day(10, 1)})
					return err
				})
				if err != nil {
					mu.Lock()
					fails = append(fails, err)
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		require.Empty(t, fails)

		// каждая транзакция увидела свою сумму: потерянных обновлений нет
		assert.Len(t, seen, workers)
		for s, n := range seen {
			assert.Equal(t, 1, n, "сумма %s увидена более одного раза", s)
		}
		list, err := repos.Expenses.List(ctx)
		require.NoError(t, err)
		assert.Len(t, list, workers)
		sum, err := repos.Expenses.SumByCategory(ctx, "c")
		require.NoError(t, err)
		assert.Equal(t, domain.Money(5000), sum)
	})
}
