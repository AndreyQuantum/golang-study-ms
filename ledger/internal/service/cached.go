package service

import (
	"context"
	"log"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
)

const (
	cacheTTL        = 30 * time.Second
	budgetsCacheKey = "budgets:all"
)

// Cache - хранилище JSON с TTL (в проде Redis). Если оно недоступно, сервис работает без него.
type Cache interface {
	GetJSON(ctx context.Context, key string, dst any) (bool, error)
	SetJSON(ctx context.Context, key string, v any, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
}

type cached struct {
	Service
	cache Cache
	ttl   time.Duration
}

// NewCached кеширует отчёт (ключ на интервал) и список бюджетов на 30 секунд. SetBudget сбрасывает список бюджетов.
// Остальные методы проксируются. Ошибки кеша только логируются, запрос берётся из inner.
func NewCached(inner Service, c Cache) Service {
	return &cached{Service: inner, cache: c, ttl: cacheTTL}
}

func reportKey(from, to time.Time) string {
	return "report:summary:" + from.UTC().Format(dateLayout) + ":" + to.UTC().Format(dateLayout)
}

func (s *cached) SetBudget(ctx context.Context, b domain.Budget) error {
	if err := s.Service.SetBudget(ctx, b); err != nil {
		return err
	}
	if err := s.cache.Delete(ctx, budgetsCacheKey); err != nil {
		log.Printf("cache invalidate %s: %v", budgetsCacheKey, err)
	}
	return nil
}

func (s *cached) ListBudgets(ctx context.Context) ([]domain.Budget, error) {
	var hit []domain.Budget
	if s.lookup(ctx, budgetsCacheKey, &hit) {
		return hit, nil
	}
	budgets, err := s.Service.ListBudgets(ctx)
	if err != nil {
		return nil, err
	}
	s.store(ctx, budgetsCacheKey, budgets)
	return budgets, nil
}

func (s *cached) GetReportSummary(ctx context.Context, from, to time.Time) (domain.ReportSummary, error) {
	if err := validateInterval(from, to); err != nil {
		return domain.ReportSummary{}, err
	}
	key := reportKey(from, to)
	var hit domain.ReportSummary
	if s.lookup(ctx, key, &hit) {
		return hit, nil
	}
	report, err := s.Service.GetReportSummary(ctx, from, to)
	if err != nil {
		return domain.ReportSummary{}, err
	}
	s.store(ctx, key, report)
	return report, nil
}

func (s *cached) lookup(ctx context.Context, key string, dst any) bool {
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

func (s *cached) store(ctx context.Context, key string, v any) {
	if err := s.cache.SetJSON(ctx, key, v, s.ttl); err != nil {
		log.Printf("cache set %s: %v", key, err)
	}
}
