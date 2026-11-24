package api

import (
	"net/http"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger"
)

// Handler - HTTP-обработчики. Знает только ledger.Service: про БД и кеш ему ничего не известно.
type Handler struct {
	svc ledger.Service
}

func NewHandler(svc ledger.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) createTransaction(w http.ResponseWriter, r *http.Request) {
	var req CreateTransactionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := req.ToDomain()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := tx.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	saved, err := h.svc.AddTransaction(r.Context(), tx)
	if err != nil {
		writeServiceError(w, r, "add transaction", err)
		return
	}
	writeJSON(w, http.StatusCreated, NewTransactionResponse(saved))
}

func (h *Handler) listTransactions(w http.ResponseWriter, r *http.Request) {
	txs, err := h.svc.ListTransactions(r.Context())
	if err != nil {
		writeServiceError(w, r, "list transactions", err)
		return
	}
	writeJSON(w, http.StatusOK, NewTransactionResponses(txs))
}

func (h *Handler) createBudget(w http.ResponseWriter, r *http.Request) {
	var req CreateBudgetRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	b := req.ToDomain()
	if err := b.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.svc.SetBudget(r.Context(), b); err != nil {
		writeServiceError(w, r, "set budget", err)
		return
	}
	writeJSON(w, http.StatusCreated, NewBudgetResponse(b))
}

func (h *Handler) listBudgets(w http.ResponseWriter, r *http.Request) {
	budgets, err := h.svc.ListBudgets(r.Context())
	if err != nil {
		writeServiceError(w, r, "list budgets", err)
		return
	}
	writeJSON(w, http.StatusOK, NewBudgetResponses(budgets))
}

func parseDateParam(r *http.Request, name string) (time.Time, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return time.Time{}, &ledger.ValidationError{Field: name, Message: name + " query parameter is required"}
	}
	t, err := time.Parse(dateLayout, raw)
	if err != nil {
		return time.Time{}, &ledger.ValidationError{Field: name, Message: name + " must be in YYYY-MM-DD format"}
	}
	return t, nil
}

func (h *Handler) reportSummary(w http.ResponseWriter, r *http.Request) {
	from, err := parseDateParam(r, "from")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	to, err := parseDateParam(r, "to")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	report, err := h.svc.GetReportSummary(r.Context(), from, to)
	if err != nil {
		writeServiceError(w, r, "report summary", err)
		return
	}
	writeJSON(w, http.StatusOK, NewReportSummaryResponse(report))
}
