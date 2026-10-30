package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBudgetsAPI(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		resetLedger(t)
		rec := do(t, http.MethodPost, "/api/budgets", `{"category":"еда","limit":5000}`)
		requireJSON(t, rec, http.StatusCreated)
		var got BudgetResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, BudgetResponse{Category: "еда", Limit: 5000}, got)
	})

	t.Run("list", func(t *testing.T) {
		resetLedger(t)
		requireJSON(t, do(t, http.MethodPost, "/api/budgets", `{"category":"еда","limit":5000}`), http.StatusCreated)
		rec := do(t, http.MethodGet, "/api/budgets", "")
		requireJSON(t, rec, http.StatusOK)
		var got []BudgetResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, []BudgetResponse{{Category: "еда", Limit: 5000}}, got)
	})

	t.Run("empty_list", func(t *testing.T) {
		resetLedger(t)
		rec := do(t, http.MethodGet, "/api/budgets", "")
		requireJSON(t, rec, http.StatusOK)
		assert.JSONEq(t, `[]`, rec.Body.String())
	})

	t.Run("bad_json", func(t *testing.T) {
		resetLedger(t)
		requireErrorBody(t, do(t, http.MethodPost, "/api/budgets", `{"category":`), http.StatusBadRequest)
	})

	t.Run("empty_body", func(t *testing.T) {
		resetLedger(t)
		e := requireErrorBody(t, do(t, http.MethodPost, "/api/budgets", ""), http.StatusBadRequest)
		assert.Equal(t, "request body must not be empty", e.Error)
	})

	t.Run("zero_limit", func(t *testing.T) {
		resetLedger(t)
		e := requireErrorBody(t, do(t, http.MethodPost, "/api/budgets", `{"category":"еда","limit":0}`), http.StatusBadRequest)
		assert.Equal(t, "limit must be positive", e.Error)
	})

	t.Run("negative_limit", func(t *testing.T) {
		resetLedger(t)
		requireErrorBody(t, do(t, http.MethodPost, "/api/budgets", `{"category":"еда","limit":-5}`), http.StatusBadRequest)
	})

	t.Run("empty_category", func(t *testing.T) {
		resetLedger(t)
		e := requireErrorBody(t, do(t, http.MethodPost, "/api/budgets", `{"category":"  ","limit":5}`), http.StatusBadRequest)
		assert.Equal(t, "category must not be empty", e.Error)
	})

	t.Run("unknown_field", func(t *testing.T) {
		resetLedger(t)
		requireErrorBody(t, do(t, http.MethodPost, "/api/budgets", `{"category":"еда","limit":5,"extra":1}`), http.StatusBadRequest)
	})

	t.Run("upsert", func(t *testing.T) {
		resetLedger(t)
		requireJSON(t, do(t, http.MethodPost, "/api/budgets", `{"category":"еда","limit":100}`), http.StatusCreated)
		requireJSON(t, do(t, http.MethodPost, "/api/budgets", `{"category":"еда","limit":300}`), http.StatusCreated)
		rec := do(t, http.MethodGet, "/api/budgets", "")
		requireJSON(t, rec, http.StatusOK)
		var got []BudgetResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, []BudgetResponse{{Category: "еда", Limit: 300}}, got)
	})
}

func TestTransactionsAPI(t *testing.T) {
	const okBody = `{"amount":450,"category":"еда","description":"ланч","date":"2025-09-10"}`

	t.Run("ok", func(t *testing.T) {
		resetLedger(t)
		requireJSON(t, do(t, http.MethodPost, "/api/budgets", `{"category":"еда","limit":500}`), http.StatusCreated)
		rec := do(t, http.MethodPost, "/api/transactions", okBody)
		requireJSON(t, rec, http.StatusCreated)
		var got TransactionResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, TransactionResponse{ID: 1, Amount: 450, Category: "еда", Description: "ланч", Date: "2025-09-10"}, got)
	})

	t.Run("list", func(t *testing.T) {
		resetLedger(t)
		requireJSON(t, do(t, http.MethodPost, "/api/transactions", okBody), http.StatusCreated)
		rec := do(t, http.MethodGet, "/api/transactions", "")
		requireJSON(t, rec, http.StatusOK)
		var got []TransactionResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Len(t, got, 1)
		assert.Equal(t, "2025-09-10", got[0].Date)
		assert.Equal(t, 1, got[0].ID)
	})

	t.Run("empty_list", func(t *testing.T) {
		resetLedger(t)
		rec := do(t, http.MethodGet, "/api/transactions", "")
		requireJSON(t, rec, http.StatusOK)
		assert.Equal(t, "[]\n", rec.Body.String())
	})

	t.Run("exceeded", func(t *testing.T) {
		resetLedger(t)
		requireJSON(t, do(t, http.MethodPost, "/api/budgets", `{"category":"еда","limit":500}`), http.StatusCreated)
		requireJSON(t, do(t, http.MethodPost, "/api/transactions", okBody), http.StatusCreated)
		before := len(mustTransactions(t))
		rec := do(t, http.MethodPost, "/api/transactions", okBody)
		requireJSON(t, rec, http.StatusConflict)
		assert.Equal(t, `{"error":"budget exceeded"}`+"\n", rec.Body.String())
		assert.Len(t, mustTransactions(t), before)
	})

	t.Run("bad_json", func(t *testing.T) {
		resetLedger(t)
		requireErrorBody(t, do(t, http.MethodPost, "/api/transactions", `not json`), http.StatusBadRequest)
	})

	t.Run("invalid_date", func(t *testing.T) {
		resetLedger(t)
		e := requireErrorBody(t, do(t, http.MethodPost, "/api/transactions", `{"amount":1,"category":"а","date":"10.09.2025"}`), http.StatusBadRequest)
		assert.Equal(t, "date must be in YYYY-MM-DD format", e.Error)
	})

	t.Run("missing_date", func(t *testing.T) {
		resetLedger(t)
		e := requireErrorBody(t, do(t, http.MethodPost, "/api/transactions", `{"amount":1,"category":"а"}`), http.StatusBadRequest)
		assert.Equal(t, "date must be set", e.Error)
	})

	t.Run("zero_amount", func(t *testing.T) {
		resetLedger(t)
		e := requireErrorBody(t, do(t, http.MethodPost, "/api/transactions", `{"amount":0,"category":"а","date":"2025-09-10"}`), http.StatusBadRequest)
		assert.Equal(t, "amount must be positive", e.Error)
	})

	t.Run("empty_category", func(t *testing.T) {
		resetLedger(t)
		e := requireErrorBody(t, do(t, http.MethodPost, "/api/transactions", `{"amount":1,"category":" ","date":"2025-09-10"}`), http.StatusBadRequest)
		assert.Equal(t, "category must not be empty", e.Error)
	})

	t.Run("trailing_data", func(t *testing.T) {
		resetLedger(t)
		e := requireErrorBody(t, do(t, http.MethodPost, "/api/transactions", okBody+`{}`), http.StatusBadRequest)
		assert.Equal(t, "request body must contain a single JSON object", e.Error)
	})
}

