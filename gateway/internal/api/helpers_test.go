package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AndreyQuantum/golang-study-ms/ledger"
)

const jsonCT = "application/json; charset=utf-8"

func resetLedger(t *testing.T) {
	t.Helper()
	ledger.Reset()
	t.Cleanup(ledger.Reset)
}

func do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	NewRouter().ServeHTTP(rec, req)
	return rec
}

func requireJSON(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	require.Equal(t, status, rec.Code, rec.Body.String())
	assert.Equal(t, jsonCT, rec.Header().Get("Content-Type"))
}

func requireErrorBody(t *testing.T, rec *httptest.ResponseRecorder, status int) ErrorResponse {
	t.Helper()
	requireJSON(t, rec, status)
	var e ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &e))
	require.NotEmpty(t, e.Error)
	return e
}
