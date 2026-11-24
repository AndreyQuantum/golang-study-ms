package pg

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
)

type expenseRepo struct {
	q dbtx
}

func (r *expenseRepo) Add(ctx context.Context, t domain.Transaction) (domain.Transaction, error) {
	var description sql.NullString
	if t.Description != "" {
		description = sql.NullString{String: t.Description, Valid: true}
	}
	var id int
	err := r.q.QueryRowContext(ctx,
		`INSERT INTO expenses(amount, category, description, date) VALUES($1::numeric, $2, $3, $4) RETURNING id`,
		money(t.Amount), t.Category, description, dayUTC(t.Date)).Scan(&id)
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("insert expense: %w", err)
	}
	t.ID = id
	t.Amount = math.Round(t.Amount*100) / 100
	t.Date = dayUTC(t.Date)
	return t, nil
}

func (r *expenseRepo) List(ctx context.Context) ([]domain.Transaction, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, amount::float8, category, description, date FROM expenses ORDER BY date DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := []domain.Transaction{}
	for rows.Next() {
		var t domain.Transaction
		var description sql.NullString
		if err := rows.Scan(&t.ID, &t.Amount, &t.Category, &description, &t.Date); err != nil {
			return nil, fmt.Errorf("scan transaction: %w", err)
		}
		t.Description = description.String
		t.Date = dayUTC(t.Date)
		result = append(result, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}
	return result, nil
}

func (r *expenseRepo) SumByCategory(ctx context.Context, category string) (domain.Money, error) {
	var cents int64
	err := r.q.QueryRowContext(ctx,
		`SELECT (COALESCE(SUM(amount), 0) * 100)::bigint FROM expenses WHERE category = $1`,
		category).Scan(&cents)
	if err != nil {
		return 0, fmt.Errorf("sum expenses: %w", err)
	}
	return domain.Money(cents), nil
}

func (r *expenseRepo) TotalsByCategory(ctx context.Context, from, to time.Time) ([]domain.CategoryTotal, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT category, SUM(amount)::float8 FROM expenses WHERE date BETWEEN $1 AND $2 GROUP BY category ORDER BY category`,
		dayUTC(from), dayUTC(to))
	if err != nil {
		return nil, fmt.Errorf("report summary: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := []domain.CategoryTotal{}
	for rows.Next() {
		var c domain.CategoryTotal
		if err := rows.Scan(&c.Category, &c.Total); err != nil {
			return nil, fmt.Errorf("scan report row: %w", err)
		}
		result = append(result, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("report summary: %w", err)
	}
	return result, nil
}

func (r *expenseRepo) Categories(ctx context.Context, from, to time.Time) ([]string, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT DISTINCT category FROM expenses WHERE date BETWEEN $1 AND $2 ORDER BY category`,
		dayUTC(from), dayUTC(to))
	if err != nil {
		return nil, fmt.Errorf("report categories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, fmt.Errorf("scan report category: %w", err)
		}
		result = append(result, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("report categories: %w", err)
	}
	return result, nil
}

func (r *expenseRepo) SumByCategoryBetween(ctx context.Context, category string, from, to time.Time) (domain.Money, error) {
	var cents int64
	err := r.q.QueryRowContext(ctx,
		`SELECT (COALESCE(SUM(amount), 0) * 100)::bigint FROM expenses WHERE category = $1 AND date BETWEEN $2 AND $3`,
		category, dayUTC(from), dayUTC(to)).Scan(&cents)
	if err != nil {
		return 0, fmt.Errorf("report sum %q: %w", category, err)
	}
	return domain.Money(cents), nil
}
