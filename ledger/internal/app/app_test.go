package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/testdb"
)

func TestMain(m *testing.M) { testdb.Main(m) }

var testDate = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

// integrationEnv поднимает чистую тестовую БД и miniredis и направляет в них переменные окружения фабрики.
func integrationEnv(t *testing.T) (*sql.DB, *miniredis.Miniredis) {
	t.Helper()
	pool := testdb.Open(t)
	mr := miniredis.RunT(t)
	t.Setenv("DATABASE_URL", os.Getenv("LEDGER_TEST_DATABASE_URL"))
	t.Setenv("REDIS_ADDR", mr.Addr())
	t.Setenv("REDIS_DB", "")
	return pool, mr
}

func TestNewInMemory(t *testing.T) {
	svc := NewInMemory()
	ctx := context.Background()

	require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "food", Limit: 100}))
	_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: 60, Category: "food", Date: testDate})
	require.NoError(t, err)
	_, err = svc.AddTransaction(ctx, domain.Transaction{Amount: 50, Category: "food", Date: testDate})
	assert.ErrorIs(t, err, domain.ErrBudgetExceeded)

	budgets, err := svc.ListBudgets(ctx)
	require.NoError(t, err)
	assert.Equal(t, []domain.Budget{{Category: "food", Limit: 100}}, budgets)
	txs, err := svc.ListTransactions(ctx)
	require.NoError(t, err)
	assert.Len(t, txs, 1)
}

func TestNewInMemoryIsolated(t *testing.T) {
	a, b := NewInMemory(), NewInMemory()
	require.NoError(t, a.SetBudget(context.Background(), domain.Budget{Category: "x", Limit: 1}))
	got, err := b.ListBudgets(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestNewBadDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x:y@127.0.0.1:1/z?sslmode=disable")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	svc, closeFn, err := New(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connect to PostgreSQL")
	assert.Nil(t, svc)
	assert.Nil(t, closeFn)
}

func TestNewCancelledContext(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x:y@127.0.0.1:1/z?sslmode=disable")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	svc, closeFn, err := New(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled, "ctx должен дойти до Ping")
	assert.Nil(t, svc)
	assert.Nil(t, closeFn)
}

func TestNewConnects(t *testing.T) {
	_, _ = integrationEnv(t)
	buf := captureLog(t)

	svc, closeFn, err := New(context.Background())
	require.NoError(t, err)
	require.NotNil(t, svc)
	require.NotNil(t, closeFn)
	assert.Contains(t, buf.String(), "connected to PostgreSQL")
	assert.Contains(t, buf.String(), "connected to Redis")
	assert.NotContains(t, buf.String(), "postgres:postgres@", "пароль не должен попадать в лог")

	require.NoError(t, svc.SetBudget(context.Background(), domain.Budget{Category: "food", Limit: 10}))
	got, err := svc.ListBudgets(context.Background())
	require.NoError(t, err)
	assert.Len(t, got, 1)

	assert.NoError(t, closeFn())
	assert.NoError(t, closeFn(), "повторный вызов closeFn безопасен")
}

