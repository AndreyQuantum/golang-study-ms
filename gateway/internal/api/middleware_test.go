package api

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

func TestLoggingMiddleware(t *testing.T) {
	buf := captureLog(t)

	h := Logging(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/brew", nil))

	out := buf.String()
	assert.Contains(t, out, "POST")
	assert.Contains(t, out, "/brew")
	assert.Contains(t, out, "418")
}

func TestLoggingDefaultStatus(t *testing.T) {
	buf := captureLog(t)

	h := Logging(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("x"))
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	assert.Contains(t, buf.String(), "200")
}

func TestJSONContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	JSONContentType(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, jsonCT, rec.Header().Get("Content-Type"))
}

func TestTimeoutSetsDeadline(t *testing.T) {
	var (
		deadline  time.Time
		ok        bool
		marker    any
		remaining time.Duration
	)
	h := Timeout(150 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok = r.Context().Deadline()
		remaining = time.Until(deadline)
		marker = r.Context().Value(ctxKey{})
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKey{}, "marker"))
	h.ServeHTTP(httptest.NewRecorder(), req)

	require.True(t, ok)
	assert.Greater(t, remaining, time.Duration(0))
	assert.LessOrEqual(t, remaining, 150*time.Millisecond)
	assert.Equal(t, "marker", marker, "значения исходного ctx сохраняются")
}

func TestTimeoutCancelsContextAfterHandler(t *testing.T) {
	var got context.Context
	h := Timeout(time.Hour)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Context()
		assert.NoError(t, got.Err())
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.ErrorIs(t, got.Err(), context.Canceled, "таймер не должен течь после ответа")
}

func TestTimeoutKeepsFastResponse(t *testing.T) {
	h := Timeout(time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("body"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "body", rec.Body.String())
}

func TestRouterTimesOutSlowService(t *testing.T) {
	for _, ep := range allEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			captureLog(t)
			svc := &fakeService{wait: true}
			req := httptest.NewRequest(ep.method, ep.path, strings.NewReader(ep.body))
			rec := httptest.NewRecorder()

			start := time.Now()
			NewRouter(svc, WithTimeout(30*time.Millisecond)).ServeHTTP(rec, req)

			requireJSON(t, rec, http.StatusGatewayTimeout)
			assert.JSONEq(t, `{"error":"request timeout"}`, rec.Body.String())
			assert.ErrorIs(t, svc.lastCtx().Err(), context.DeadlineExceeded)
			assert.Less(t, time.Since(start), 2*time.Second)
		})
	}
}

func TestRouterClientCancelMapsTo504(t *testing.T) {
	captureLog(t)
	svc := &fakeService{wait: true}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/transactions", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	NewRouter(svc).ServeHTTP(rec, req)
	requireJSON(t, rec, http.StatusGatewayTimeout)
	assert.JSONEq(t, `{"error":"request timeout"}`, rec.Body.String())
}

func TestRouterDefaultTimeout(t *testing.T) {
	tests := []struct {
		name string
		opts []RouterOption
		want time.Duration
	}{
		{"default", nil, DefaultRequestTimeout},
		{"custom", []RouterOption{WithTimeout(750 * time.Millisecond)}, 750 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{}
			req := httptest.NewRequest(http.MethodGet, "/api/transactions", nil)
			NewRouter(svc, tt.opts...).ServeHTTP(httptest.NewRecorder(), req)

			deadline, ok := svc.lastCtx().Deadline()
			require.True(t, ok)
			// дедлайн задан в момент запроса: от него осталось не больше таймаута и не сильно меньше
			assert.WithinDuration(t, time.Now().Add(tt.want), deadline, 100*time.Millisecond+tt.want/1000)
		})
	}
}

func TestPingStillWorksWithTimeout(t *testing.T) {
	rec := do(t, &fakeService{}, http.MethodGet, "/ping", "")
	requireJSON(t, rec, http.StatusOK)
	assert.JSONEq(t, `{"message":"pong"}`, rec.Body.String())
}

func TestSlowRequestsDoNotLeakGoroutines(t *testing.T) {
	captureLog(t)
	// без t.Parallel() и параллельных клиентов: считаем горутины процесса
	before := runtime.NumGoroutine()
	svc := &fakeService{wait: true}
	router := NewRouter(svc, WithTimeout(time.Millisecond))
	for range 50 {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/reports/summary?from=2025-09-01&to=2025-09-30", nil))
		require.Equal(t, http.StatusGatewayTimeout, rec.Code)
	}
	for range 100 {
		if runtime.NumGoroutine() <= before {
			return
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("горутины не вернулись к исходному числу %d (сейчас %d)", before, runtime.NumGoroutine())
}
