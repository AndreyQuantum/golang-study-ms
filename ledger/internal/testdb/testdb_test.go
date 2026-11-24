package testdb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) { Main(m) }

func TestOpenTruncatesTables(t *testing.T) {
	pool := Open(t)
	_, err := pool.Exec(`INSERT INTO budgets(category, limit_amount) VALUES('x', 1)`)
	require.NoError(t, err)

	pool2 := Open(t)
	var n int
	require.NoError(t, pool2.QueryRow(`SELECT COUNT(*) FROM budgets`).Scan(&n))
	assert.Equal(t, 0, n)
}

func TestMainHoldsAdvisoryLock(t *testing.T) {
	pool := Open(t)
	conn, err := pool.Conn(context.Background())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	var got bool
	require.NoError(t, conn.QueryRowContext(context.Background(), `SELECT pg_try_advisory_lock($1)`, lockKey).Scan(&got))
	assert.False(t, got, "блокировка прогона должна удерживаться процессом тестов")
}
