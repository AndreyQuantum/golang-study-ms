package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/repository/memory"
)

func day(m time.Month, d int) time.Time {
	return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC)
}

func newService() Service {
	st := memory.New()
	return New(st.Repos(), st)
}

func TestValidationDoesNotTouchRepositories(t *testing.T) {
	sp := newSpy()
	svc := sp.service()
	ctx := context.Background()

	_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: -1, Category: "food", Date: day(10, 1)})
	var ve *domain.ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, "amount must be positive", ve.Message)

	err = svc.SetBudget(ctx, domain.Budget{Category: "food", Limit: 0})
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, "limit must be positive", ve.Message)

	assert.Empty(t, sp.names())
}

func TestAddTransactionLimit(t *testing.T) {
	ctx := context.Background()

	t.Run("within limit", func(t *testing.T) {
		svc := newService()
		require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "food", Limit: 5000}))
		_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: 3000, Category: "food", Date: day(10, 1)})
		require.NoError(t, err)
	})

	t.Run("exactly at limit with float noise", func(t *testing.T) {
		svc := newService()
		require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "food", Limit: 0.3}))
		_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: 0.1, Category: "food", Date: day(10, 1)})
		require.NoError(t, err)
		_, err = svc.AddTransaction(ctx, domain.Transaction{Amount: 0.2, Category: "food", Date: day(10, 1)})
		require.NoError(t, err)
		_, err = svc.AddTransaction(ctx, domain.Transaction{Amount: 0.01, Category: "food", Date: day(10, 1)})
		assert.ErrorIs(t, err, domain.ErrBudgetExceeded)
	})

	t.Run("exceeded is not saved", func(t *testing.T) {
		svc := newService()
		require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "food", Limit: 5000}))
		_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: 3000, Category: "food", Date: day(10, 1)})
		require.NoError(t, err)
		_, err = svc.AddTransaction(ctx, domain.Transaction{Amount: 2500, Category: "food", Date: day(10, 1)})
		require.ErrorIs(t, err, domain.ErrBudgetExceeded)
		for _, part := range []string{`category "food"`, "limit 5000.00", "spent 3000.00", "new 2500.00"} {
			assert.Contains(t, err.Error(), part)
		}
		list, err := svc.ListTransactions(ctx)
		require.NoError(t, err)
		assert.Len(t, list, 1)
	})

	t.Run("category without budget", func(t *testing.T) {
		svc := newService()
		_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: 1e6, Category: "other", Date: day(10, 1)})
		assert.NoError(t, err)
	})

	t.Run("raised limit lets it pass", func(t *testing.T) {
		svc := newService()
		require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "food", Limit: 100}))
		_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: 100, Category: "food", Date: day(10, 1)})
		require.NoError(t, err)
		_, err = svc.AddTransaction(ctx, domain.Transaction{Amount: 50, Category: "food", Date: day(10, 1)})
		require.ErrorIs(t, err, domain.ErrBudgetExceeded)
		require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "food", Limit: 200}))
		_, err = svc.AddTransaction(ctx, domain.Transaction{Amount: 50, Category: "food", Date: day(10, 1)})
		assert.NoError(t, err)
	})
}

func TestAddTransactionNormalizes(t *testing.T) {
	svc := newService()
	ctx := context.Background()

	saved, err := svc.AddTransaction(ctx, domain.Transaction{
		Amount: 10.005, Category: "food", Description: "x",
		Date: time.Date(2026, 10, 1, 15, 30, 0, 0, time.FixedZone("x", 3*3600)),
	})
	require.NoError(t, err)
	assert.Equal(t, 10.01, saved.Amount)
	assert.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), saved.Date)
	assert.Equal(t, "x", saved.Description)
}

func TestAddTransactionSequentialIDs(t *testing.T) {
	svc := newService()
	for want := 1; want <= 3; want++ {
		saved, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 1, Category: "x", Date: day(10, 1)})
		require.NoError(t, err)
		assert.Equal(t, want, saved.ID)
	}
}

func TestAddTransactionIsAtomic(t *testing.T) {
	sp := newSpy()
	svc := sp.service()
	ctx := context.Background()
	require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "food", Limit: 100}))
	sp.reset()

	_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: 10, Category: "food", Date: day(10, 1)})
	require.NoError(t, err)

	sp.mu.Lock()
	defer sp.mu.Unlock()
	want := []call{
		{"Budgets.GetByCategoryForUpdate", true},
		{"Expenses.SumByCategory", true},
		{"Expenses.Add", true},
	}
	assert.Equal(t, want, sp.calls, "блокировка, затем сумма, затем вставка, всё внутри транзакции")
}

func TestAddTransactionWithoutBudgetStillInTx(t *testing.T) {
	sp := newSpy()
	svc := sp.service()
	_, err := svc.AddTransaction(context.Background(), domain.Transaction{Amount: 10, Category: "free", Date: day(10, 1)})
	require.NoError(t, err)

	sp.mu.Lock()
	defer sp.mu.Unlock()
	want := []call{{"Budgets.GetByCategoryForUpdate", true}, {"Expenses.Add", true}}
	assert.Equal(t, want, sp.calls)
}

