// Package service - бизнес-логика ledger: валидация, правило бюджета, отчёт.
// Знает только интерфейсы domain, о способе хранения ничего не знает.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
)

const dateLayout = "2006-01-02"

// Service - то, что видят gateway и демо. Первым параметром всегда идёт ctx.
type Service interface {
	SetBudget(ctx context.Context, b domain.Budget) error
	ListBudgets(ctx context.Context) ([]domain.Budget, error)
	AddTransaction(ctx context.Context, tx domain.Transaction) (domain.Transaction, error)
	ListTransactions(ctx context.Context) ([]domain.Transaction, error)
	GetReportSummary(ctx context.Context, from, to time.Time) (domain.ReportSummary, error)
}

type service struct {
	repos domain.Repos
	tx    domain.TxManager
}

func New(repos domain.Repos, tx domain.TxManager) Service {
	return &service{repos: repos, tx: tx}
}

func (s *service) SetBudget(ctx context.Context, b domain.Budget) error {
	if err := b.Validate(); err != nil {
		return err
	}
	return s.repos.Budgets.Upsert(ctx, b)
}

func (s *service) ListBudgets(ctx context.Context) ([]domain.Budget, error) {
	return s.repos.Budgets.List(ctx)
}

// AddTransaction проверяет правило "сумма по категории + новая сумма <= лимит" и сохраняет расход.
// Проверка и вставка идут в одной транзакции под блокировкой бюджета категории, поэтому параллельные
// запросы не могут вместе превысить лимит. Запись расхода - последняя операция внутри транзакции.
func (s *service) AddTransaction(ctx context.Context, t domain.Transaction) (domain.Transaction, error) {
	if err := t.Validate(); err != nil {
		return domain.Transaction{}, err
	}
	t.Amount = round2(t.Amount)
	t.Date = dayUTC(t.Date)

	var saved domain.Transaction
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r domain.Repos) error {
		b, err := r.Budgets.GetByCategoryForUpdate(ctx, t.Category)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			// у категории нет лимита
		case err != nil:
			return fmt.Errorf("lock budget: %w", err)
		default:
			spent, err := r.Expenses.SumByCategory(ctx, t.Category)
			if err != nil {
				return fmt.Errorf("check budget: %w", err)
			}
			limit, amount := domain.MoneyFromFloat(b.Limit), domain.MoneyFromFloat(t.Amount)
			if spent+amount > limit {
				return fmt.Errorf("%w: category %q, limit %s, spent %s, new %s",
					domain.ErrBudgetExceeded, t.Category, limit, spent, amount)
			}
		}
		saved, err = r.Expenses.Add(ctx, t)
		return err
	})
	if err != nil {
		return domain.Transaction{}, err
	}
	return saved, nil
}

func (s *service) ListTransactions(ctx context.Context) ([]domain.Transaction, error) {
	return s.repos.Expenses.List(ctx)
}

func (s *service) GetReportSummary(ctx context.Context, from, to time.Time) (domain.ReportSummary, error) {
	if err := validateInterval(from, to); err != nil {
		return domain.ReportSummary{}, err
	}
	from, to = dayUTC(from), dayUTC(to)
	totals, err := s.repos.Expenses.TotalsByCategory(ctx, from, to)
	if err != nil {
		return domain.ReportSummary{}, err
	}

	report := domain.ReportSummary{
		From:       from.Format(dateLayout),
		To:         to.Format(dateLayout),
		Categories: make([]domain.CategoryTotal, 0, len(totals)),
	}
	var total domain.Money
	for _, c := range totals {
		cents := domain.MoneyFromFloat(c.Total)
		report.Categories = append(report.Categories, domain.CategoryTotal{Category: c.Category, Total: cents.Float()})
		total += cents
	}
	report.Total = total.Float()
	return report, nil
}

// validateInterval проверяет границы отчёта; её вызывают и сервис, и кеш-декоратор до обращения к Redis.
func validateInterval(from, to time.Time) error {
	if from.IsZero() {
		return &domain.ValidationError{Field: "from", Message: "from must be set"}
	}
	if to.IsZero() {
		return &domain.ValidationError{Field: "to", Message: "to must be set"}
	}
	if dayUTC(from).After(dayUTC(to)) {
		return &domain.ValidationError{Field: "from", Message: "from must not be after to"}
	}
	return nil
}