func TestNewRedisDown(t *testing.T) {
	_, _ = integrationEnv(t)
	t.Setenv("REDIS_ADDR", "127.0.0.1:1")

	svc, closeFn, err := New(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connect to Redis")
	assert.Nil(t, svc)
	assert.Nil(t, closeFn)
}

func TestNewBadRedisDB(t *testing.T) {
	_, _ = integrationEnv(t)
	t.Setenv("REDIS_DB", "abc")

	svc, closeFn, err := New(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis config")
	assert.Nil(t, svc)
	assert.Nil(t, closeFn)
}

func schemaSnapshot(t *testing.T, pool *sql.DB) (tables []string, version int64) {
	t.Helper()
	rows, err := pool.Query(`SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' ORDER BY table_name`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, pool.QueryRow(`SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version`).Scan(&version))
	return tables, version
}

func TestNewDoesNotMigrate(t *testing.T) {
	pool, _ := integrationEnv(t)
	tablesBefore, versionBefore := schemaSnapshot(t, pool)
	require.Contains(t, tablesBefore, "budgets", "тестовая БД должна быть уже мигрирована (goose up)")

	_, closeFn, err := New(context.Background())
	require.NoError(t, err)
	require.NoError(t, closeFn())

	tablesAfter, versionAfter := schemaSnapshot(t, pool)
	assert.Equal(t, tablesBefore, tablesAfter)
	assert.Equal(t, versionBefore, versionAfter)
}

func TestLimitOverPg(t *testing.T) {
	_, _ = integrationEnv(t)
	captureLog(t)
	svc, closeFn, err := New(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeFn() })
	ctx := context.Background()

	t.Run("float noise", func(t *testing.T) {
		require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "noise", Limit: 0.30}))
		for _, amount := range []float64{0.10, 0.20} {
			_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: amount, Category: "noise", Date: testDate})
			require.NoError(t, err)
		}
		_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: 0.01, Category: "noise", Date: testDate})
		assert.ErrorIs(t, err, domain.ErrBudgetExceeded)
	})

	t.Run("within and exceeding", func(t *testing.T) {
		require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "food", Limit: 100}))
		_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: 60, Category: "food", Date: testDate})
		require.NoError(t, err)
		_, err = svc.AddTransaction(ctx, domain.Transaction{Amount: 50, Category: "food", Date: testDate})
		require.ErrorIs(t, err, domain.ErrBudgetExceeded)
		_, err = svc.AddTransaction(ctx, domain.Transaction{Amount: 40, Category: "food", Date: testDate})
		require.NoError(t, err, "ровно в лимит")

		list, err := svc.ListTransactions(ctx)
		require.NoError(t, err)
		assert.Len(t, list, 4, "две строки noise и две food, отклонённая не вставлена")
	})

	t.Run("without budget", func(t *testing.T) {
		_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: 1e6, Category: "health", Date: testDate})
		assert.NoError(t, err)
	})

	t.Run("raised limit", func(t *testing.T) {
		require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "raise", Limit: 100}))
		_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: 100, Category: "raise", Date: testDate})
		require.NoError(t, err)
		_, err = svc.AddTransaction(ctx, domain.Transaction{Amount: 50, Category: "raise", Date: testDate})
		require.ErrorIs(t, err, domain.ErrBudgetExceeded)
		require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "raise", Limit: 200}))
		_, err = svc.AddTransaction(ctx, domain.Transaction{Amount: 50, Category: "raise", Date: testDate})
		assert.NoError(t, err)
	})
}

func TestConcurrentAddRespectsLimit(t *testing.T) {
	pool, _ := integrationEnv(t)
	captureLog(t)
	svc, closeFn, err := New(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeFn() })
	ctx := context.Background()
	require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "food", Limit: 100}))

	const workers = 10
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: 30, Category: "food", Date: testDate})
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
		assert.True(t, errors.Is(err, domain.ErrBudgetExceeded), "неожиданная ошибка: %v", err)
	}
	assert.Equal(t, 3, ok)

	var sum float64
	require.NoError(t, pool.QueryRow(`SELECT COALESCE(SUM(amount), 0)::float8 FROM expenses`).Scan(&sum))
	assert.Equal(t, 90.0, sum)
}

func TestReportSurvivesRedisFailure(t *testing.T) {
	_, mr := integrationEnv(t)
	buf := captureLog(t)
	svc, closeFn, err := New(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeFn() })
	ctx := context.Background()

	_, err = svc.AddTransaction(ctx, domain.Transaction{Amount: 12.5, Category: "food", Date: testDate})
	require.NoError(t, err)

	first, err := svc.GetReportSummary(ctx, testDate, testDate)
	require.NoError(t, err)
	_, err = svc.GetReportSummary(ctx, testDate, testDate)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "cache hit")

	mr.Close()

	got, err := svc.GetReportSummary(ctx, testDate, testDate)
	require.NoError(t, err, "отчёт должен отдаваться из БД при недоступном Redis")
	assert.Equal(t, first, got)
	assert.Contains(t, buf.String(), "falling back to database")
}
