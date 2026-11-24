package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/AndreyQuantum/golang-study-ms/ledger"
)

func NewRouter(svc ledger.Service) http.Handler {
	h := NewHandler(svc)
	r := chi.NewRouter()
	r.Use(Logging, JSONContentType)

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
