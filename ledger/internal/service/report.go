package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
)

const (
	// defaultHeartbeat - как часто расчёт отчёта сообщает в лог, что он ещё идёт.
	defaultHeartbeat = 400 * time.Millisecond
	// defaultMaxWorkers - сколько категорий считается одновременно: больше запросов, чем соединений в пуле, не нужно.
	defaultMaxWorkers = 8
)

// Option настраивает service.New.
type Option func(*service)

// WithHeartbeatInterval задаёт период heartbeat. d <= 0 игнорируется.
func WithHeartbeatInterval(d time.Duration) Option {
	return func(s *service) {
		if d > 0 {
			s.heartbeat = d
		}
	}
}

// WithLogf задаёт функцию лога. nil игнорируется.
func WithLogf(f func(format string, args ...any)) Option {
	return func(s *service) {
		if f != nil {
			s.logf = f
		}
	}
}

// WithMaxWorkers ограничивает число одновременных запросов отчёта к хранилищу. n < 1 игнорируется.
func WithMaxWorkers(n int) Option {
	return func(s *service) {
		if n >= 1 {
			s.maxWorkers = n
		}
	}
}

// GetReportSummary считает отчёт по категориям за интервал: сначала список категорий с расходами,
// затем по одной горутине на категорию (сумма за период), результаты сводятся после sync.WaitGroup.
//
// Каждая горутина пишет только в свой слот sums, общего map нет. Первая ошибка отменяет остальных,
// отмена ctx прерывает расчёт; в обоих случаях метод возвращает ошибку, в которой работает errors.Is
// (context.Canceled, context.DeadlineExceeded). Пока идёт расчёт, раз в heartbeat пишется строка в лог;
// горутина heartbeat гарантированно завершена к возврату из метода.
func (s *service) GetReportSummary(parent context.Context, from, to time.Time) (domain.ReportSummary, error) {
	if err := validateInterval(from, to); err != nil {
		return domain.ReportSummary{}, err
	}
	from, to = dayUTC(from), dayUTC(to)
	fromStr, toStr := from.Format(dateLayout), to.Format(dateLayout)

	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer s.startHeartbeat(ctx, fromStr, toStr)()

	// abort логирует причину и оборачивает её: если отменён родитель, возвращается его причина,
	// а не наведённый context.Canceled от derived ctx.
	abort := func(err error) (domain.ReportSummary, error) {
		if perr := parent.Err(); perr != nil {
			err = perr
		}
		s.logf("report: calculation aborted: %v", err)
		return domain.ReportSummary{}, fmt.Errorf("report %s..%s: %w", fromStr, toStr, err)
	}

	cats, err := s.repos.Expenses.Categories(ctx, from, to)
	if err != nil {
		return abort(err)
	}

	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
		sums     = make([]domain.Money, len(cats))
		sem      = make(chan struct{}, s.maxWorkers)
	)
	// fail запоминает первую ошибку и отменяет остальных воркеров; firstErr пишется только внутри Once.
	fail := func(err error) {
		once.Do(func() {
			firstErr = err
			cancel()
		})
	}
	for i, c := range cats {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				fail(ctx.Err())
				return
			}
			if err := ctx.Err(); err != nil {
				fail(err)
				return
			}
			m, err := s.repos.Expenses.SumByCategoryBetween(ctx, c, from, to)
			if err != nil {
				fail(err)
				return
			}
			sums[i] = m
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return abort(firstErr)
	}

	report := domain.ReportSummary{
		From:       fromStr,
		To:         toStr,
		Categories: make([]domain.CategoryTotal, 0, len(cats)),
	}
	var total domain.Money
	for i, c := range cats {
		report.Categories = append(report.Categories, domain.CategoryTotal{Category: c, Total: sums[i].Float()})
		total += sums[i]
	}
	report.Total = total.Float()
	return report, nil
}

// startHeartbeat запускает горутину, которая раз в s.heartbeat пишет в лог, что расчёт идёт.
// Возвращаемая stop останавливает тикер и ждёт завершения горутины, поэтому после stop она не живёт.
// Горутина читает только ctx, done и свои копии строк дат.
func (s *service) startHeartbeat(ctx context.Context, from, to string) (stop func()) {
	done := make(chan struct{})
	stopped := make(chan struct{})
	ticker := time.NewTicker(s.heartbeat)
	go func() {
		defer close(stopped)
		defer s.logf("report: heartbeat stopped")
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.logf("report: calculating %s..%s", from, to)
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}
