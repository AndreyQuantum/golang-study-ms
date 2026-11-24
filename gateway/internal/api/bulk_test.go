package api

import (
	"encoding/json"
	"errors"
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

const bulkPath = "/api/transactions/bulk"

func bulkBody(items ...string) string { return "[" + strings.Join(items, ",") + "]" }

func item(amount, category, date string) string {
	return `{"amount":` + amount + `,"category":"` + category + `","date":"` + date + `"}`
}

func TestBulkAllAccepted(t *testing.T) {
	svc := newService(t)
	rec := do(t, svc, http.MethodPost, bulkPath, bulkBody(
		item("10", "еда", "2026-10-01"), item("20", "еда", "2026-10-02"), item("30", "такси", "2026-10-03")))
	requireJSON(t, rec, http.StatusOK)
	assert.JSONEq(t, `{"accepted":3,"rejected":0,"errors":[]}`, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "null")
	assert.Len(t, mustTransactions(t, svc), 3)
}

func TestBulkPartialWithBudget(t *testing.T) {
	svc := newService(t)
	require.NoError(t, svc.SetBudget(t.Context(), ledger.Budget{Category: "еда", Limit: 25}))
	rec := do(t, svc, http.MethodPost, bulkPath+"?workers=1", bulkBody(
		item("10", "еда", "2026-10-01"), item("10", "еда", "2026-10-01"),
		item("10", "еда", "2026-10-01"), item("10", "еда", "2026-10-01")))
	requireJSON(t, rec, http.StatusOK)
	assert.JSONEq(t, `{"accepted":2,"rejected":2,"errors":[{"index":2,"error":"budget exceeded"},{"index":3,"error":"budget exceeded"}]}`, rec.Body.String())
	assert.Len(t, mustTransactions(t, svc), 2)
}

func TestBulkConversionErrorsKeepOriginalIndex(t *testing.T) {
	svc := &fakeService{}
	svc.importSummary = ledger.ImportSummary{Accepted: 2, Rejected: 1, Errors: []ledger.ImportError{
		// индекс 2 в списке, отправленном в ledger, - это элемент 4 исходного массива
		{Index: 2, Err: &ledger.ValidationError{Field: "amount", Message: "amount must be positive"}},
	}}
	rec := do(t, svc, http.MethodPost, bulkPath, bulkBody(
		item("1", "еда", "2026-10-01"),
		item("1", "еда", "10.10.2026"),
		item("1", "еда", "2026-10-01"),
		item("1", "еда", "10.10.2026"),
		item("-5", "еда", "2026-10-01")))
	requireJSON(t, rec, http.StatusOK)
	assert.JSONEq(t, `{"accepted":2,"rejected":3,"errors":[
		{"index":1,"error":"date must be in YYYY-MM-DD format"},
		{"index":3,"error":"date must be in YYYY-MM-DD format"},
		{"index":4,"error":"amount must be positive"}]}`, rec.Body.String())
	_, input := svc.lastImport()
	assert.Len(t, input, 3, "в ledger уходят только элементы без ошибок конверсии")
}

func TestBulkRealServiceIndexMapping(t *testing.T) {
	svc := newService(t)
	rec := do(t, svc, http.MethodPost, bulkPath+"?workers=1", bulkBody(
		item("1", "еда", "2026-10-01"),
		item("1", "еда", "bad"),
		item("1", "еда", "2026-10-01"),
		item("1", "еда", "bad"),
		item("-5", "еда", "2026-10-01")))
	requireJSON(t, rec, http.StatusOK)
	assert.JSONEq(t, `{"accepted":2,"rejected":3,"errors":[
		{"index":1,"error":"date must be in YYYY-MM-DD format"},
		{"index":3,"error":"date must be in YYYY-MM-DD format"},
		{"index":4,"error":"amount must be positive"}]}`, rec.Body.String())
}

func TestBulkValidationMessagesMatchSingleEndpoint(t *testing.T) {
	bad := []string{
		`{"amount":-1,"category":"еда","date":"2026-10-01"}`,
		`{"amount":1,"category":" ","date":"2026-10-01"}`,
		`{"amount":1,"category":"еда"}`,
	}
	svc := newService(t)
	rec := do(t, svc, http.MethodPost, bulkPath, bulkBody(bad...))
	requireJSON(t, rec, http.StatusOK)
	var resp BulkImportResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Errors, 3)
	for i, body := range bad {
		single := do(t, svc, http.MethodPost, "/api/transactions", body)
		var e ErrorResponse
		require.NoError(t, json.Unmarshal(single.Body.Bytes(), &e))
		assert.Equal(t, e.Error, resp.Errors[i].Error)
		assert.Equal(t, i, resp.Errors[i].Index)
	}
}

func TestBulkEmptyArray(t *testing.T) {
	rec := do(t, newService(t), http.MethodPost, bulkPath, `[]`)
	requireJSON(t, rec, http.StatusOK)
	assert.JSONEq(t, `{"accepted":0,"rejected":0,"errors":[]}`, rec.Body.String())
}

func TestBulkBadRequests(t *testing.T) {
	valid := item("1", "еда", "2026-10-01")
	many := bulkBody(slicesRepeat(valid, ledger.MaxImportItems+1)...)
	huge := `[{"amount":1,"category":"` + strings.Repeat("x", maxBodyBytes) + `"}]`
	cases := []struct{ name, path, body string }{
		{"empty body", bulkPath, ``},
		{"not json", bulkPath, `abc`},
		{"object", bulkPath, valid},
		{"null", bulkPath, `null`},
		{"unknown field", bulkPath, `[{"amount":1,"category":"еда","bogus":1}]`},
		{"trailing data", bulkPath, `[] []`},
		{"too large", bulkPath, huge},
		{"too many items", bulkPath, many},
		{"workers zero", bulkPath + "?workers=0", `[]`},
		{"workers too big", bulkPath + "?workers=33", `[]`},
		{"workers text", bulkPath + "?workers=x", `[]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{}
			rec := do(t, svc, http.MethodPost, tc.path, tc.body)
			requireErrorBody(t, rec, http.StatusBadRequest)
			assert.Zero(t, svc.callCount())
		})
	}
	rec := do(t, &fakeService{}, http.MethodPost, bulkPath+"?workers=0", `[]`)
	assert.JSONEq(t, `{"error":"workers must be an integer between 1 and 32"}`, rec.Body.String())
}

func slicesRepeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

func TestBulkWorkersPassedToService(t *testing.T) {
	svc := &fakeService{}
	requireJSON(t, do(t, svc, http.MethodPost, bulkPath, `[]`), http.StatusOK)
	workers, _ := svc.lastImport()
	assert.Equal(t, 4, workers)
	requireJSON(t, do(t, svc, http.MethodPost, bulkPath+"?workers=7", `[]`), http.StatusOK)
	workers, _ = svc.lastImport()
	assert.Equal(t, 7, workers)
}

func TestBulkInternalItemErrorHidden(t *testing.T) {
	buf := captureLog(t)
	svc := &fakeService{importSummary: ledger.ImportSummary{Rejected: 1,
		Errors: []ledger.ImportError{{Index: 0, Err: errors.New("pq: secret")}}}}
	rec := do(t, svc, http.MethodPost, bulkPath, bulkBody(item("1", "еда", "2026-10-01")))
	requireJSON(t, rec, http.StatusOK)
	assert.JSONEq(t, `{"accepted":0,"rejected":1,"errors":[{"index":0,"error":"internal error"}]}`, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "secret")
	assert.Contains(t, buf.String(), "pq: secret")
}
