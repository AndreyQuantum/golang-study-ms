package api

import (
	"strings"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger"
)

const dateLayout = "2006-01-02"

type CreateTransactionRequest struct {
	Amount      float64 `json:"amount"`
	Category    string  `json:"category"`
	Description string  `json:"description"`
	Date        string  `json:"date"`
}

type TransactionResponse struct {
	ID          int     `json:"id"`
	Amount      float64 `json:"amount"`
	Category    string  `json:"category"`
	Description string  `json:"description"`
	Date        string  `json:"date"`
}

type CreateBudgetRequest struct {
	Category string  `json:"category"`
	Limit    float64 `json:"limit"`
}

type BudgetResponse struct {
	Category string  `json:"category"`
	Limit    float64 `json:"limit"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func (r CreateTransactionRequest) ToDomain() (ledger.Transaction, error) {
	var date time.Time
	if r.Date != "" {
		parsed, err := time.Parse(dateLayout, r.Date)
		if err != nil {
			return ledger.Transaction{}, &ledger.ValidationError{Field: "date", Message: "date must be in YYYY-MM-DD format"}
		}
		date = parsed
	}
	return ledger.Transaction{
		Amount:      r.Amount,
		Category:    strings.TrimSpace(r.Category),
		Description: r.Description,
		Date:        date,
	}, nil
}

func NewTransactionResponse(t ledger.Transaction) TransactionResponse {
	return TransactionResponse{
		ID:          t.ID,
		Amount:      t.Amount,
		Category:    t.Category,
		Description: t.Description,
		Date:        t.Date.Format(dateLayout),
	}
}

func NewTransactionResponses(txs []ledger.Transaction) []TransactionResponse {
	result := make([]TransactionResponse, 0, len(txs))
	for _, t := range txs {
		result = append(result, NewTransactionResponse(t))
	}
	return result
}

func (r CreateBudgetRequest) ToDomain() ledger.Budget {
	return ledger.Budget{Category: strings.TrimSpace(r.Category), Limit: r.Limit}
}

func NewBudgetResponse(b ledger.Budget) BudgetResponse {
	return BudgetResponse{Category: b.Category, Limit: b.Limit}
}

func NewBudgetResponses(budgets []ledger.Budget) []BudgetResponse {
	result := make([]BudgetResponse, 0, len(budgets))
	for _, b := range budgets {
		result = append(result, NewBudgetResponse(b))
	}
	return result
}

type CategoryTotalResponse struct {
	Category string  `json:"category"`
	Total    float64 `json:"total"`
}

type ReportSummaryResponse struct {
	From       string                  `json:"from"`
	To         string                  `json:"to"`
	Total      float64                 `json:"total"`
	Categories []CategoryTotalResponse `json:"categories"`
}

func NewReportSummaryResponse(r ledger.ReportSummary) ReportSummaryResponse {
	categories := make([]CategoryTotalResponse, 0, len(r.Categories))
	for _, c := range r.Categories {
		categories = append(categories, CategoryTotalResponse{Category: c.Category, Total: c.Total})
	}
	return ReportSummaryResponse{From: r.From, To: r.To, Total: r.Total, Categories: categories}
}
