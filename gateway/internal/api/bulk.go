package api

import (
	"net/http"
	"strconv"

	"github.com/AndreyQuantum/golang-study-ms/ledger"
)

const workersMessage = "workers must be an integer between 1 and 32"

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
