package ledger

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger/entity"
	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/cache"
	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/db"
	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/postgres"
	"github.com/AndreyQuantum/golang-study-ms/ledger/repository"
)

type (
	Transaction     = entity.Transaction
	Budget          = entity.Budget
	Validatable     = entity.Validatable
	ValidationError = entity.ValidationError
	ReportSummary   = entity.ReportSummary
	CategoryTotal   = entity.CategoryTotal
)

var ErrBudgetExceeded = repository.ErrBudgetExceeded

const opTimeout = 5 * time.Second

var (
	mu      sync.RWMutex
	store   Store = newMemoryStore()
	closers []func() error
)

func current() Store {
	mu.RLock()
	defer mu.RUnlock()
	return store
}

func opContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), opTimeout)
}

func Init(ctx context.Context) error {
	dsn := db.DSNFromEnv()
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	log.Printf("connected to PostgreSQL (%s)", db.Redact(dsn))

	cfg, err := cache.ConfigFromEnv()
	if err != nil {
		_ = pool.Close()
		return fmt.Errorf("redis config: %w", err)
	}
	c, err := cache.New(ctx, cfg)
	if err != nil {
		_ = pool.Close()
		return fmt.Errorf("connect to Redis: %w", err)
	}
	log.Printf("connected to Redis (%s, db %d)", cfg.Addr, cfg.DB)

	mu.Lock()
	defer mu.Unlock()
	for _, closeFn := range closers {
		if err := closeFn(); err != nil {
			log.Printf("close previous resources: %v", err)
		}
	}
	store = newCachedStore(postgres.New(pool), c)
	closers = []func() error{c.Close, pool.Close}
	return nil
}

func Close() error {
	mu.Lock()
	defer mu.Unlock()
	var errs []error
	for _, closeFn := range closers {
		if err := closeFn(); err != nil {
			errs = append(errs, err)
		}
	}
	closers = nil
	store = newMemoryStore()
	return errors.Join(errs...)
}

func SetBudget(b Budget) error {
	if err := b.Validate(); err != nil {
		return err
	}
	ctx, cancel := opContext()
	defer cancel()
	return current().SetBudget(ctx, b)
}

func ListBudgets() ([]Budget, error) {
	ctx, cancel := opContext()
	defer cancel()
	return current().ListBudgets(ctx)
}

func AddTransaction(tx Transaction) (Transaction, error) {
	if err := tx.Validate(); err != nil {
		return Transaction{}, err
	}
	ctx, cancel := opContext()
	defer cancel()
	return current().AddTransaction(ctx, tx)
}

func ListTransactions() ([]Transaction, error) {
	ctx, cancel := opContext()
	defer cancel()
	return current().ListTransactions(ctx)
}

func GetReportSummary(ctx context.Context, from, to time.Time) (ReportSummary, error) {
	if from.IsZero() {
		return ReportSummary{}, &ValidationError{Field: "from", Message: "from must be set"}
	}
	if to.IsZero() {
		return ReportSummary{}, &ValidationError{Field: "to", Message: "to must be set"}
	}
	if dayUTC(from).After(dayUTC(to)) {
		return ReportSummary{}, &ValidationError{Field: "from", Message: "from must not be after to"}
	}
	return current().ReportSummary(ctx, from, to)
}

func CheckValid(v Validatable) error {
	return entity.CheckValid(v)
}

func Reset() {
	mu.Lock()
	defer mu.Unlock()
	store = newMemoryStore()
}
