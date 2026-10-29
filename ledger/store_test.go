package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCache struct {
	mu      sync.Mutex
	data    map[string][]byte
	ttls    map[string]time.Duration
	getErr  error
	setErr  error
	delErr  error
	deleted []string
}

func newFakeCache() *fakeCache {
	return &fakeCache{data: map[string][]byte{}, ttls: map[string]time.Duration{}}
}

func (f *fakeCache) GetJSON(_ context.Context, key string, dst any) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return false, f.getErr
	}
	raw, ok := f.data[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, dst)
}

func (f *fakeCache) SetJSON(_ context.Context, key string, v any, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f.data[key] = raw
	f.ttls[key] = ttl
	return nil
}

func (f *fakeCache) Delete(_ context.Context, keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.delErr != nil {
		return f.delErr
	}
	for _, k := range keys {
		delete(f.data, k)
		f.deleted = append(f.deleted, k)
	}
	return nil
}

type countingStore struct {
	Store
	budgetCalls int
	reportCalls int
}

func (c *countingStore) ListBudgets(ctx context.Context) ([]Budget, error) {
	c.budgetCalls++
	return c.Store.ListBudgets(ctx)
}

func (c *countingStore) ReportSummary(ctx context.Context, from, to time.Time) (ReportSummary, error) {
	c.reportCalls++
	return c.Store.ReportSummary(ctx, from, to)
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

func day(m time.Month, d int) time.Time {
	return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC)
}

func seededMemoryStore(t *testing.T) *memoryStore {
	t.Helper()
	s := newMemoryStore()
	ctx := context.Background()
	for _, tx := range []Transaction{
		{Amount: 100.10, Category: "food", Date: day(10, 1)},
		{Amount: 50.20, Category: "food", Date: day(10, 10)},
		{Amount: 30, Category: "transport", Date: day(10, 5)},
		{Amount: 999, Category: "food", Date: day(9, 30)},
		{Amount: 999, Category: "health", Date: day(10, 11)},
	} {
		_, err := s.AddTransaction(ctx, tx)
		require.NoError(t, err)
	}
	return s
}

func TestMemoryStoreReportSummary(t *testing.T) {
	t.Parallel()
	s := seededMemoryStore(t)

	got, err := s.ReportSummary(context.Background(), day(10, 1), day(10, 10))
	require.NoError(t, err)
	assert.Equal(t, ReportSummary{
		From:  "2026-10-01",
		To:    "2026-10-10",
		Total: 180.30,
		Categories: []CategoryTotal{
			{Category: "food", Total: 150.30},
			{Category: "transport", Total: 30},
		},
	}, got)
}

func TestMemoryStoreReportSummaryEmpty(t *testing.T) {
	t.Parallel()
	got, err := newMemoryStore().ReportSummary(context.Background(), day(10, 1), day(10, 31))
	require.NoError(t, err)
	assert.NotNil(t, got.Categories)
	assert.Empty(t, got.Categories)
	assert.Equal(t, 0.0, got.Total)
}

func TestCachedReportSummaryMissThenHit(t *testing.T) {
	buf := captureLog(t)
	fc := newFakeCache()
	inner := &countingStore{Store: seededMemoryStore(t)}
	s := newCachedStore(inner, fc)
	ctx := context.Background()

	first, err := s.ReportSummary(ctx, day(10, 1), day(10, 10))
	require.NoError(t, err)
	second, err := s.ReportSummary(ctx, day(10, 1), day(10, 10))
	require.NoError(t, err)

	assert.Equal(t, first, second)
	assert.Equal(t, 1, inner.reportCalls)
	assert.Equal(t, 30*time.Second, fc.ttls["report:summary:2026-10-01:2026-10-10"])
	assert.Contains(t, buf.String(), "cache miss report:summary:2026-10-01:2026-10-10")
	assert.Contains(t, buf.String(), "cache hit report:summary:2026-10-01:2026-10-10")
}

func TestCachedReportSummaryKeyPerInterval(t *testing.T) {
	captureLog(t)
	fc := newFakeCache()
	inner := &countingStore{Store: seededMemoryStore(t)}
	s := newCachedStore(inner, fc)
	ctx := context.Background()

	_, err := s.ReportSummary(ctx, day(10, 1), day(10, 10))
	require.NoError(t, err)
	_, err = s.ReportSummary(ctx, day(10, 1), day(10, 31))
	require.NoError(t, err)

	assert.Equal(t, 2, inner.reportCalls)
	assert.Len(t, fc.data, 2)
}

