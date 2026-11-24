package service

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/repository/memory"
)

// guard - страховка от зависания теста, а не условие успеха.
const guard = 5 * time.Second

var errBoom = errors.New("boom")

type probeMode int

const (
	modePass    probeMode = iota // сразу считать на данных memory
	modeRelease                  // ждать закрытия release (или отмены ctx)
	modeCtx                      // ждать только отмены ctx и вернуть ctx.Err()
)

// probeExpenses подменяет SumByCategoryBetween: считает активные вызовы и умеет блокироваться.
// Остальные методы берутся у memory-репозитория.
type probeExpenses struct {
	domain.ExpenseRepository
	mode    probeMode
	release chan struct{}
	entered chan string // по сообщению на каждый зашедший вызов; отправка неблокирующая

	failCat     string // категория, на которой вызов возвращает failErr
	failErr     error
	failWaitFor int // сколько сообщений entered прочитать перед ошибкой

	calls        atomic.Int32
	active       atomic.Int32
	maxActive    atomic.Int32
	canceledSeen atomic.Int32
}

func (p *probeExpenses) SumByCategoryBetween(ctx context.Context, category string, from, to time.Time) (domain.Money, error) {
	p.calls.Add(1)
	n := p.active.Add(1)
	defer p.active.Add(-1)
	for {
		m := p.maxActive.Load()
		if n <= m || p.maxActive.CompareAndSwap(m, n) {
			break
		}
	}

	if p.failErr != nil && category == p.failCat {
		for range p.failWaitFor {
			select {
			case <-p.entered:
			case <-time.After(guard):
				return 0, errors.New("probe: воркеры не зашли в репозиторий")
			}
		}
		return 0, p.failErr
	}

	select {
	case p.entered <- category:
	default:
	}
	switch p.mode {
	case modeRelease:
		select {
		case <-p.release:
		case <-ctx.Done():
			p.canceledSeen.Add(1)
			return 0, ctx.Err()
		}
	case modeCtx:
		<-ctx.Done()
		p.canceledSeen.Add(1)
		return 0, ctx.Err()
	}
	return p.ExpenseRepository.SumByCategoryBetween(ctx, category, from, to)
}

// lockedLog - потокобезопасный logf: вся история под mutex, в канал строки уходят неблокирующе.
type lockedLog struct {
	mu    sync.Mutex
	lines []string
	ch    chan string
}

func newLockedLog() *lockedLog { return &lockedLog{ch: make(chan string, 256)} }

func (l *lockedLog) logf(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	l.mu.Lock()
	l.lines = append(l.lines, s)
	l.mu.Unlock()
	select {
	case l.ch <- s:
	default:
	}
}

func (l *lockedLog) history() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

func (l *lockedLog) count(sub string) int {
	n := 0
	for _, s := range l.history() {
		if strings.Contains(s, sub) {
			n++
		}
	}
	return n
}

// newProbe создаёт сервис поверх memory: по одному расходу в каждой категории (сумма i+1) за 10 октября.
func newProbe(t *testing.T, cats []string, mode probeMode, opts ...Option) (Service, *probeExpenses, *lockedLog) {
	t.Helper()
	st := memory.New()
	repos := st.Repos()
	for i, c := range cats {
		_, err := repos.Expenses.Add(context.Background(), domain.Transaction{Amount: float64(i + 1), Category: c, Date: day(10, 10)})
		require.NoError(t, err)
	}
	p := &probeExpenses{
		ExpenseRepository: repos.Expenses,
		mode:              mode,
		release:           make(chan struct{}),
		entered:           make(chan string, len(cats)),
	}
	lg := newLockedLog()
	opts = append([]Option{WithLogf(lg.logf)}, opts...)
	return New(domain.Repos{Budgets: repos.Budgets, Expenses: p}, st, opts...), p, lg
}

type reportResult struct {
	report domain.ReportSummary
	err    error
}

func startReport(ctx context.Context, svc Service, from, to time.Time) <-chan reportResult {
	ch := make(chan reportResult, 1)
	go func() {
		r, err := svc.GetReportSummary(ctx, from, to)
		ch <- reportResult{r, err}
	}()
	return ch
}

func waitResult(t *testing.T, ch <-chan reportResult) reportResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(guard):
		t.Fatal("GetReportSummary не вернулся")
		return reportResult{}
	}
}

func waitEntered(t *testing.T, p *probeExpenses, n int) {
	t.Helper()
	for range n {
		select {
		case <-p.entered:
		case <-time.After(guard):
			t.Fatal("воркер не зашёл в репозиторий")
		}
	}
}

func catNames(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("cat%02d", i)
	}
	return out
}

