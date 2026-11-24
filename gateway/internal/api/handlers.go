package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger"
)

// opTimeout ограничивает время одного обращения к сервису; отсчёт идёт от контекста запроса.
const opTimeout = 5 * time.Second

// Handler - HTTP-обработчики. Знает только ledger.Service: про БД и кеш ему ничего не известно.
type Handler struct {
	svc ledger.Service
}

func NewHandler(svc ledger.Service) *Handler {
	return &Handler{svc: svc}
}

func opCtx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), opTimeout)
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

	ctx, cancel := opCtx(r)
	defer cancel()
	saved, err := h.svc.AddTransaction(ctx, tx)
	var ve *ledger.ValidationError
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, NewTransactionResponse(saved))
	case errors.Is(err, ledger.ErrBudgetExceeded):
		writeError(w, http.StatusConflict, "budget exceeded")
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		log.Printf("add transaction: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func (h *Handler) listTransactions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := opCtx(r)
	defer cancel()
	txs, err := h.svc.ListTransactions(ctx)
	if err != nil {
		log.Printf("list transactions: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
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

	ctx, cancel := opCtx(r)
	defer cancel()
	err := h.svc.SetBudget(ctx, b)
	var ve *ledger.ValidationError
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, NewBudgetResponse(b))
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		log.Printf("set budget: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func (h *Handler) listBudgets(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := opCtx(r)
	defer cancel()
	budgets, err := h.svc.ListBudgets(ctx)
	if err != nil {
		log.Printf("list budgets: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
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

	ctx, cancel := opCtx(r)
	defer cancel()
	report, err := h.svc.GetReportSummary(ctx, from, to)
	var ve *ledger.ValidationError
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, NewReportSummaryResponse(report))
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		log.Printf("report summary: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
