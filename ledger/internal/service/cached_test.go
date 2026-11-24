package service

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

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
)

type fakeCache struct {
	mu      sync.Mutex
	data    map[string][]byte
	ttls    map[string]time.Duration
	getErr  error
	setErr  error
	delErr  error
	deleted []string
	gets    int
	sets    int
}

func newFakeCache() *fakeCache {
	return &fakeCache{data: map[string][]byte{}, ttls: map[string]time.Duration{}}
}

func (f *fakeCache) GetJSON(_ context.Context, key string, dst any) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
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
	f.sets++
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

type countingService struct {
	Service
	budgetCalls int
	reportCalls int
}

func (c *countingService) ListBudgets(ctx context.Context) ([]domain.Budget, error) {
	c.budgetCalls++
	return c.Service.ListBudgets(ctx)
}

func (c *countingService) GetReportSummary(ctx context.Context, from, to time.Time) (domain.ReportSummary, error) {
	c.reportCalls++
	return c.Service.GetReportSummary(ctx, from, to)
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

func TestCachedReportSummaryMissThenHit(t *testing.T) {
	buf := captureLog(t)
	fc := newFakeCache()
	inner := &countingService{Service: seeded(t)}
	s := NewCached(inner, fc)
	ctx := context.Background()

	first, err := s.GetReportSummary(ctx, day(10, 1), day(10, 10))
	require.NoError(t, err)
	second, err := s.GetReportSummary(ctx, day(10, 1), day(10, 10))
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
	inner := &countingService{Service: seeded(t)}
	s := NewCached(inner, fc)
	ctx := context.Background()

	_, err := s.GetReportSummary(ctx, day(10, 1), day(10, 10))
	require.NoError(t, err)
	_, err = s.GetReportSummary(ctx, day(10, 1), day(10, 31))
	require.NoError(t, err)

	assert.Equal(t, 2, inner.reportCalls)
	assert.Len(t, fc.data, 2)
}

func TestCachedListBudgetsAndInvalidation(t *testing.T) {
	buf := captureLog(t)
	fc := newFakeCache()
	inner := &countingService{Service: newService()}
	s := NewCached(inner, fc)
	ctx := context.Background()

	require.NoError(t, s.SetBudget(ctx, domain.Budget{Category: "food", Limit: 100}))

	got, err := s.ListBudgets(ctx)
	require.NoError(t, err)
	assert.Equal(t, []domain.Budget{{Category: "food", Limit: 100}}, got)
	_, err = s.ListBudgets(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, inner.budgetCalls)
	assert.Contains(t, buf.String(), "cache hit budgets:all")
	assert.Equal(t, 30*time.Second, fc.ttls["budgets:all"])

	require.NoError(t, s.SetBudget(ctx, domain.Budget{Category: "food", Limit: 200}))
	assert.Contains(t, fc.deleted, "budgets:all")

	got, err = s.ListBudgets(ctx)
	require.NoError(t, err)
	assert.Equal(t, []domain.Budget{{Category: "food", Limit: 200}}, got)
	assert.Equal(t, 2, inner.budgetCalls)
}

func TestCachedFailedSetBudgetDoesNotInvalidate(t *testing.T) {
	captureLog(t)
	fc := newFakeCache()
	s := NewCached(newService(), fc)

	err := s.SetBudget(context.Background(), domain.Budget{Category: "", Limit: 1})
	require.Error(t, err)
	assert.Empty(t, fc.deleted)
}

func TestCachedDegradesWhenCacheFails(t *testing.T) {
	buf := captureLog(t)
	fc := newFakeCache()
	boom := errors.New("redis down")
	fc.getErr, fc.setErr, fc.delErr = boom, boom, boom
	inner := &countingService{Service: seeded(t)}
	s := NewCached(inner, fc)
	ctx := context.Background()

	require.NoError(t, s.SetBudget(ctx, domain.Budget{Category: "food", Limit: 100}))

	budgets, err := s.ListBudgets(ctx)
	require.NoError(t, err)
	assert.Len(t, budgets, 1)

	report, err := s.GetReportSummary(ctx, day(10, 1), day(10, 10))
	require.NoError(t, err)
	assert.Equal(t, 180.30, report.Total)
	assert.Contains(t, buf.String(), "redis down")
	assert.Contains(t, buf.String(), "falling back to database")

	_, err = s.GetReportSummary(ctx, day(10, 1), day(10, 10))
	require.NoError(t, err)
	assert.Equal(t, 2, inner.reportCalls)
}

func TestCachedPassesThroughTransactions(t *testing.T) {
	captureLog(t)
	fc := newFakeCache()
	s := NewCached(newService(), fc)
	ctx := context.Background()

	saved, err := s.AddTransaction(ctx, domain.Transaction{Amount: 5, Category: "x", Date: day(10, 1)})
	require.NoError(t, err)
	assert.Equal(t, 1, saved.ID)
	list, err := s.ListTransactions(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Zero(t, fc.gets+fc.sets, "транзакции кеш не трогают")
}

func TestCachedInvalidIntervalSkipsCache(t *testing.T) {
	buf := captureLog(t)
	fc := newFakeCache()
	inner := &countingService{Service: newService()}
	s := NewCached(inner, fc)

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
			_, err := s.GetReportSummary(context.Background(), tt.from, tt.to)
			var ve *domain.ValidationError
			require.ErrorAs(t, err, &ve)
			assert.Equal(t, tt.wantMsg, ve.Message)
		})
	}
	assert.Zero(t, fc.gets+fc.sets)
	assert.Zero(t, inner.reportCalls)
	assert.NotContains(t, buf.String(), "cache miss")
}