func expectedProbeReport(cats []string) domain.ReportSummary {
	rep := domain.ReportSummary{From: "2026-10-01", To: "2026-10-31", Categories: []domain.CategoryTotal{}}
	var total domain.Money
	for i, c := range cats {
		m := domain.MoneyFromFloat(float64(i + 1))
		rep.Categories = append(rep.Categories, domain.CategoryTotal{Category: c, Total: m.Float()})
		total += m
	}
	rep.Total = total.Float()
	return rep
}

func TestReportMatchesTotalsByCategory(t *testing.T) {
	st := memory.New()
	svc := New(st.Repos(), st)
	ctx := context.Background()
	for _, tx := range []domain.Transaction{
		{Amount: 100.10, Category: "food", Date: day(10, 1)},
		{Amount: 50.20, Category: "food", Date: day(10, 10)},
		{Amount: 30, Category: "transport", Date: day(10, 5)},
		{Amount: 0.1, Category: "books", Date: day(10, 6)},
		{Amount: 0.2, Category: "books", Date: day(10, 7)},
		{Amount: 999, Category: "food", Date: day(9, 30)},
		{Amount: 999, Category: "health", Date: day(10, 11)},
	} {
		_, err := svc.AddTransaction(ctx, tx)
		require.NoError(t, err)
	}

	got, err := svc.GetReportSummary(ctx, day(10, 1), day(10, 10))
	require.NoError(t, err)
	assert.Equal(t, domain.ReportSummary{
		From:  "2026-10-01",
		To:    "2026-10-10",
		Total: 180.60,
		Categories: []domain.CategoryTotal{
			{Category: "books", Total: 0.3},
			{Category: "food", Total: 150.30},
			{Category: "transport", Total: 30},
		},
	}, got)

	// эталон - один запрос с GROUP BY
	oracle, err := st.Repos().Expenses.TotalsByCategory(ctx, day(10, 1), day(10, 10))
	require.NoError(t, err)
	assert.Equal(t, oracle, got.Categories)
}

