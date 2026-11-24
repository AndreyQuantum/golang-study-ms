package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AndreyQuantum/golang-study-ms/ledger"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "budgets.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestSeedBudgets(t *testing.T) {
	ctx := context.Background()

	t.Run("valid file", func(t *testing.T) {
		svc := ledger.NewInMemory()
		path := writeFile(t, `[{"category":"food","limit":100},{"category":"transport","limit":50}]`)
		require.NoError(t, seedBudgets(ctx, svc, path))

		got, err := svc.ListBudgets(ctx)
		require.NoError(t, err)
		assert.Equal(t, []ledger.Budget{{Category: "food", Limit: 100}, {Category: "transport", Limit: 50}}, got)
	})

	t.Run("invalid budget", func(t *testing.T) {
		svc := ledger.NewInMemory()
		err := seedBudgets(ctx, svc, writeFile(t, `[{"category":"food","limit":-1}]`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse")
		got, _ := svc.ListBudgets(ctx)
		assert.Empty(t, got)
	})

	t.Run("missing file", func(t *testing.T) {
		err := seedBudgets(ctx, ledger.NewInMemory(), filepath.Join(t.TempDir(), "nope.json"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "open budgets file")
	})
}

func TestRunDemo(t *testing.T) {
	ctx := context.Background()
	svc := ledger.NewInMemory()
	path := writeFile(t, `[{"category":"food","limit":5000}]`)

	require.NoError(t, run(ctx, svc, path, true))

	txs, err := svc.ListTransactions(ctx)
	require.NoError(t, err)
	assert.Len(t, txs, 2, "в демо проходят только транзакции в пределах лимита и без бюджета")
}

func TestRunWithBadSeed(t *testing.T) {
	err := run(context.Background(), ledger.NewInMemory(), filepath.Join(t.TempDir(), "nope.json"), false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "seed budgets")
}
