package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func NewRouter() http.Handler {
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
		r.Post("/transactions", createTransaction)
		r.Get("/transactions", listTransactions)
		r.Post("/budgets", createBudget)
		r.Get("/budgets", listBudgets)
		r.Get("/reports/summary", reportSummary)
	})
	return r
}