func TestReportEmptyAndSingleDay(t *testing.T) {
	svc, _, _ := newProbe(t, nil, modePass)
	got, err := svc.GetReportSummary(context.Background(), day(10, 1), day(10, 31))
	require.NoError(t, err)
	assert.NotNil(t, got.Categories)
	assert.Empty(t, got.Categories)
	assert.Equal(t, 0.0, got.Total)

	svc, _, _ = newProbe(t, []string{"a", "b"}, modePass)
	got, err = svc.GetReportSummary(context.Background(), day(10, 10), day(10, 10).Add(5*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 3.0, got.Total)
	assert.Len(t, got.Categories, 2)
}

func TestReportOneGoroutinePerCategory(t *testing.T) {
	cats := catNames(5)
	svc, p, _ := newProbe(t, cats, modeRelease, WithMaxWorkers(5))
	ch := startReport(context.Background(), svc, day(10, 1), day(10, 31))

	// все пять вызовов зашли до отпускания: расчёт параллельный
	waitEntered(t, p, 5)
	assert.EqualValues(t, 5, p.active.Load())
	close(p.release)

	r := waitResult(t, ch)
	require.NoError(t, r.err)
	assert.Equal(t, expectedProbeReport(cats), r.report)
	assert.EqualValues(t, 5, p.calls.Load())
}

func TestReportRespectsMaxWorkers(t *testing.T) {
	cats := catNames(10)
	svc, p, _ := newProbe(t, cats, modeRelease, WithMaxWorkers(3))
	ch := startReport(context.Background(), svc, day(10, 1), day(10, 31))

	waitEntered(t, p, 3)
	select {
	case c := <-p.entered:
		t.Fatalf("категория %q зашла сверх лимита воркеров", c)
	case <-time.After(20 * time.Millisecond):
	}
	assert.EqualValues(t, 3, p.active.Load())
	close(p.release)

	r := waitResult(t, ch)
	require.NoError(t, r.err)
	assert.Equal(t, expectedProbeReport(cats), r.report)
	assert.EqualValues(t, 3, p.maxActive.Load(), "лимит достигнут, но не превышен")
}

func TestReportCancelStopsWorkers(t *testing.T) {
	svc, p, lg := newProbe(t, catNames(3), modeCtx)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := startReport(ctx, svc, day(10, 1), day(10, 31))

	waitEntered(t, p, 3)
	cancel()
	r := waitResult(t, ch)
	require.ErrorIs(t, r.err, context.Canceled)
	assert.EqualValues(t, 0, p.active.Load())
	assert.Equal(t, 1, lg.count("report: calculation aborted"))
	assert.Equal(t, 1, lg.count("report: heartbeat stopped"))
}

func TestReportDeadlineExceeded(t *testing.T) {
	svc, p, _ := newProbe(t, catNames(3), modeCtx)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	r := waitResult(t, startReport(ctx, svc, day(10, 1), day(10, 31)))
	require.ErrorIs(t, r.err, context.DeadlineExceeded)
	assert.NotErrorIs(t, r.err, context.Canceled)
	assert.EqualValues(t, 0, p.active.Load())
}

func TestReportFirstErrorCancelsOthers(t *testing.T) {
	const n = 4
	cats := catNames(n)
	svc, p, lg := newProbe(t, cats, modeCtx, WithMaxWorkers(n))
	p.failCat, p.failErr, p.failWaitFor = cats[1], errBoom, n-1

	r := waitResult(t, startReport(context.Background(), svc, day(10, 1), day(10, 31)))
	require.ErrorIs(t, r.err, errBoom)
	assert.NotErrorIs(t, r.err, context.Canceled)
	assert.EqualValues(t, 0, p.active.Load())
	assert.EqualValues(t, n-1, p.canceledSeen.Load())
	assert.Equal(t, 1, lg.count("report: calculation aborted: "))
}

func TestReportCanceledBeforeStart(t *testing.T) {
	svc, p, _ := newProbe(t, catNames(3), modePass)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.GetReportSummary(ctx, day(10, 1), day(10, 31))
	require.ErrorIs(t, err, context.Canceled)
	assert.EqualValues(t, 0, p.calls.Load())
}

func TestReportHeartbeatTicks(t *testing.T) {
	svc, p, lg := newProbe(t, catNames(2), modeRelease, WithHeartbeatInterval(5*time.Millisecond))
	ch := startReport(context.Background(), svc, day(10, 1), day(10, 31))

	ticks := 0
	for ticks < 3 {
		select {
		case s := <-lg.ch:
			if s == "report: calculating 2026-10-01..2026-10-31" {
				ticks++
			}
		case <-time.After(guard):
			t.Fatal("heartbeat не тикает")
		}
	}
	close(p.release)
	r := waitResult(t, ch)
	require.NoError(t, r.err)

	// метод дождался горутины heartbeat: "stopped" ровно одна и последняя, после неё строк нет
	hist := lg.history()
	require.NotEmpty(t, hist)
	assert.Equal(t, 1, lg.count("report: heartbeat stopped"))
	assert.Equal(t, "report: heartbeat stopped", hist[len(hist)-1])
	assert.GreaterOrEqual(t, lg.count("report: calculating 2026-10-01..2026-10-31"), 3)
}

func TestReportNoGoroutineLeak(t *testing.T) {
	// без t.Parallel(): считаем горутины процесса
	before := runtime.NumGoroutine()
	for i := range 50 {
		svc, _, _ := newProbe(t, catNames(3), modePass, WithHeartbeatInterval(time.Millisecond))
		_, err := svc.GetReportSummary(context.Background(), day(10, 1), day(10, 31))
		require.NoError(t, err)

		svc, p, _ := newProbe(t, catNames(3), modeCtx, WithHeartbeatInterval(time.Millisecond))
		ctx, cancel := context.WithCancel(context.Background())
		ch := startReport(ctx, svc, day(10, 1), day(10, 31))
		waitEntered(t, p, 1+i%3)
		cancel()
		require.ErrorIs(t, waitResult(t, ch).err, context.Canceled)
	}

	for range 100 {
		if runtime.NumGoroutine() <= before {
			return
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	var dump strings.Builder
	_ = pprof.Lookup("goroutine").WriteTo(&dump, 1)
	t.Fatalf("горутины не вернулись к исходному числу %d (сейчас %d):\n%s", before, runtime.NumGoroutine(), dump.String())
}

func TestReportIsRaceFree(t *testing.T) {
	st := memory.New()
	svc := New(st.Repos(), st, WithHeartbeatInterval(time.Millisecond), WithLogf(func(string, ...any) {}))
	for i, c := range catNames(6) {
		_, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: float64(i + 1), Category: c, Date: day(10, 10)})
		require.NoError(t, err)
	}

	const callers = 20
	results := make([]domain.ReportSummary, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = svc.GetReportSummary(context.Background(), day(10, 1), day(10, 31))
		}()
	}
	wg.Wait()
	for i := range callers {
		require.NoError(t, errs[i])
		assert.Equal(t, results[0], results[i])
	}
	assert.Equal(t, 21.0, results[0].Total)
}

func TestReportOptionsIgnoreInvalidValues(t *testing.T) {
	st := memory.New()
	svc, ok := New(st.Repos(), st, WithHeartbeatInterval(0), WithMaxWorkers(0), WithLogf(nil)).(*service)
	require.True(t, ok)
	assert.Equal(t, defaultHeartbeat, svc.heartbeat)
	assert.Equal(t, defaultMaxWorkers, svc.maxWorkers)
	assert.NotNil(t, svc.logf)
}
