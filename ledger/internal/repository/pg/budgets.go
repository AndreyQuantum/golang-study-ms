package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
)

type budgetRepo struct {
	q dbtx
}

func (r *budgetRepo) Upsert(ctx context.Context, b domain.Budget) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO budgets(category, limit_amount) VALUES($1, $2::numeric)
		 ON CONFLICT(category) DO UPDATE SET limit_amount = EXCLUDED.limit_amount`,
		b.Category, money(b.Limit))
	if err != nil {
		return fmt.Errorf("set budget %q: %w", b.Category, err)
	}
	return nil
}

func (r *budgetRepo) GetByCategory(ctx context.Context, category string) (domain.Budget, error) {
	return r.get(ctx, `SELECT limit_amount::float8 FROM budgets WHERE category = $1`, category)
}

// GetByCategoryForUpdate берёт блокировку строки бюджета до конца транзакции: параллельная
// проверка лимита по той же категории ждёт коммита или отката.
func (r *budgetRepo) GetByCategoryForUpdate(ctx context.Context, category string) (domain.Budget, error) {
	return r.get(ctx, `SELECT limit_amount::float8 FROM budgets WHERE category = $1 FOR UPDATE`, category)
}

func (r *budgetRepo) get(ctx context.Context, query, category string) (domain.Budget, error) {
	b := domain.Budget{Category: category}
	err := r.q.QueryRowContext(ctx, query, category).Scan(&b.Limit)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return domain.Budget{}, domain.ErrNotFound
	case err != nil:
		return domain.Budget{}, fmt.Errorf("get budget %q: %w", category, err)
	}
	return b, nil
}

func (r *budgetRepo) List(ctx context.Context) ([]domain.Budget, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT category, limit_amount::float8 FROM budgets ORDER BY category`)
	if err != nil {
		return nil, fmt.Errorf("list budgets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := []domain.Budget{}
	for rows.Next() {
		var b domain.Budget
		if err := rows.Scan(&b.Category, &b.Limit); err != nil {
			return nil, fmt.Errorf("scan budget: %w", err)
		}
		result = append(result, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list budgets: %w", err)
	}
	return result, nil
}
