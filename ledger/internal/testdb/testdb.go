// Package testdb - хелперы интеграционных тестов с PostgreSQL. Импортируется только из _test.go файлов.
//
// Пакеты с БД-тестами запускаются go test параллельно, а каждый из них очищает таблицы и проверяет
// точное состояние. Поэтому Main держит advisory-lock на весь прогон пакета: БД-пакеты идут строго по очереди.
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/db"
)

const (
	envURL  = "LEDGER_TEST_DATABASE_URL"
	lockKey = 0x68773801
)

// Main - тело TestMain для пакетов с БД-тестами. Без LEDGER_TEST_DATABASE_URL просто запускает тесты.
func Main(m *testing.M) {
	dsn := os.Getenv(envURL)
	if dsn == "" {
		os.Exit(m.Run())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		cancel()
		fmt.Fprintf(os.Stderr, "testdb: %v\n", err)
		os.Exit(1)
	}
	conn, err := pool.Conn(ctx)
	if err == nil {
		_, err = conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, lockKey)
	}
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "testdb: advisory lock: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, lockKey)
	_ = conn.Close()
	_ = pool.Close()
	os.Exit(code)
}

// Open возвращает пул к тестовой БД с пустыми таблицами. Без LEDGER_TEST_DATABASE_URL пропускает тест.
func Open(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv(envURL)
	if dsn == "" {
		t.Skip(envURL + " is not set")
	}
	pool, err := db.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if _, err := pool.ExecContext(context.Background(), `TRUNCATE budgets, expenses RESTART IDENTITY`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pool
}
