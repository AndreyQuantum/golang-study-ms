package api

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AndreyQuantum/golang-study-ms/ledger"
)

func TestCreateTransactionRequestToDomain(t *testing.T) {
	t.Parallel()

	t.Run("ok", func(t *testing.T) {
		t.Parallel()
		got, err := CreateTransactionRequest{Amount: 10, Category: "  еда ", Description: "d", Date: "2025-09-10"}.ToDomain()
		require.NoError(t, err)
		assert.Equal(t, "еда", got.Category)
		assert.Equal(t, 10.0, got.Amount)
		assert.Equal(t, "d", got.Description)
		assert.True(t, got.Date.Equal(time.Date(2025, 9, 10, 0, 0, 0, 0, time.UTC)))
	})

	t.Run("empty date", func(t *testing.T) {
		t.Parallel()
		got, err := CreateTransactionRequest{Amount: 1, Category: "a"}.ToDomain()
		require.NoError(t, err)
		assert.True(t, got.Date.IsZero())
	})

	t.Run("bad date", func(t *testing.T) {
		t.Parallel()
		_, err := CreateTransactionRequest{Amount: 1, Category: "a", Date: "2025/09/10"}.ToDomain()
		var ve *ledger.ValidationError
		require.True(t, errors.As(err, &ve))
		assert.Equal(t, "date", ve.Field)
		assert.Equal(t, "date must be in YYYY-MM-DD format", ve.Message)
	})
}

func TestTransactionResponses(t *testing.T) {
	t.Parallel()
	tx := ledger.Transaction{ID: 3, Amount: 12.5, Category: "a", Description: "d", Date: time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)}
	assert.Equal(t, TransactionResponse{ID: 3, Amount: 12.5, Category: "a", Description: "d", Date: "2026-01-02"}, NewTransactionResponse(tx))

	empty := NewTransactionResponses(nil)
	assert.NotNil(t, empty)
	assert.Empty(t, empty)
	assert.Len(t, NewTransactionResponses([]ledger.Transaction{tx, tx}), 2)
}

func TestBudgetDTO(t *testing.T) {
	t.Parallel()
	b := CreateBudgetRequest{Category: " еда ", Limit: 100}.ToDomain()
	assert.Equal(t, ledger.Budget{Category: "еда", Limit: 100}, b)
	assert.Equal(t, BudgetResponse{Category: "еда", Limit: 100}, NewBudgetResponse(b))

	empty := NewBudgetResponses(nil)
	assert.NotNil(t, empty)
	assert.Empty(t, empty)
	assert.Len(t, NewBudgetResponses([]ledger.Budget{b}), 1)
}

func TestNewReportSummaryResponse(t *testing.T) {
	t.Parallel()
	got := NewReportSummaryResponse(ledger.ReportSummary{
		From:       "2026-10-01",
		To:         "2026-10-31",
		Total:      12.5,
		Categories: []ledger.CategoryTotal{{Category: "a", Total: 12.5}},
	})
	assert.Equal(t, ReportSummaryResponse{
		From:       "2026-10-01",
		To:         "2026-10-31",
		Total:      12.5,
		Categories: []CategoryTotalResponse{{Category: "a", Total: 12.5}},
	}, got)

	empty := NewReportSummaryResponse(ledger.ReportSummary{})
	assert.NotNil(t, empty.Categories)
	assert.Empty(t, empty.Categories)
}
