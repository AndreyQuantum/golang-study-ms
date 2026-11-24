package service

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/repository/memory"
)

func importTxs(n int, category string, amount float64) []domain.Transaction {
	txs := make([]domain.Transaction, n)
	for i := range txs {
		txs[i] = domain.Transaction{Amount: amount, Category: category, Date: day(10, 1)}
	}
	return txs
}

func TestImportArgumentValidation(t *testing.T) {
	cases := []struct {
		name    string
		txs     []domain.Transaction
		workers int
		field   string
		message string
	}{
		{"zero workers", importTxs(1, "food", 1), 0, "workers", "workers must be an integer between 1 and 32"},
		{"negative workers", importTxs(1, "food", 1), -1, "workers", "workers must be an integer between 1 and 32"},
		{"too many workers", importTxs(1, "food", 1), 33, "workers", "workers must be an integer between 1 and 32"},
		{"too many items", importTxs(1001, "food", 1), 4, "transactions", "too many transactions: at most 1000 per request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sp := newSpy()
			summary, err := sp.service().ImportTransactions(context.Background(), tc.txs, tc.workers)
			var ve *domain.ValidationError
			require.ErrorAs(t, err, &ve)
			assert.Equal(t, tc.field, ve.Field)
			assert.Equal(t, tc.message, ve.Message)
			assert.Empty(t, sp.names())
			assert.Zero(t, summary.Accepted+summary.Rejected)
			assert.NotNil(t, summary.Errors)
		})
	}
}

func TestImportEmpty(t *testing.T) {
	svc := newService()
	for _, txs := range [][]domain.Transaction{nil, {}} {
		summary, err := svc.ImportTransactions(context.Background(), txs, 4)
		require.NoError(t, err)
		assert.Equal(t, domain.ImportSummary{Errors: []domain.ImportError{}}, summary)
	}
}

func TestImportCanceledBeforeStart(t *testing.T) {
	sp := newSpy()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	summary, err := sp.service().ImportTransactions(ctx, importTxs(5, "food", 1), 4)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
	assert.Zero(t, summary.Accepted+summary.Rejected)
	assert.NotNil(t, summary.Errors)
	assert.Empty(t, sp.names())
}

func TestImportAcceptsAll(t *testing.T) {
	for _, workers := range []int{1, 4, 32} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			svc := newService()
			txs := importTxs(50, "food", 10.005)
			for i := range txs {
				txs[i].Date = time.Date(2026, 10, 1, 15, 30, 0, 0, time.UTC)
			}
			summary, err := svc.ImportTransactions(context.Background(), txs, workers)
			require.NoError(t, err)
			assert.Equal(t, 50, summary.Accepted)
			assert.Zero(t, summary.Rejected)
			assert.NotNil(t, summary.Errors)
			assert.Empty(t, summary.Errors)

			stored, err := svc.ListTransactions(context.Background())
			require.NoError(t, err)
			require.Len(t, stored, 50)
			for _, s := range stored {
				assert.Equal(t, 10.01, s.Amount, "сумма округляется до копеек")
				assert.Equal(t, day(10, 1), s.Date, "дата усечена до дня UTC")
			}
		})
	}
}

func TestImportMoreWorkersThanItems(t *testing.T) {
	svc := newService()
	summary, err := svc.ImportTransactions(context.Background(), importTxs(3, "food", 5), 32)
	require.NoError(t, err)
	assert.Equal(t, 3, summary.Accepted)
	stored, _ := svc.ListTransactions(context.Background())
	assert.Len(t, stored, 3)
}

func TestImportSingleWorkerKeepsOrder(t *testing.T) {
	svc := newService()
	txs := importTxs(10, "food", 1)
	for i := range txs {
		txs[i].Description = fmt.Sprintf("item-%d", i)
	}
	_, err := svc.ImportTransactions(context.Background(), txs, 1)
	require.NoError(t, err)
	stored, _ := svc.ListTransactions(context.Background())
	require.Len(t, stored, 10)
	for i, s := range stored {
		assert.Equal(t, i+1, s.ID)
		assert.Equal(t, fmt.Sprintf("item-%d", i), s.Description)
	}
}

func TestImportRejectsInvalid(t *testing.T) {
	svc := newService()
	txs := importTxs(6, "food", 5)
	txs[1].Amount = -1
	txs[4].Category = ""
	summary, err := svc.ImportTransactions(context.Background(), txs, 4)
	require.NoError(t, err)
	assert.Equal(t, 4, summary.Accepted)
	assert.Equal(t, 2, summary.Rejected)
	require.Len(t, summary.Errors, 2)
	assert.Equal(t, 1, summary.Errors[0].Index)
	assert.Equal(t, 4, summary.Errors[1].Index)
	var ve *domain.ValidationError
	require.ErrorAs(t, summary.Errors[0], &ve)
	assert.Equal(t, "amount must be positive", ve.Message)
	require.ErrorAs(t, summary.Errors[1], &ve)
	assert.Equal(t, "category must not be empty", ve.Message)
}

