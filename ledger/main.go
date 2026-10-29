package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger/entity"
	"github.com/AndreyQuantum/golang-study-ms/ledger/repository"
)

func main() {
	budgets := repository.NewBudgetRepository()
	repo := repository.NewTransactionRepository(budgets)
	fmt.Println("Ledger service started")

	demoCheckValid()

	if err := loadBudgetsFromFile(budgets, "budgets.json"); err != nil {
		log.Fatalf("load budgets: %v", err)
	}
	setBudget(budgets, entity.Budget{Category: "entertainment", Limit: 3000})
	printBudgets(budgets)

	fmt.Println("\n== Сценарий 1: транзакция в пределах лимита")
	addTransaction(repo, entity.Transaction{
		Amount:      450.50,
		Category:    "food",
		Description: "Продукты в магазине",
		Date:        time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	})

	fmt.Println("\n== Сценарий 2: транзакция превышает лимит")
	restaurant := entity.Transaction{
		Amount:      4800,
		Category:    "food",
		Description: "Ресторан",
		Date:        time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
	}
	addTransaction(repo, restaurant)

	fmt.Println("\n== Сценарий 3: увеличиваем лимит через SetBudget и повторяем")
	setBudget(budgets, entity.Budget{Category: "food", Limit: 6000})
	fmt.Println("Новый лимит food: 6000.00")
	addTransaction(repo, restaurant)

	fmt.Println("\n== Сценарий 4: транзакция ровно на лимит")
	addTransaction(repo, entity.Transaction{
		Amount:      3000,
		Category:    "transport",
		Description: "Такси в аэропорт",
		Date:        time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC),
	})

	fmt.Println("\n== Сценарий 5: бюджет задан через SetBudget, лимит превышен")
	addTransaction(repo, entity.Transaction{
		Amount:      3500,
		Category:    "entertainment",
		Description: "Билеты на концерт",
		Date:        time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC),
	})

	fmt.Println("\n== Сценарий 6: категория без бюджета")
	addTransaction(repo, entity.Transaction{
		Amount:      10000,
		Category:    "health",
		Description: "Стоматолог",
		Date:        time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC),
	})

	fmt.Println("\n== Сценарий 7: невалидная транзакция (отрицательная сумма)")
	addTransaction(repo, entity.Transaction{
		Amount:      -500,
		Category:    "food",
		Description: "Возврат",
		Date:        time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
	})

	fmt.Println("\n== Сохранённые транзакции (отклонённых здесь быть не должно)")
	for _, t := range repo.ListTransactions() {
		fmt.Printf("#%d | %s | %-13s | %8.2f | %s\n",
			t.ID, t.Date.Format("2006-01-02"), t.Category, t.Amount, t.Description)
	}
}

func demoCheckValid() {
	fmt.Println("\n== CheckValid: одна функция, разные типы")
	items := []struct {
		name string
		v    entity.Validatable
	}{
		{"Transaction (валидная)", entity.Transaction{
			Amount:   100,
			Category: "food",
			Date:     time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		}},
		{"Transaction (amount = NaN)", entity.Transaction{
			Amount:   math.NaN(),
			Category: "food",
			Date:     time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		}},
		{"Budget (валидный)", entity.Budget{Category: "food", Limit: 5000}},
		{"Budget (лимит 0)", entity.Budget{Category: "food", Limit: 0}},
	}
	for _, it := range items {
		if err := entity.CheckValid(it.v); err != nil {
			fmt.Printf("%-27s -> ошибка: %v\n", it.name, err)
			continue
		}
		fmt.Printf("%-27s -> OK\n", it.name)
	}
	fmt.Println()
}

func loadBudgetsFromFile(budgets *repository.BudgetRepository, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open budgets file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err := budgets.LoadBudgets(bufio.NewReader(f)); err != nil {
		return fmt.Errorf("load budgets from %s: %w", path, err)
	}
	return nil
}

func setBudget(budgets *repository.BudgetRepository, b entity.Budget) {
	if err := budgets.SetBudget(b); err != nil {
		log.Fatalf("set budget %q: %v", b.Category, err)
	}
}

func addTransaction(repo *repository.TransactionRepository, tx entity.Transaction) {
	err := repo.AddTransaction(tx)
	var ve *entity.ValidationError
	switch {
	case err == nil:
		fmt.Printf("OK: %q (%s, %.2f) добавлена\n", tx.Description, tx.Category, tx.Amount)
	case errors.Is(err, repository.ErrBudgetExceeded):
		fmt.Printf("ОТКАЗ: %q не добавлена: %v\n", tx.Description, err)
	case errors.As(err, &ve):
		fmt.Printf("ОТКАЗ (валидация): %q не добавлена: %v\n", tx.Description, err)
	default:
		fmt.Printf("ОШИБКА: %q: %v\n", tx.Description, err)
	}
}

func printBudgets(budgets *repository.BudgetRepository) {
	fmt.Println("Бюджеты:")
	for _, b := range budgets.ListBudgets() {
		fmt.Printf("  %-13s %8.2f\n", b.Category, b.Limit)
	}
}
