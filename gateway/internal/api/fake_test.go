package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AndreyQuantum/golang-study-ms/ledger"
)

// fakeService - ledger.Service с настраиваемой ошибкой; запоминает ctx последнего вызова.
// Поля защищены mutex: запросы могут обслуживаться параллельно, а тесты идут под -race.
type fakeService struct {
	err  error
	wait bool // блокироваться до отмены ctx и вернуть ctx.Err()

	mu     sync.Mutex
	calls  int
	gotCtx context.Context
}

var _ ledger.Service = (*fakeService)(nil)

func (f *fakeService) record(ctx context.Context) error {
	f.mu.Lock()
	f.calls++
	f.gotCtx = ctx
	err, wait := f.err, f.wait
	f.mu.Unlock()
	if wait {
		<-ctx.Done()
		return ctx.Err()
	}
	return err
}

func (f *fakeService) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeService) lastCtx() context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gotCtx
}

func (f *fakeService) SetBudget(ctx context.Context, _ ledger.Budget) error {
	return f.record(ctx)
}

func (f *fakeService) ListBudgets(ctx context.Context) ([]ledger.Budget, error) {
	return []ledger.Budget{}, f.record(ctx)
}

func (f *fakeService) AddTransaction(ctx context.Context, tx ledger.Transaction) (ledger.Transaction, error) {
	return tx, f.record(ctx)
}

func (f *fakeService) ListTransactions(ctx context.Context) ([]ledger.Transaction, error) {
	return []ledger.Transaction{}, f.record(ctx)
}

func (f *fakeService) GetReportSummary(ctx context.Context, _, _ time.Time) (ledger.ReportSummary, error) {
	return ledger.ReportSummary{}, f.record(ctx)
}

type endpoint struct {
	name   string
	method string
	path   string
	body   string
}

var allEndpoints = []endpoint{
	{"create transaction", http.MethodPost, "/api/transactions", `{"amount":10,"category":"еда","date":"2025-09-10"}`},
	{"list transactions", http.MethodGet, "/api/transactions", ""},
	{"create budget", http.MethodPost, "/api/budgets", `{"category":"еда","limit":100}`},
	{"list budgets", http.MethodGet, "/api/budgets", ""},
	{"report summary", http.MethodGet, "/api/reports/summary?from=2025-09-01&to=2025-09-30", ""},
}

func TestInternalErrorFromService(t *testing.T) {
	for _, ep := range allEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			svc := &fakeService{err: errors.New("db down")}
			rec := do(t, svc, ep.method, ep.path, ep.body)
			requireJSON(t, rec, http.StatusInternalServerError)
			assert.JSONEq(t, `{"error":"internal server error"}`, rec.Body.String())
			assert.Equal(t, 1, svc.callCount())
		})
	}
}

func TestBudgetExceededMapsToConflict(t *testing.T) {
	svc := &fakeService{err: fmt.Errorf("category %q: %w", "еда", ledger.ErrBudgetExceeded)}
	rec := do(t, svc, http.MethodPost, "/api/transactions", allEndpoints[0].body)
	requireJSON(t, rec, http.StatusConflict)
	assert.JSONEq(t, `{"error":"budget exceeded"}`, rec.Body.String())
}

func TestValidationErrorFromServiceMapsToBadRequest(t *testing.T) {
	ve := &ledger.ValidationError{Field: "x", Message: "rejected by service"}
	for _, ep := range []endpoint{allEndpoints[0], allEndpoints[2], allEndpoints[4]} {
		t.Run(ep.name, func(t *testing.T) {
			svc := &fakeService{err: ve}
			rec := do(t, svc, ep.method, ep.path, ep.body)
			requireJSON(t, rec, http.StatusBadRequest)
			assert.JSONEq(t, `{"error":"rejected by service"}`, rec.Body.String())
		})
	}
}

type ctxKey struct{}

func TestRequestContextReachesService(t *testing.T) {
	for _, ep := range allEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			svc := &fakeService{}
			var body *strings.Reader
			if ep.body != "" {
				body = strings.NewReader(ep.body)
			} else {
				body = strings.NewReader("")
			}
			req := httptest.NewRequest(ep.method, ep.path, body)
			req = req.WithContext(context.WithValue(req.Context(), ctxKey{}, "marker"))

			rec := httptest.NewRecorder()
			NewRouter(svc).ServeHTTP(rec, req)
			require.Less(t, rec.Code, 500, rec.Body.String())

			require.Equal(t, 1, svc.callCount())
			assert.Equal(t, "marker", svc.lastCtx().Value(ctxKey{}), "в сервис должен уходить контекст запроса")
		})
	}
}

func TestReportWithBadDateDoesNotCallService(t *testing.T) {
	svc := &fakeService{}
	rec := do(t, svc, http.MethodGet, "/api/reports/summary?from=bad&to=2025-09-30", "")
	requireErrorBody(t, rec, http.StatusBadRequest)
	assert.Zero(t, svc.callCount())
}

func TestContextErrorsMapToGatewayTimeout(t *testing.T) {
	errs := map[string]error{
		"deadline": context.DeadlineExceeded,
		"canceled": context.Canceled,
		"wrapped":  fmt.Errorf("wrapped: %w", context.DeadlineExceeded),
	}
	for _, ep := range allEndpoints {
		for name, cause := range errs {
			t.Run(ep.name+"/"+name, func(t *testing.T) {
				captureLog(t)
				svc := &fakeService{err: cause}
				rec := do(t, svc, ep.method, ep.path, ep.body)
				requireJSON(t, rec, http.StatusGatewayTimeout)
				assert.JSONEq(t, `{"error":"request timeout"}`, rec.Body.String())
				assert.Equal(t, 1, svc.callCount())
			})
		}
	}
}

// Драйвер или транзакция могут вернуть ошибку, не оборачивающую ctx.Err(): тогда решает состояние ctx запроса.
func TestExpiredContextMapsToGatewayTimeout(t *testing.T) {
	for _, ep := range allEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			captureLog(t)
			svc := &fakeService{err: errors.New("conn closed")}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			req := httptest.NewRequest(ep.method, ep.path, strings.NewReader(ep.body)).WithContext(ctx)
			rec := httptest.NewRecorder()
			NewRouter(svc).ServeHTTP(rec, req)
			requireJSON(t, rec, http.StatusGatewayTimeout)
			assert.JSONEq(t, `{"error":"request timeout"}`, rec.Body.String())

			// та же ошибка при живом ctx - обычная внутренняя ошибка
			rec = do(t, &fakeService{err: errors.New("conn closed")}, ep.method, ep.path, ep.body)
			requireJSON(t, rec, http.StatusInternalServerError)
		})
	}
}

func TestValidationErrorWinsOverContext(t *testing.T) {
	ve := &ledger.ValidationError{Field: "x", Message: "rejected by service"}
	svc := &fakeService{err: errors.Join(ve, context.DeadlineExceeded)}
	rec := do(t, svc, http.MethodPost, "/api/transactions", allEndpoints[0].body)
	requireJSON(t, rec, http.StatusBadRequest)
}
