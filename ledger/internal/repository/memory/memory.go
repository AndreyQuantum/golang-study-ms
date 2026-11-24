// Package memory - реализация domain.Repos и domain.TxManager в памяти.
// Нужна для юнит-тестов сервиса и для работы без БД.
package memory

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
)

// Store держит бюджеты и расходы под одним mutex.
type Store struct {
	mu       sync.Mutex
	budgets  map[string]domain.Budget
	expenses []domain.Transaction
}

var _ domain.TxManager = (*Store)(nil)

func New() *Store {
	return &Store{budgets: map[string]domain.Budget{}}
}

// Repos возвращает репозитории для вызовов вне транзакции: каждый метод сам берёт mutex.
func (s *Store) Repos() domain.Repos {
	return s.repos(false)
}

func (s *Store) repos(locked bool) domain.Repos {
	return domain.Repos{
		Budgets:  &budgetRepo{s: s, locked: locked},
		Expenses: &expenseRepo{s: s, locked: locked},
	}
}

// WithinTx держит mutex на всё время fn, поэтому проверка лимита и вставка атомарны.
// Ограничения: отката нет (сервис делает запись последней операцией fn), а вложенный
// WithinTx приведёт к deadlock - внутри fn пользуйтесь только переданными repos.
func (s *Store) WithinTx(ctx context.Context, fn func(ctx context.Context, repos domain.Repos) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(ctx, s.repos(true))
}

func (s *Store) lock(locked bool) func() {
	if locked {
		return func() {}
	}
	s.mu.Lock()
	return s.mu.Unlock
}

func dayUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

type budgetRepo struct {
	s      *Store
	locked bool
}

func (r *budgetRepo) Upsert(ctx context.Context, b domain.Budget) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	defer r.s.lock(r.locked)()
	r.s.budgets[b.Category] = b
	return nil
}

func (r *budgetRepo) GetByCategory(ctx context.Context, category string) (domain.Budget, error) {
	if err := ctx.Err(); err != nil {
		return domain.Budget{}, err
	}
	defer r.s.lock(r.locked)()
	b, ok := r.s.budgets[category]
	if !ok {
		return domain.Budget{}, domain.ErrNotFound
	}
	return b, nil
}

// GetByCategoryForUpdate: блокировка уже держится mutex'ом транзакции.
func (r *budgetRepo) GetByCategoryForUpdate(ctx context.Context, category string) (domain.Budget, error) {
	return r.GetByCategory(ctx, category)
}

func (r *budgetRepo) List(ctx context.Context) ([]domain.Budget, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	defer r.s.lock(r.locked)()
	result := make([]domain.Budget, 0, len(r.s.budgets))
	for _, category := range slices.Sorted(maps.Keys(r.s.budgets)) {
		result = append(result, r.s.budgets[category])
	}
	return result, nil
}

type expenseRepo struct {
	s      *Store
	locked bool
}

func (r *expenseRepo) Add(ctx context.Context, t domain.Transaction) (domain.Transaction, error) {
	if err := ctx.Err(); err != nil {
		return domain.Transaction{}, err
	}
	defer r.s.lock(r.locked)()
	t.ID = len(r.s.expenses) + 1
	r.s.expenses = append(r.s.expenses, t)
	return t, nil
}

func (r *expenseRepo) List(ctx context.Context) ([]domain.Transaction, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	defer r.s.lock(r.locked)()
	return append([]domain.Transaction{}, r.s.expenses...), nil
}

func (r *expenseRepo) SumByCategory(ctx context.Context, category string) (domain.Money, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	defer r.s.lock(r.locked)()
	var sum domain.Money
	for _, t := range r.s.expenses {
		if t.Category == category {
			sum += domain.MoneyFromFloat(t.Amount)
		}
	}
	return sum, nil
}

func (r *expenseRepo) TotalsByCategory(ctx context.Context, from, to time.Time) ([]domain.CategoryTotal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	from, to = dayUTC(from), dayUTC(to)
	defer r.s.lock(r.locked)()
	sums := map[string]domain.Money{}
	for _, t := range r.s.expenses {
		day := dayUTC(t.Date)
		if day.Before(from) || day.After(to) {
			continue
		}
		sums[t.Category] += domain.MoneyFromFloat(t.Amount)
	}
	result := make([]domain.CategoryTotal, 0, len(sums))
	for _, category := range slices.Sorted(maps.Keys(sums)) {
		result = append(result, domain.CategoryTotal{Category: category, Total: sums[category].Float()})
	}
	return result, nil
}