func TestCachedListBudgetsAndInvalidation(t *testing.T) {
	buf := captureLog(t)
	fc := newFakeCache()
	inner := &countingStore{Store: newMemoryStore()}
	s := newCachedStore(inner, fc)
	ctx := context.Background()

	require.NoError(t, s.SetBudget(ctx, Budget{Category: "food", Limit: 100}))

	got, err := s.ListBudgets(ctx)
	require.NoError(t, err)
	assert.Equal(t, []Budget{{Category: "food", Limit: 100}}, got)
	_, err = s.ListBudgets(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, inner.budgetCalls)
	assert.Contains(t, buf.String(), "cache hit budgets:all")
	assert.Equal(t, 30*time.Second, fc.ttls["budgets:all"])

	require.NoError(t, s.SetBudget(ctx, Budget{Category: "food", Limit: 200}))
	assert.Contains(t, fc.deleted, "budgets:all")

	got, err = s.ListBudgets(ctx)
	require.NoError(t, err)
	assert.Equal(t, []Budget{{Category: "food", Limit: 200}}, got)
	assert.Equal(t, 2, inner.budgetCalls)
}

func TestCachedInvalidSetBudgetDoesNotInvalidate(t *testing.T) {
	captureLog(t)
	fc := newFakeCache()
	s := newCachedStore(newMemoryStore(), fc)

	err := s.SetBudget(context.Background(), Budget{Category: "", Limit: 1})
	require.Error(t, err)
	assert.Empty(t, fc.deleted)
}

func TestCachedDegradesWhenCacheFails(t *testing.T) {
	buf := captureLog(t)
	fc := newFakeCache()
	boom := errors.New("redis down")
	fc.getErr, fc.setErr, fc.delErr = boom, boom, boom
	inner := &countingStore{Store: seededMemoryStore(t)}
	s := newCachedStore(inner, fc)
	ctx := context.Background()

	require.NoError(t, s.SetBudget(ctx, Budget{Category: "food", Limit: 100}))

	budgets, err := s.ListBudgets(ctx)
	require.NoError(t, err)
	assert.Len(t, budgets, 1)

	report, err := s.ReportSummary(ctx, day(10, 1), day(10, 10))
	require.NoError(t, err)
	assert.Equal(t, 180.30, report.Total)
	assert.Contains(t, buf.String(), "redis down")

	_, err = s.ReportSummary(ctx, day(10, 1), day(10, 10))
	require.NoError(t, err)
	assert.Equal(t, 2, inner.reportCalls)
}

func TestCachedPassesThroughTransactions(t *testing.T) {
	captureLog(t)
	s := newCachedStore(newMemoryStore(), newFakeCache())
	ctx := context.Background()

	saved, err := s.AddTransaction(ctx, Transaction{Amount: 5, Category: "x", Date: day(10, 1)})
	require.NoError(t, err)
	assert.Equal(t, 1, saved.ID)
	list, err := s.ListTransactions(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestGetReportSummaryValidation(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	tests := []struct {
		name     string
		from, to time.Time
		wantMsg  string
	}{
		{"zero from", time.Time{}, day(10, 1), "from must be set"},
		{"zero to", day(10, 1), time.Time{}, "to must be set"},
		{"from after to", day(10, 2), day(10, 1), "from must not be after to"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := GetReportSummary(context.Background(), tt.from, tt.to)
			var ve *ValidationError
			require.True(t, errors.As(err, &ve))
			assert.Equal(t, tt.wantMsg, ve.Message)
		})
	}
}

func TestGetReportSummaryOnMemoryStore(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	_, err := AddTransaction(Transaction{Amount: 10, Category: "food", Date: day(10, 3)})
	require.NoError(t, err)
	_, err = AddTransaction(Transaction{Amount: 5, Category: "food", Date: day(10, 4)})
	require.NoError(t, err)

	got, err := GetReportSummary(context.Background(), day(10, 3), day(10, 3))
	require.NoError(t, err)
	assert.Equal(t, 10.0, got.Total)
	assert.Equal(t, []CategoryTotal{{Category: "food", Total: 10}}, got.Categories)
}

func TestCloseResetsToMemory(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	require.NoError(t, SetBudget(Budget{Category: "food", Limit: 1}))
	require.NoError(t, Close())
	got, err := ListBudgets()
	require.NoError(t, err)
	assert.Empty(t, got)
}
