package pg

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/repository/repotest"
	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/testdb"
)

func TestMain(m *testing.M) { testdb.Main(m) }

func day(m time.Month, d int) time.Time {
	return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC)
}

func newStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	pool := testdb.Open(t)
	return New(pool), pool
}

func newRepos(t *testing.T) (domain.Repos, domain.TxManager) {
	t.Helper()
	s, _ := newStore(t)
	return s.Repos(), s
}

func TestBudgetRepository(t *testing.T)  { repotest.RunBudgetSuite(t, newRepos) }
func TestExpenseRepository(t *testing.T) { repotest.RunExpenseSuite(t, newRepos) }
func TestTxManager(t *testing.T)         { repotest.RunTxSuite(t, newRepos) }

func TestWithinTxRollbackOnError(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	err := s.WithinTx(ctx, func(ctx context.Context, r domain.Repos) error {
		_, err := r.Expenses.Add(ctx, domain.Transaction{Amount: 5, Category: "food", Date: day(10, 1)})
		require.NoError(t, err)
		return domain.ErrBudgetExceeded
	})
	assert.ErrorIs(t, err, domain.ErrBudgetExceeded)

	list, err := s.Repos().Expenses.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestGetByCategoryForUpdateBlocksSecondTx(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	require.NoError(t, s.Repos().Budgets.Upsert(ctx, domain.Budget{Category: "food", Limit: 100}))

	locked := make(chan struct{})
	released := make(chan time.Time, 1)
	done := make(chan error, 1)
	go func() {
		done <- s.WithinTx(ctx, func(ctx context.Context, r domain.Repos) error {
			if _, err := r.Budgets.GetByCategoryForUpdate(ctx, "food"); err != nil {
				return err
			}
			close(locked)
			time.Sleep(300 * time.Millisecond)
			released <- time.Now()
			return nil
		})
	}()
	<-locked

	var acquired time.Time
	err := s.WithinTx(ctx, func(ctx context.Context, r domain.Repos) error {
		_, err := r.Budgets.GetByCategoryForUpdate(ctx, "food")
		acquired = time.Now()
		return err
	})
	require.NoError(t, err)
	require.NoError(t, <-done)

	releasedAt := <-released
	assert.False(t, acquired.Before(releasedAt), "вторая транзакция получила блокировку раньше конца первой")
}

func TestSumByCategoryNumeric(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	for range 3 {
		_, err := s.Repos().Expenses.Add(ctx, domain.Transaction{Amount: 0.10, Category: "food", Date: day(10, 1)})
		require.NoError(t, err)
	}
	sum, err := s.Repos().Expenses.SumByCategory(ctx, "food")
	require.NoError(t, err)
	assert.Equal(t, domain.Money(30), sum)
}

func TestTotalsByCategoryInterval(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	for _, tx := range []domain.Transaction{
		{Amount: 100.10, Category: "food", Date: day(10, 1)},
		{Amount: 50.20, Category: "food", Date: day(10, 10)},
		{Amount: 30, Category: "transport", Date: day(10, 5)},
		{Amount: 999, Category: "food", Date: day(9, 30)},
		{Amount: 999, Category: "health", Date: day(10, 11)},
	} {
		_, err := s.Repos().Expenses.Add(ctx, tx)
		require.NoError(t, err)
	}
	got, err := s.Repos().Expenses.TotalsByCategory(ctx, day(10, 1), day(10, 10))
	require.NoError(t, err)
	assert.Equal(t, []domain.CategoryTotal{
		{Category: "food", Total: 150.30},
		{Category: "transport", Total: 30},
	}, got)
}

func TestAddNullDescription(t *testing.T) {
	s, pool := newStore(t)
	ctx := context.Background()
	_, err := s.Repos().Expenses.Add(ctx, domain.Transaction{Amount: 1, Category: "a", Date: day(10, 1)})
	require.NoError(t, err)

	var isNull bool
	require.NoError(t, pool.QueryRow(`SELECT description IS NULL FROM expenses`).Scan(&isNull))
	assert.True(t, isNull)
}

func TestListOrderDateDescIDDesc(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	for _, tx := range []domain.Transaction{
		{Amount: 1, Category: "a", Description: "first", Date: day(10, 1)},
		{Amount: 2, Category: "a", Date: day(10, 3)},
		{Amount: 3, Category: "b", Description: "third", Date: day(10, 3)},
		{Amount: 4, Category: "b", Date: day(10, 2)},
	} {
		_, err := s.Repos().Expenses.Add(ctx, tx)
		require.NoError(t, err)
	}

	got, err := s.Repos().Expenses.List(ctx)
	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.Equal(t, []int{3, 2, 4, 1}, []int{got[0].ID, got[1].ID, got[2].ID, got[3].ID})
	assert.Equal(t, "third", got[0].Description)
	assert.Equal(t, "", got[1].Description)
	assert.Equal(t, day(10, 3), got[0].Date)
	assert.Equal(t, time.UTC, got[0].Date.Location())
	assert.Equal(t, 3.0, got[0].Amount)
}

// TestReportQueriesExpiredContext проверяет, что обёртки ошибок сохраняют причину (errors.Is работает).
// Обрыв уже запущенного запроса проверяет TestReportQueriesCancelMidQuery.
func TestReportQueriesExpiredContext(t *testing.T) {
	s, _ := newStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()

	_, err := s.Repos().Expenses.Categories(ctx, day(10, 1), day(10, 31))
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	_, err = s.Repos().Expenses.SumByCategoryBetween(ctx, "food", day(10, 1), day(10, 31))
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestReportQueriesCancelMidQuery проверяет боевую конфигурацию пула (db.Open): когда ctx истекает,
// пока запрос ждёт лок, клиент получает DeadlineExceeded, а запрос прерывается и на стороне PostgreSQL
// (pgx при отмене закрывает соединение и шлёт CancelRequest), а не ждёт до конца блокирующей транзакции.
func TestReportQueriesCancelMidQuery(t *testing.T) {
	s, pool := newStore(t)
	ctx := context.Background()

	blocker, err := pool.Conn(ctx)
	require.NoError(t, err)
	defer func() { _ = blocker.Close() }()
	var blockerPID int
	require.NoError(t, blocker.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID))
	_, err = blocker.ExecContext(ctx, `BEGIN`)
	require.NoError(t, err)
	_, err = blocker.ExecContext(ctx, `LOCK TABLE expenses IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	defer func() { _, _ = blocker.ExecContext(context.Background(), `ROLLBACK`) }()

	activeExpenseQueries := func() int {
		var n int
		require.NoError(t, pool.QueryRowContext(ctx,
			`SELECT count(*) FROM pg_stat_activity
			 WHERE state = 'active' AND query ILIKE '%FROM expenses%' AND pid <> pg_backend_pid() AND pid <> $1`,
			blockerPID).Scan(&n))
		return n
	}

	calls := map[string]func(ctx context.Context) error{
		"Categories": func(ctx context.Context) error {
			_, err := s.Repos().Expenses.Categories(ctx, day(10, 1), day(10, 31))
			return err
		},
		"SumByCategoryBetween": func(ctx context.Context) error {
			_, err := s.Repos().Expenses.SumByCategoryBetween(ctx, "food", day(10, 1), day(10, 31))
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			callCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
			defer cancel()
			start := time.Now()
			err := call(callCtx)
			assert.ErrorIs(t, err, context.DeadlineExceeded)
			assert.Less(t, time.Since(start), 3*time.Second, "вызов должен вернуться вскоре после дедлайна")

			// лок всё ещё держится: если запрос не оборван на стороне сервера, он останется active
			deadline := time.Now().Add(5 * time.Second)
			for activeExpenseQueries() > 0 && time.Now().Before(deadline) {
				time.Sleep(50 * time.Millisecond)
			}
			assert.Zero(t, activeExpenseQueries(), "запрос к expenses продолжает выполняться на сервере")
		})
	}
}
