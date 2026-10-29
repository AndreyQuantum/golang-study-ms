package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	NewRouter().ServeHTTP(rec, req)
	return rec
}

func TestRouterBasics(t *testing.T) {
	tests := []struct {
		name, method, path, body string
		status                   int
		wantBody                 string
	}{
		{"ping", http.MethodGet, "/ping", "", 200, `{"message":"pong"}`},
		{"not found", http.MethodGet, "/nope", "", 404, `{"error":"not found"}`},
		{"method not allowed", http.MethodDelete, "/api/budgets", "", 405, `{"error":"method not allowed"}`},
		{"empty body", http.MethodPost, "/api/budgets", "", 400, `{"error":"request body must not be empty"}`},
		{"trailing data", http.MethodPost, "/api/budgets", `{"category":"a","limit":1}{}`, 400, `{"error":"request body must contain a single JSON object"}`},
		{"bad date", http.MethodPost, "/api/transactions", `{"amount":1,"category":"a","date":"10.09.2025"}`, 400, `{"error":"date must be in YYYY-MM-DD format"}`},
		{"missing date", http.MethodPost, "/api/transactions", `{"amount":1,"category":"a"}`, 400, `{"error":"date must be set"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, tt.method, tt.path, tt.body)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if got := strings.TrimSpace(rec.Body.String()); got != tt.wantBody {
				t.Fatalf("body = %s, want %s", got, tt.wantBody)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
				t.Fatalf("content-type = %q", ct)
			}
		})
	}
}

func TestBudgetAndTransactionFlow(t *testing.T) {
	if rec := do(t, http.MethodPost, "/api/budgets", `{"category":"flow","limit":500}`); rec.Code != 201 {
		t.Fatalf("budget status = %d", rec.Code)
	}
	ok := `{"amount":450,"category":"flow","description":"lunch","date":"2025-09-10"}`
	rec := do(t, http.MethodPost, "/api/transactions", ok)
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), `"date":"2025-09-10"`) {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = do(t, http.MethodPost, "/api/transactions", ok)
	if rec.Code != 409 || strings.TrimSpace(rec.Body.String()) != `{"error":"budget exceeded"}` {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if rec = do(t, http.MethodGet, "/api/transactions", ""); rec.Code != 200 {
		t.Fatalf("list status = %d", rec.Code)
	}
}
