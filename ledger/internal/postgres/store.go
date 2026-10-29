package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger/entity"
	"github.com/AndreyQuantum/golang-study-ms/ledger/repository"
)

const dateLayout = "2006-01-02"

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func money(x float64) string {
	return strconv.FormatFloat(math.Round(x*100)/100, 'f', 2, 64)
}

func dayUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func (s *Store) SetBudget(ctx context.Context, b entity.Budget) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO budgets(category, limit_amount) VALUES($1, $2::numeric)
		 ON CONFLICT(category) DO UPDATE SET limit_amount = EXCLUDED.limit_amount`,
		b.Category, money(b.Limit))
	if err != nil {
		return fmt.Errorf("set budget %q: %w", b.Category, err)
	}
	return nil
}

func (s *Store) ListBudgets(ctx context.Context) ([]entity.Budget, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT category, limit_amount::float8 FROM budgets ORDER BY category`)
	if err != nil {
		return nil, fmt.Errorf("list budgets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := []entity.Budget{}
	for rows.Next() {
		var b entity.Budget
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

func (s *Store) AddTransaction(ctx context.Context, t entity.Transaction) (entity.Transaction, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return entity.Transaction{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	amount := money(t.Amount)

	var limit string
	err = tx.QueryRowContext(ctx,
		`SELECT limit_amount::text FROM budgets WHERE category = $1 FOR UPDATE`,
		t.Category).Scan(&limit)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return entity.Transaction{}, fmt.Errorf("lock budget: %w", err)
	default:
		var spent string
		var exceeded bool
		err = tx.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(amount), 0)::text, COALESCE(SUM(amount), 0) + $2::numeric > $3::numeric
			 FROM expenses WHERE category = $1`,
			t.Category, amount, limit).Scan(&spent, &exceeded)
		if err != nil {
			return entity.Transaction{}, fmt.Errorf("check budget: %w", err)
		}
		if exceeded {
			return entity.Transaction{}, fmt.Errorf("%w: category %q, limit %s, spent %s, new %s",
				repository.ErrBudgetExceeded, t.Category, limit, spent, amount)
		}
	}

	var description sql.NullString
	if t.Description != "" {
		description = sql.NullString{String: t.Description, Valid: true}
	}
	var id int
	err = tx.QueryRowContext(ctx,
		`INSERT INTO expenses(amount, category, description, date) VALUES($1::numeric, $2, $3, $4) RETURNING id`,
		amount, t.Category, description, dayUTC(t.Date)).Scan(&id)
	if err != nil {
		return entity.Transaction{}, fmt.Errorf("insert expense: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return entity.Transaction{}, fmt.Errorf("commit: %w", err)
	}
	t.ID = id
	t.Amount = math.Round(t.Amount*100) / 100
	t.Date = dayUTC(t.Date)
	return t, nil
}

func (s *Store) ListTransactions(ctx context.Context) ([]entity.Transaction, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, amount::float8, category, description, date FROM expenses ORDER BY date DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := []entity.Transaction{}
	for rows.Next() {
		var t entity.Transaction
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

func (s *Store) ReportSummary(ctx context.Context, from, to time.Time) (entity.ReportSummary, error) {
	from, to = dayUTC(from), dayUTC(to)
	rows, err := s.db.QueryContext(ctx,
		`SELECT category, SUM(amount)::float8 FROM expenses WHERE date BETWEEN $1 AND $2 GROUP BY category ORDER BY category`,
		from, to)
	if err != nil {
		return entity.ReportSummary{}, fmt.Errorf("report summary: %w", err)
	}
	defer func() { _ = rows.Close() }()

	report := entity.ReportSummary{
		From:       from.Format(dateLayout),
		To:         to.Format(dateLayout),
		Categories: []entity.CategoryTotal{},
	}
	var totalCents int64
	for rows.Next() {
		var c entity.CategoryTotal
		if err := rows.Scan(&c.Category, &c.Total); err != nil {
			return entity.ReportSummary{}, fmt.Errorf("scan report row: %w", err)
		}
		cents := int64(math.Round(c.Total * 100))
		c.Total = float64(cents) / 100
		totalCents += cents
		report.Categories = append(report.Categories, c)
	}
	if err := rows.Err(); err != nil {
		return entity.ReportSummary{}, fmt.Errorf("report summary: %w", err)
	}
	report.Total = float64(totalCents) / 100
	return report, nil
}
