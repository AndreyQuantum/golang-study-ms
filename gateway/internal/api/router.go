package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/AndreyQuantum/golang-study-ms/ledger"
)

// DefaultRequestTimeout - таймаут запроса, если он не задан через WithTimeout.
const DefaultRequestTimeout = 2 * time.Second

type routerConfig struct {
	timeout time.Duration
}

// RouterOption настраивает NewRouter.
type RouterOption func(*routerConfig)

// WithTimeout задаёт таймаут запроса. d <= 0 игнорируется.
func WithTimeout(d time.Duration) RouterOption {
	return func(c *routerConfig) {
		if d > 0 {
			c.timeout = d
		}
	}
}

func NewRouter(svc ledger.Service, opts ...RouterOption) http.Handler {
	cfg := routerConfig{timeout: DefaultRequestTimeout}
	for _, opt := range opts {
		opt(&cfg)
	}
	h := NewHandler(svc)
	r := chi.NewRouter()
	// Logging снаружи, чтобы в логе был итоговый статус (в том числе 504); Timeout покрывает /ping и все /api.
	r.Use(Logging, JSONContentType, Timeout(cfg.timeout))

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	})

	r.Get("/ping", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"message": "pong"})
	})

	r.Route("/api", func(r chi.Router) {
		r.Post("/transactions", h.createTransaction)
		r.Get("/transactions", h.listTransactions)
		r.Post("/budgets", h.createBudget)
		r.Get("/budgets", h.listBudgets)
		r.Get("/reports/summary", h.reportSummary)
	})
	return r
}