func TestRouter(t *testing.T) {
	t.Run("ping", func(t *testing.T) {
		rec := do(t, http.MethodGet, "/ping", "")
		requireJSON(t, rec, http.StatusOK)
		assert.JSONEq(t, `{"message":"pong"}`, rec.Body.String())
	})

	t.Run("not_found", func(t *testing.T) {
		rec := do(t, http.MethodGet, "/nope", "")
		requireJSON(t, rec, http.StatusNotFound)
		assert.JSONEq(t, `{"error":"not found"}`, rec.Body.String())
	})

	t.Run("method_not_allowed", func(t *testing.T) {
		rec := do(t, http.MethodDelete, "/api/budgets", "")
		requireJSON(t, rec, http.StatusMethodNotAllowed)
		assert.JSONEq(t, `{"error":"method not allowed"}`, rec.Body.String())
	})
}

func TestReportSummaryAPI(t *testing.T) {
	seed := func(t *testing.T) {
		t.Helper()
		resetLedger(t)
		for _, body := range []string{
			`{"amount":100.10,"category":"еда","date":"2026-10-01"}`,
			`{"amount":50.20,"category":"еда","date":"2026-10-10"}`,
			`{"amount":30,"category":"транспорт","date":"2026-10-05"}`,
			`{"amount":999,"category":"еда","date":"2026-09-30"}`,
		} {
			requireJSON(t, do(t, http.MethodPost, "/api/transactions", body), http.StatusCreated)
		}
	}

	t.Run("ok", func(t *testing.T) {
		seed(t)
		rec := do(t, http.MethodGet, "/api/reports/summary?from=2026-10-01&to=2026-10-10", "")
		requireJSON(t, rec, http.StatusOK)
		var got ReportSummaryResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, ReportSummaryResponse{
			From:  "2026-10-01",
			To:    "2026-10-10",
			Total: 180.30,
			Categories: []CategoryTotalResponse{
				{Category: "еда", Total: 150.30},
				{Category: "транспорт", Total: 30},
			},
		}, got)
	})

	t.Run("empty", func(t *testing.T) {
		resetLedger(t)
		rec := do(t, http.MethodGet, "/api/reports/summary?from=2026-10-01&to=2026-10-31", "")
		requireJSON(t, rec, http.StatusOK)
		assert.JSONEq(t, `{"from":"2026-10-01","to":"2026-10-31","total":0,"categories":[]}`, rec.Body.String())
	})

	t.Run("same_day", func(t *testing.T) {
		seed(t)
		rec := do(t, http.MethodGet, "/api/reports/summary?from=2026-10-05&to=2026-10-05", "")
		requireJSON(t, rec, http.StatusOK)
		var got ReportSummaryResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, 30.0, got.Total)
	})

	bad := map[string]string{
		"missing_from":   "/api/reports/summary?to=2026-10-10",
		"missing_to":     "/api/reports/summary?from=2026-10-01",
		"missing_both":   "/api/reports/summary",
		"bad_from":       "/api/reports/summary?from=01.10.2026&to=2026-10-10",
		"bad_to":         "/api/reports/summary?from=2026-10-01&to=tomorrow",
		"from_after_to":  "/api/reports/summary?from=2026-10-11&to=2026-10-10",
		"impossible_day": "/api/reports/summary?from=2026-02-31&to=2026-03-01",
	}
	for name, path := range bad {
		t.Run(name, func(t *testing.T) {
			resetLedger(t)
			requireErrorBody(t, do(t, http.MethodGet, path, ""), http.StatusBadRequest)
		})
	}

	t.Run("method_not_allowed", func(t *testing.T) {
		resetLedger(t)
		requireErrorBody(t, do(t, http.MethodPost, "/api/reports/summary?from=2026-10-01&to=2026-10-10", ""), http.StatusMethodNotAllowed)
	})
}
