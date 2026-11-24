package api

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"slices"
	"strconv"

	"github.com/AndreyQuantum/golang-study-ms/ledger"
)

// Тексты отказов собираются из лимитов ledger, чтобы не расходиться с сервисом.
var (
	workersMessage = fmt.Sprintf("workers must be an integer between 1 and %d", ledger.MaxImportWorkers)
	tooManyMessage = fmt.Sprintf("too many transactions: at most %d per request", ledger.MaxImportItems)
)

var errBulkNotArray = errors.New("request body must be a JSON array of transactions")

// parseWorkers читает ?workers=: нет параметра - значение по умолчанию, иначе целое от 1 до ledger.MaxImportWorkers.
func parseWorkers(r *http.Request) (int, error) {
	q := r.URL.Query()
	if !q.Has("workers") {
		return ledger.DefaultImportWorkers, nil
	}
	n, err := strconv.Atoi(q.Get("workers"))
	if err != nil || n < 1 || n > ledger.MaxImportWorkers {
		return 0, &ledger.ValidationError{Field: "workers", Message: workersMessage}
	}
	return n, nil
}

// itemErrorMessage - текст отказа по элементу. Тексты те же, что у POST /api/transactions;
// неожиданные ошибки наружу не отдаются, их подробности попадают только в лог.
func itemErrorMessage(index int, err error) string {
	var ve *ledger.ValidationError
	switch {
	case errors.Is(err, ledger.ErrBudgetExceeded):
		return "budget exceeded"
	case errors.As(err, &ve):
		return ve.Message
	default:
		log.Printf("bulk import: item %d: %v", index, err)
		return "internal error"
	}
}

// buildBulkResponse объединяет отказы конверсии (индексы уже исходные) и отказы ledger (индексы в списке valid,
// origIdx переводит их в исходные). Итог отсортирован по индексу, Errors не nil.
func buildBulkResponse(convErrs []ImportItemError, s ledger.ImportSummary, origIdx []int) BulkImportResponse {
	errs := make([]ImportItemError, 0, len(convErrs)+len(s.Errors))
	errs = append(errs, convErrs...)
	for _, e := range s.Errors {
		idx := e.Index
		if idx >= 0 && idx < len(origIdx) {
			idx = origIdx[idx]
		}
		errs = append(errs, ImportItemError{Index: idx, Error: itemErrorMessage(idx, e.Err)})
	}
	slices.SortFunc(errs, func(a, b ImportItemError) int { return cmp.Compare(a.Index, b.Index) })
	return BulkImportResponse{Accepted: s.Accepted, Rejected: len(convErrs) + s.Rejected, Errors: errs}
}

// bulkImport - POST /api/transactions/bulk: массив транзакций, ?workers= (по умолчанию 4, 1..32).
func (h *Handler) bulkImport(w http.ResponseWriter, r *http.Request) {
	workers, err := parseWorkers(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var reqs []CreateTransactionRequest
	if err := decodeJSONValue(w, r, &reqs, "JSON array"); err != nil {
		var ute *json.UnmarshalTypeError
		if errors.As(err, &ute) && ute.Type.Kind() == reflect.Slice { // верхний уровень не массив
			err = errBulkNotArray
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if reqs == nil {
		writeError(w, http.StatusBadRequest, errBulkNotArray.Error())
		return
	}
	if len(reqs) > ledger.MaxImportItems {
		writeError(w, http.StatusBadRequest, tooManyMessage)
		return
	}

	var (
		convErrs []ImportItemError
		valid    []ledger.Transaction
		origIdx  []int
	)
	for i, req := range reqs {
		tx, err := req.ToDomain()
		if err != nil {
			convErrs = append(convErrs, ImportItemError{Index: i, Error: err.Error()})
			continue
		}
		valid = append(valid, tx)
		origIdx = append(origIdx, i)
	}

	summary, err := h.svc.ImportTransactions(r.Context(), valid, workers)
	if err != nil {
		if isInterrupted(r, err) {
			// таймаут или отмена: код тот же, что у остальных endpoint, но тело несёт частичную сводку
			log.Printf("bulk import: %v", err)
			writeJSON(w, http.StatusGatewayTimeout, BulkImportTimeoutResponse{
				Error:              "request timeout",
				BulkImportResponse: buildBulkResponse(convErrs, summary, origIdx),
			})
			return
		}
		writeServiceError(w, r, "bulk import", err)
		return
	}
	writeJSON(w, http.StatusOK, buildBulkResponse(convErrs, summary, origIdx))
}

// isInterrupted: импорт прерван дедлайном или отменой (в том числе когда ошибка драйвера не оборачивает ctx.Err(),
// но контекст запроса уже закончился). Ошибки предметной области приоритетнее, как в writeServiceError.
func isInterrupted(r *http.Request, err error) bool {
	var ve *ledger.ValidationError
	if errors.Is(err, ledger.ErrBudgetExceeded) || errors.As(err, &ve) {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || r.Context().Err() != nil
}