func TestConcurrentAddRespectsLimit(t *testing.T) {
	svc := newService()
	ctx := context.Background()
	require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "a", Limit: 30}))
	require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "b", Limit: 10}))

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		okA  int
		okB  int
		fail []error
	)
	for i := range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cat := "a"
			if i%2 == 1 {
				cat = "b"
			}
			_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: 1, Category: cat, Date: day(10, 1)})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil && cat == "a":
				okA++
			case err == nil:
				okB++
			case !errors.Is(err, domain.ErrBudgetExceeded):
				fail = append(fail, err)
			}
		}()
	}
	wg.Wait()
	require.Empty(t, fail)
	assert.Equal(t, 30, okA)
	assert.Equal(t, 10, okB)

	list, err := svc.ListTransactions(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 40)
}

func TestCanceledContext(t *testing.T) {
	svc := newService()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: 1, Category: "x", Date: day(10, 1)})
	assert.ErrorIs(t, err, context.Canceled)
	assert.ErrorIs(t, svc.SetBudget(ctx, domain.Budget{Category: "x", Limit: 1}), context.Canceled)

	list, err := svc.ListTransactions(context.Background())
	require.NoError(t, err)
	assert.Empty(t, list)
	budgets, err := svc.ListBudgets(context.Background())
	require.NoError(t, err)
	assert.Empty(t, budgets)
}

func TestBudgets(t *testing.T) {
	svc := newService()
	ctx := context.Background()

	got, err := svc.ListBudgets(ctx)
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)

	for _, c := range []string{"яблоки", "авто", "быт"} {
		require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: c, Limit: 1}))
	}
	require.NoError(t, svc.SetBudget(ctx, domain.Budget{Category: "авто", Limit: 2}))
	got, err = svc.ListBudgets(ctx)
	require.NoError(t, err)
	assert.Equal(t, []domain.Budget{{Category: "авто", Limit: 2}, {Category: "быт", Limit: 1}, {Category: "яблоки", Limit: 1}}, got)
}

func TestListTransactions(t *testing.T) {
	svc := newService()
	ctx := context.Background()
	got, err := svc.ListTransactions(ctx)
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)

	for _, c := range []string{"a", "b"} {
		_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: 1, Category: c, Date: day(10, 1)})
		require.NoError(t, err)
	}
	got, err = svc.ListTransactions(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, []string{"a", "b"}, []string{got[0].Category, got[1].Category})
}

func seeded(t *testing.T) Service {
	t.Helper()
	svc := newService()
	for _, tx := range []domain.Transaction{
		{Amount: 100.10, Category: "food", Date: day(10, 1)},
		{Amount: 50.20, Category: "food", Date: day(10, 10)},
		{Amount: 30, Category: "transport", Date: day(10, 5)},
		{Amount: 999, Category: "food", Date: day(9, 30)},
		{Amount: 999, Category: "health", Date: day(10, 11)},
	} {
		_, err := svc.AddTransaction(context.Background(), tx)
		require.NoError(t, err)
	}
	return svc
}

func TestGetReportSummary(t *testing.T) {
	got, err := seeded(t).GetReportSummary(context.Background(), day(10, 1), day(10, 10))
	require.NoError(t, err)
	assert.Equal(t, domain.ReportSummary{
		From:  "2026-10-01",
		To:    "2026-10-10",
		Total: 180.30,
		Categories: []domain.CategoryTotal{
			{Category: "food", Total: 150.30},
			{Category: "transport", Total: 30},
		},
	}, got)
}

func TestGetReportSummarySingleDayAndEmpty(t *testing.T) {
	svc := seeded(t)
	got, err := svc.GetReportSummary(context.Background(), day(10, 5), day(10, 5))
	require.NoError(t, err)
	assert.Equal(t, 30.0, got.Total)

	empty, err := newService().GetReportSummary(context.Background(), day(10, 1), day(10, 31))
	require.NoError(t, err)
	assert.NotNil(t, empty.Categories)
	assert.Empty(t, empty.Categories)
	assert.Equal(t, 0.0, empty.Total)
}

func TestGetReportSummaryRoundsToCents(t *testing.T) {
	svc := newService()
	ctx := context.Background()
	for range 3 {
		_, err := svc.AddTransaction(ctx, domain.Transaction{Amount: 0.1, Category: "a", Date: day(10, 1)})
		require.NoError(t, err)
	}
	got, err := svc.GetReportSummary(ctx, day(10, 1), day(10, 1))
	require.NoError(t, err)
	assert.Equal(t, 0.3, got.Total)
	assert.Equal(t, 0.3, got.Categories[0].Total)
}

func TestGetReportSummaryValidation(t *testing.T) {
	sp := newSpy()
	svc := sp.service()
	tests := []struct {
		name     string
		from, to time.Time
		wantMsg  string
	}{
		{"zero from", time.Time{}, day(10, 1), "from must be set"},
		{"zero to", day(10, 1), time.Time{}, "to must be set"},
		{"from after to", day(10, 2), day(10, 1), "from must not be after to"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.GetReportSummary(context.Background(), tt.from, tt.to)
			var ve *domain.ValidationError
			require.ErrorAs(t, err, &ve)
			assert.Equal(t, tt.wantMsg, ve.Message)
		})
	}
	assert.Empty(t, sp.names())

	// разное время суток в один день - это не "from после to"
	_, err := svc.GetReportSummary(context.Background(), day(10, 1).Add(20*time.Hour), day(10, 1).Add(time.Hour))
	assert.NoError(t, err)
}