func TestImportErrorsSortedByIndex(t *testing.T) {
	svc := newService()
	txs := importTxs(100, "food", -1) // все невалидны, воркеры завершают в произвольном порядке
	summary, err := svc.ImportTransactions(context.Background(), txs, 16)
	require.NoError(t, err)
	require.Len(t, summary.Errors, 100)
	for i, e := range summary.Errors {
		assert.Equal(t, i, e.Index)
	}
}

func TestImportBudgetExceededDeterministic(t *testing.T) {
	svc := newService()
	ctx := context.Background()
	require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "food", Limit: 25}))
	summary, err := svc.ImportTransactions(ctx, importTxs(5, "food", 10), 1)
	require.NoError(t, err)
	assert.Equal(t, 2, summary.Accepted)
	assert.Equal(t, 3, summary.Rejected)
	require.Len(t, summary.Errors, 3)
	for i, e := range summary.Errors {
		assert.Equal(t, i+2, e.Index)
		assert.ErrorIs(t, e, domain.ErrBudgetExceeded)
	}
}

func TestImportInternalErrorIsRejected(t *testing.T) {
	st := memory.New()
	svc := New(st.Repos(), categoryFailer{TxManager: st, bad: "broken"})
	txs := importTxs(4, "food", 5)
	txs[2].Category = "broken"
	summary, err := svc.ImportTransactions(context.Background(), txs, 3)
	require.NoError(t, err)
	assert.Equal(t, 3, summary.Accepted)
	require.Len(t, summary.Errors, 1)
	assert.Equal(t, 2, summary.Errors[0].Index)
	assert.EqualError(t, summary.Errors[0].Err, "boom")
}

// categoryFailer возвращает ошибку хранилища для транзакций заданной категории (категорию видно в repos через Add).
type categoryFailer struct {
	domain.TxManager
	bad string
}

func (c categoryFailer) WithinTx(ctx context.Context, fn func(context.Context, domain.Repos) error) error {
	return c.TxManager.WithinTx(ctx, func(ctx context.Context, r domain.Repos) error {
		r.Expenses = failingAdd{ExpenseRepository: r.Expenses, bad: c.bad}
		return fn(ctx, r)
	})
}

type failingAdd struct {
	domain.ExpenseRepository
	bad string
}

func (f failingAdd) Add(ctx context.Context, t domain.Transaction) (domain.Transaction, error) {
	if t.Category == f.bad {
		return domain.Transaction{}, errors.New("boom")
	}
	return f.ExpenseRepository.Add(ctx, t)
}

// blockingTx - domain.TxManager: первые pass вызовов WithinTx идут в memory, остальные сообщают в entered
// и ждут отмены ctx, после чего возвращают ctx.Err(). Так отмена посреди импорта детерминирована, без sleep.
type blockingTx struct {
	inner   *memory.Store
	pass    int32
	entered chan struct{}
	calls   atomic.Int32
}

func newBlockingTx(pass int) *blockingTx {
	return &blockingTx{inner: memory.New(), pass: int32(pass), entered: make(chan struct{}, domain.MaxImportItems)}
}

func (b *blockingTx) service() Service { return New(b.inner.Repos(), b) }

func (b *blockingTx) WithinTx(ctx context.Context, fn func(context.Context, domain.Repos) error) error {
	if b.calls.Add(1) <= b.pass {
		return b.inner.WithinTx(ctx, fn)
	}
	b.entered <- struct{}{}
	<-ctx.Done()
	return ctx.Err()
}

func (b *blockingTx) waitEntered(t *testing.T, n int) {
	t.Helper()
	for range n {
		select {
		case <-b.entered:
		case <-time.After(guard):
			t.Fatal("воркеры не дошли до хранилища")
		}
	}
}

// expiredCtx - контекст, дедлайн которого наступает по команде: expire закрывает Done, после чего Err() == DeadlineExceeded.
// Нужен, чтобы проверять отмену по дедлайну без таймеров и sleep.
type expiredCtx struct {
	context.Context
	done chan struct{}
}

func newExpiredCtx() (*expiredCtx, func()) {
	c := &expiredCtx{Context: context.Background(), done: make(chan struct{})}
	var once sync.Once
	return c, func() { once.Do(func() { close(c.done) }) }
}

func (c *expiredCtx) Done() <-chan struct{} { return c.done }

func (c *expiredCtx) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// startImport запускает импорт в горутине и возвращает функцию ожидания результата (не дольше guard).
// wait вызывается только из горутины теста, поэтому t.Fatal в ней допустим.
func startImport(svc Service, ctx context.Context, txs []domain.Transaction, workers int) func(t *testing.T) (domain.ImportSummary, error) {
	type res struct {
		s   domain.ImportSummary
		err error
	}
	ch := make(chan res, 1)
	go func() {
		s, err := svc.ImportTransactions(ctx, txs, workers)
		ch <- res{s, err}
	}()
	return func(t *testing.T) (domain.ImportSummary, error) {
		t.Helper()
		select {
		case r := <-ch:
			return r.s, r.err
		case <-time.After(guard):
			t.Fatal("ImportTransactions не вернулся после отмены")
			return domain.ImportSummary{}, nil
		}
	}
}

