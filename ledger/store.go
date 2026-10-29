package ledger

import (
	"context"
	"log"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger/repository"
)

const (
	dateLayout      = "2006-01-02"
	cacheTTL        = 30 * time.Second
	budgetsCacheKey = "budgets:all"
)

type Store interface {
	SetBudget(ctx context.Context, b Budget) error
	ListBudgets(ctx context.Context) ([]Budget, error)
	AddTransaction(ctx context.Context, tx Transaction) (Transaction, error)
	ListTransactions(ctx context.Context) ([]Transaction, error)
	ReportSummary(ctx context.Context, from, to time.Time) (ReportSummary, error)
}

type Cache interface {
	GetJSON(ctx context.Context, key string, dst any) (bool, error)
	SetJSON(ctx context.Context, key string, v any, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
}

func dayUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func roundCents(x float64) float64 {
	return math.Round(x*100) / 100
}

func reportKey(from, to time.Time) string {
	return "report:summary:" + from.UTC().Format(dateLayout) + ":" + to.UTC().Format(dateLayout)
}

type memoryStore struct {
	budgets      *repository.BudgetRepository
	transactions *repository.TransactionRepository
}

func newMemoryStore() *memoryStore {
	budgets := repository.NewBudgetRepository()
	return &memoryStore{
		budgets:      budgets,
		transactions: repository.NewTransactionRepository(budgets),
	}
}

func (s *memoryStore) SetBudget(_ context.Context, b Budget) error {
	return s.budgets.SetBudget(b)
}

func (s *memoryStore) ListBudgets(context.Context) ([]Budget, error) {
	return s.budgets.ListBudgets(), nil
}

func (s *memoryStore) AddTransaction(_ context.Context, tx Transaction) (Transaction, error) {
	tx.Amount = roundCents(tx.Amount)
	tx.Date = dayUTC(tx.Date)
	return s.transactions.AddTransaction(tx)
}

func (s *memoryStore) ListTransactions(context.Context) ([]Transaction, error) {
	return s.transactions.ListTransactions(), nil
}

func (s *memoryStore) ReportSummary(_ context.Context, from, to time.Time) (ReportSummary, error) {
	from, to = dayUTC(from), dayUTC(to)
	cents := map[string]int64{}
	for _, tx := range s.transactions.ListTransactions() {
		day := dayUTC(tx.Date)
		if day.Before(from) || day.After(to) {
			continue
		}
		cents[tx.Category] += int64(math.Round(tx.Amount * 100))
	}

	report := ReportSummary{
		From:       from.Format(dateLayout),
		To:         to.Format(dateLayout),
		Categories: []CategoryTotal{},
	}
	var total int64
	for category, c := range cents {
		report.Categories = append(report.Categories, CategoryTotal{Category: category, Total: float64(c) / 100})
		total += c
	}
	slices.SortFunc(report.Categories, func(a, b CategoryTotal) int { return strings.Compare(a.Category, b.Category) })
	report.Total = float64(total) / 100
	return report, nil
}

type cachedStore struct {
	Store
	cache Cache
	ttl   time.Duration
}

func newCachedStore(inner Store, c Cache) *cachedStore {
	return &cachedStore{Store: inner, cache: c, ttl: cacheTTL}
}

func (s *cachedStore) SetBudget(ctx context.Context, b Budget) error {
	if err := s.Store.SetBudget(ctx, b); err != nil {
		return err
	}
	if err := s.cache.Delete(ctx, budgetsCacheKey); err != nil {
		log.Printf("cache invalidate %s: %v", budgetsCacheKey, err)
	}
	return nil
}

func (s *cachedStore) ListBudgets(ctx context.Context) ([]Budget, error) {
	var cached []Budget
	if s.lookup(ctx, budgetsCacheKey, &cached) {
		return cached, nil
	}
	budgets, err := s.Store.ListBudgets(ctx)
	if err != nil {
		return nil, err
	}
	s.store(ctx, budgetsCacheKey, budgets)
	return budgets, nil
}

func (s *cachedStore) ReportSummary(ctx context.Context, from, to time.Time) (ReportSummary, error) {
	key := reportKey(from, to)
	var cached ReportSummary
	if s.lookup(ctx, key, &cached) {
		return cached, nil
	}
	report, err := s.Store.ReportSummary(ctx, from, to)
	if err != nil {
		return ReportSummary{}, err
	}
	s.store(ctx, key, report)
	return report, nil
}

func (s *cachedStore) lookup(ctx context.Context, key string, dst any) bool {
	ok, err := s.cache.GetJSON(ctx, key, dst)
	switch {
	case err != nil:
		log.Printf("cache error for %s, falling back to database: %v", key, err)
		return false
	case ok:
		log.Printf("cache hit %s", key)
		return true
	default:
		log.Printf("cache miss %s", key)
		return false
	}
}

func (s *cachedStore) store(ctx context.Context, key string, v any) {
	if err := s.cache.SetJSON(ctx, key, v, s.ttl); err != nil {
		log.Printf("cache set %s: %v", key, err)
	}
}

var _ Store = (*memoryStore)(nil)
var _ Store = (*cachedStore)(nil)
