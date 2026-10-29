package api

import (
	"errors"
	"log"
	"net/http"

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
	writeJSON(w, http.StatusOK, NewTransactionResponses(ledger.ListTransactions()))
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
	writeJSON(w, http.StatusOK, NewBudgetResponses(ledger.ListBudgets()))
}
