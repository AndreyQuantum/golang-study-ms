package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AndreyQuantum/golang-study-ms/ledger"
)


func TestParseWorkers(t *testing.T) {
	ok := map[string]int{"": ledger.DefaultImportWorkers, "workers=1": 1, "workers=32": 32, "workers=7": 7}
	for query, want := range ok {
		got, err := parseWorkers(httptest.NewRequest(http.MethodPost, "/api/transactions/bulk?"+query, nil))
		require.NoError(t, err, query)
		assert.Equal(t, want, got, query)
	}
	for _, query := range []string{"workers=0", "workers=33", "workers=-1", "workers=abc", "workers=2.5", "workers="} {
		_, err := parseWorkers(httptest.NewRequest(http.MethodPost, "/api/transactions/bulk?"+query, nil))
		require.Error(t, err, query)
		assert.Equal(t, workersMessage, err.Error(), query)
	}
}

func TestDecodeJSONValueMessages(t *testing.T) {
	decode := func(body string, dst any, what string) error {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
		return decodeJSONValue(httptest.NewRecorder(), req, dst, what)
	}
	var obj CreateTransactionRequest
	// decodeJSON сохраняет прежние тексты
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{} {}`))
	assert.EqualError(t, decodeJSON(httptest.NewRecorder(), req, &obj), "request body must contain a single JSON object")
	req = httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(``))
	assert.EqualError(t, decodeJSON(httptest.NewRecorder(), req, &obj), "request body must not be empty")
	req = httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"bad":1}`))
	assert.ErrorContains(t, decodeJSON(httptest.NewRecorder(), req, &obj), "invalid JSON: ")

	var arr []CreateTransactionRequest
	assert.EqualError(t, decode(`[] []`, &arr, "JSON array"), "request body must contain a single JSON array")
	require.NoError(t, decode(`[{"amount":1}]`, &arr, "JSON array"))
	assert.Len(t, arr, 1)
}
