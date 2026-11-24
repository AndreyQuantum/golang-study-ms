package service

import (
	"context"
	"sync"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/repository/memory"
)

// spy оборачивает memory.Store: помнит порядок вызовов и то, были ли они внутри транзакции.
type spy struct {
	store *memory.Store

	mu    sync.Mutex
	inTx  bool
	calls []call
}

type call struct {
	name string
	inTx bool
}

func newSpy() *spy { return &spy{store: memory.New()} }

func (s *spy) record(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call{name: name, inTx: s.inTx})
}

func (s *spy) names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.calls))
	for _, c := range s.calls {
		out = append(out, c.name)
	}
	return out
}

func (s *spy) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = nil
}

func (s *spy) service() Service { return New(s.repos(s.store.Repos()), s) }

func (s *spy) repos(inner domain.Repos) domain.Repos {
	return domain.Repos{
		Budgets:  &spyBudgets{s: s, inner: inner.Budgets},
		Expenses: &spyExpenses{s: s, inner: inner.Expenses},
	}
}

// WithinTx реализует domain.TxManager. Флаг inTx общий, поэтому тесты порядка вызовов идут последовательно.
func (s *spy) WithinTx(ctx context.Context, fn func(context.Context, domain.Repos) error) error {
	return s.store.WithinTx(ctx, func(ctx context.Context, r domain.Repos) error {
		s.mu.Lock()
		s.inTx = true
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			s.inTx = false
			s.mu.Unlock()
		}()
		return fn(ctx, s.repos(r))
	})
}

type spyBudgets struct {
	s     *spy
	inner domain.BudgetRepository
}

func (r *spyBudgets) Upsert(ctx context.Context, b domain.Budget) error {
	r.s.record("Budgets.Upsert")
	return r.inner.Upsert(ctx, b)
}

func (r *spyBudgets) GetByCategory(ctx context.Context, c string) (domain.Budget, error) {
	r.s.record("Budgets.GetByCategory")
	return r.inner.GetByCategory(ctx, c)
}

func (r *spyBudgets) GetByCategoryForUpdate(ctx context.Context, c string) (domain.Budget, error) {
	r.s.record("Budgets.GetByCategoryForUpdate")
	return r.inner.GetByCategoryForUpdate(ctx, c)
}

func (r *spyBudgets) List(ctx context.Context) ([]domain.Budget, error) {
	r.s.record("Budgets.List")
	return r.inner.List(ctx)
}

type spyExpenses struct {
	s     *spy
	inner domain.ExpenseRepository
}

func (r *spyExpenses) Add(ctx context.Context, t domain.Transaction) (domain.Transaction, error) {
	r.s.record("Expenses.Add")
	return r.inner.Add(ctx, t)
}

func (r *spyExpenses) List(ctx context.Context) ([]domain.Transaction, error) {
	r.s.record("Expenses.List")
	return r.inner.List(ctx)
}

func (r *spyExpenses) SumByCategory(ctx context.Context, c string) (domain.Money, error) {
	r.s.record("Expenses.SumByCategory")
	return r.inner.SumByCategory(ctx, c)
}

func (r *spyExpenses) TotalsByCategory(ctx context.Context, from, to time.Time) ([]domain.CategoryTotal, error) {
	r.s.record("Expenses.TotalsByCategory")
	return r.inner.TotalsByCategory(ctx, from, to)
}
