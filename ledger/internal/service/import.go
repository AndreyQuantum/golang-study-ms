package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
)

type importJob struct {
	index int
	tx    domain.Transaction
}

// importResult - итог по одному элементу; err == nil означает, что транзакция принята.
type importResult struct {
	index int
	err   error
}

// ImportTransactions добавляет пакет транзакций пулом из workers горутин.
//
// Конвейер: продюсер кладёт задания в канал jobs и закрывает его; workers воркеров читают jobs и для каждого элемента
// вызывают AddTransaction (валидация, правило бюджета и вставка в одной транзакции WithinTx, отдельной копии логики нет);
// результаты уходят в канал results; закрывающая горутина после wg.Wait() закрывает results; отдельная горутина-коллектор
// читает results до закрытия и собирает сводку. Счётчики принятых и отклонённых - atomic.Int64, срез ошибок трогает только
// коллектор. К возврату все горутины завершены.
//
// Каждый элемент обрабатывается независимо: отказ одного не останавливает остальные, пакет целиком не откатывается.
// Если ctx отменён или истёк до обработки всех элементов, возвращается частичная сводка и ошибка, оборачивающая ctx.Err().
// Необработанные и прерванные отменой элементы не входят ни в Accepted, ни в Rejected. Если отмена пришла уже после
// обработки всех элементов, ошибки нет. Ошибки в сводке отсортированы по индексу элемента в исходном срезе.
func (s *service) ImportTransactions(ctx context.Context, txs []domain.Transaction, workers int) (domain.ImportSummary, error) {
	empty := domain.ImportSummary{Errors: []domain.ImportError{}}
	if workers < 1 || workers > domain.MaxImportWorkers {
		return empty, &domain.ValidationError{Field: "workers",
			Message: fmt.Sprintf("workers must be an integer between 1 and %d", domain.MaxImportWorkers)}
	}
	if len(txs) > domain.MaxImportItems {
		return empty, &domain.ValidationError{Field: "transactions",
			Message: fmt.Sprintf("too many transactions: at most %d per request", domain.MaxImportItems)}
	}
	if len(txs) == 0 {
		return empty, nil
	}
	if err := ctx.Err(); err != nil {
		return empty, fmt.Errorf("import not started: %w", err)
	}

	jobs := make(chan importJob)
	results := make(chan importResult, workers)

	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		defer close(jobs)
		for i, tx := range txs {
			select {
			case jobs <- importJob{index: i, tx: tx}:
			case <-ctx.Done():
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}
				_, err := s.AddTransaction(ctx, job.tx)
				if err != nil && ctx.Err() != nil && !isDomainError(err) {
					continue // прервано отменой: элемент не считается ни принятым, ни отклонённым
				}
				results <- importResult{index: job.index, err: err}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var accepted, rejected atomic.Int64
	var errs []domain.ImportError // принадлежит только коллектору до закрытия collectorDone
	collectorDone := make(chan struct{})
	go func() {
		defer close(collectorDone)
		for r := range results {
			if r.err == nil {
				accepted.Add(1)
				continue
			}
			rejected.Add(1)
			errs = append(errs, domain.ImportError{Index: r.index, Err: r.err})
		}
	}()

	<-collectorDone
	<-producerDone

	if errs == nil {
		errs = []domain.ImportError{}
	}
	slices.SortFunc(errs, func(a, b domain.ImportError) int { return cmp.Compare(a.Index, b.Index) })
	summary := domain.ImportSummary{Accepted: int(accepted.Load()), Rejected: int(rejected.Load()), Errors: errs}

	if processed := summary.Accepted + summary.Rejected; processed < len(txs) {
		return summary, fmt.Errorf("import interrupted after %d of %d transactions: %w", processed, len(txs), ctx.Err())
	}
	return summary, nil
}

// isDomainError - отказ по правилам предметной области (а не сбой из-за отмены ctx).
func isDomainError(err error) bool {
	var ve *domain.ValidationError
	return errors.Is(err, domain.ErrBudgetExceeded) || errors.As(err, &ve)
}
