package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/AndreyQuantum/golang-study-ms/ledger"
)

const maxBodyBytes = 1 << 20

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}

// writeServiceError - единое место, где ошибки сервиса превращаются в HTTP-ответ.
// Порядок важен: ошибки предметной области (409, 400) приоритетнее ошибок контекста.
// Истёкший дедлайн или отмена запроса (в том числе когда клиент отключился) дают 504 на любом endpoint.
func writeServiceError(w http.ResponseWriter, r *http.Request, op string, err error) {
	var ve *ledger.ValidationError
	switch {
	case errors.Is(err, ledger.ErrBudgetExceeded):
		writeError(w, http.StatusConflict, "budget exceeded")
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		log.Printf("%s: %v", op, err)
		writeError(w, http.StatusGatewayTimeout, "request timeout")
	case r.Context().Err() != nil:
		// драйвер или транзакция могли вернуть ошибку, не оборачивающую ctx.Err(), но запрос уже просрочен или отменён
		log.Printf("%s: %v (request context: %v)", op, err, r.Context().Err())
		writeError(w, http.StatusGatewayTimeout, "request timeout")
	default:
		log.Printf("%s: %v", op, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

// decodeJSON читает тело с единственным JSON-объектом.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	return decodeJSONValue(w, r, dst, "JSON object")
}

// decodeJSONValue читает ровно одно JSON-значение (what - его название в тексте ошибки: "JSON object", "JSON array").
func decodeJSONValue(w http.ResponseWriter, r *http.Request, dst any, what string) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("request body must not be empty")
		}
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request body must contain a single %s", what)
	}
	return nil
}
