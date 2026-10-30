package api

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger"
)

func createTransaction(w http.ResponseWriter, r *http.Request) {
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

	saved, err := ledger.AddTransaction(tx)
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

func listTransactions(w http.ResponseWriter, _ *http.Request) {
	txs, err := ledger.ListTransactions()
	if err != nil {
		log.Printf("list transactions: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, NewTransactionResponses(txs))
}

func createBudget(w http.ResponseWriter, r *http.Request) {
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

	err := ledger.SetBudget(b)
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

func listBudgets(w http.ResponseWriter, _ *http.Request) {
	budgets, err := ledger.ListBudgets()
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

func reportSummary(w http.ResponseWriter, r *http.Request) {
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

	report, err := ledger.GetReportSummary(r.Context(), from, to)
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