func TestImportCancelMidImport(t *testing.T) {
	cases := []struct {
		name   string
		cause  error
		newCtx func() (context.Context, func())
	}{
		{"canceled", context.Canceled, func() (context.Context, func()) {
			return context.WithCancel(context.Background())
		}},
		{"deadline", context.DeadlineExceeded, func() (context.Context, func()) {
			return newExpiredCtx()
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bt := newBlockingTx(0)
			ctx, stop := tc.newCtx()
			defer stop()
			wait := startImport(bt.service(), ctx, importTxs(10, "food", 1), 2)
			bt.waitEntered(t, 2)
			stop()
			summary, err := wait(t)
			require.Error(t, err)
			assert.ErrorIs(t, err, tc.cause)
			assert.Zero(t, summary.Accepted+summary.Rejected, "прерванные элементы не считаются")
			assert.NotNil(t, summary.Errors)
			assert.EqualValues(t, 2, bt.calls.Load(), "воркеры не берут новые задания после отмены")
		})
	}
}

func TestImportCancelPartialSummary(t *testing.T) {
	bt := newBlockingTx(3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wait := startImport(bt.service(), ctx, importTxs(10, "food", 1), 1)
	bt.waitEntered(t, 1) // четвёртый элемент заблокирован
	cancel()
	summary, err := wait(t)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 3, summary.Accepted)
	assert.Zero(t, summary.Rejected)
	assert.Empty(t, summary.Errors)
	assert.NotNil(t, summary.Errors)
	stored, lerr := bt.inner.Repos().Expenses.List(context.Background())
	require.NoError(t, lerr)
	assert.Len(t, stored, 3)
}

func TestImportProducerStopsOnCancel(t *testing.T) {
	bt := newBlockingTx(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wait := startImport(bt.service(), ctx, importTxs(1000, "food", 1), 1)
	bt.waitEntered(t, 1)
	cancel()
	_, err := wait(t)
	assert.ErrorIs(t, err, context.Canceled)
	assert.EqualValues(t, 1, bt.calls.Load())
}

// Тест без t.Parallel(): сравнивает число горутин процесса до и после.
func TestImportNoGoroutineLeak(t *testing.T) {
	scenarios := map[string]func(t *testing.T){
		"success": func(t *testing.T) {
			_, err := newService().ImportTransactions(context.Background(), importTxs(50, "food", 1), 8)
			require.NoError(t, err)
		},
		"cancel mid import": func(t *testing.T) {
			bt := newBlockingTx(2)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _ = bt.service().ImportTransactions(ctx, importTxs(100, "food", 1), 4)
			}()
			bt.waitEntered(t, 2)
			cancel()
			<-done
		},
		"empty": func(t *testing.T) {
			_, err := newService().ImportTransactions(context.Background(), nil, 4)
			require.NoError(t, err)
		},
		"bad arguments": func(t *testing.T) {
			_, err := newService().ImportTransactions(context.Background(), importTxs(1, "food", 1), 0)
			require.Error(t, err)
		},
	}
	for name, run := range scenarios {
		t.Run(name, func(t *testing.T) {
			before := runtime.NumGoroutine()
			run(t)
			deadline := time.Now().Add(guard)
			for runtime.NumGoroutine() > before {
				if time.Now().After(deadline) {
					t.Fatalf("утечка горутин: было %d, стало %d", before, runtime.NumGoroutine())
				}
				runtime.Gosched()
			}
		})
	}
}

func TestImportConcurrentRespectsLimit(t *testing.T) {
	for range 20 {
		svc := newService()
		ctx := context.Background()
		require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "food", Limit: 100}))
		summary, err := svc.ImportTransactions(ctx, importTxs(20, "food", 10), 8)
		require.NoError(t, err)
		assert.Equal(t, 10, summary.Accepted)
		assert.Equal(t, 10, summary.Rejected)
		for _, e := range summary.Errors {
			assert.ErrorIs(t, e, domain.ErrBudgetExceeded)
		}
		stored, err := svc.ListTransactions(ctx)
		require.NoError(t, err)
		var sum float64
		for _, s := range stored {
			sum += s.Amount
		}
		assert.Equal(t, 100.0, sum)
	}
}

func TestImportMixedCategories(t *testing.T) {
	svc := newService()
	ctx := context.Background()
	require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "food", Limit: 50}))
	require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "taxi", Limit: 30}))
	var txs []domain.Transaction
	txs = append(txs, importTxs(10, "food", 10)...)  // принято 5
	txs = append(txs, importTxs(10, "taxi", 10)...)  // принято 3
	txs = append(txs, importTxs(10, "games", 10)...) // без лимита, принято 10
	summary, err := svc.ImportTransactions(ctx, txs, 32)
	require.NoError(t, err)
	assert.Equal(t, 18, summary.Accepted)
	assert.Equal(t, 12, summary.Rejected)
	for _, e := range summary.Errors {
		assert.ErrorIs(t, e, domain.ErrBudgetExceeded)
		assert.Less(t, e.Index, 20, "категория games лимита не имеет")
	}
}
