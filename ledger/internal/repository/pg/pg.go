// Package pg - реализация domain.Repos и domain.TxManager на PostgreSQL.
// Здесь только SQL: никакой бизнес-логики и HTTP.
package pg

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
)

// dbtx - то, что умеют и *sql.DB, и *sql.Tx; репозитории работают с любым из них.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Store struct {
	db *sql.DB
}

var _ domain.TxManager = (*Store)(nil)

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// Repos возвращает репозитории, привязанные к пулу соединений (без общей транзакции).
func (s *Store) Repos() domain.Repos {
	return reposOn(s.db)
}

func reposOn(q dbtx) domain.Repos {
	return domain.Repos{Budgets: &budgetRepo{q: q}, Expenses: &expenseRepo{q: q}}
}

// WithinTx выполняет fn в одной транзакции БД (READ COMMITTED). Ошибка fn откатывает транзакцию
// и возвращается как есть.
func (s *Store) WithinTx(ctx context.Context, fn func(ctx context.Context, repos domain.Repos) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(ctx, reposOn(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func money(x float64) string {
	return strconv.FormatFloat(math.Round(x*100)/100, 'f', 2, 64)
}

func dayUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
