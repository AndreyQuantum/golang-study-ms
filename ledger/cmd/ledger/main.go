package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger"
	"github.com/AndreyQuantum/golang-study-ms/ledger/repository"
)

func main() {
	seed := flag.String("seed", "", "path to a JSON file with budgets to load into the database, e.g. budgets.json")
	demo := flag.Bool("demo", false, "add sample transactions for the current month")
	flag.Parse()

	fmt.Println("Ledger service started")
	demoCheckValid()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := ledger.Init(ctx); err != nil {
		log.Fatalf("init ledger: %v\ncheck that PostgreSQL and Redis are running, DATABASE_URL / DB_* / REDIS_* are correct and migrations are applied (goose up)", err)
	}
	if err := run(*seed, *demo); err != nil {
		log.Printf("%v", err)
		if cerr := ledger.Close(); cerr != nil {
			log.Printf("close ledger: %v", cerr)
		}
		os.Exit(1)
	}
	if err := ledger.Close(); err != nil {
		log.Printf("close ledger: %v", err)
	}
}

func run(seed string, demo bool) error {
	if seed != "" {
		if err := seedBudgets(seed); err != nil {
			return fmt.Errorf("seed budgets: %w", err)
		}
	}

	if demo {
		runDemo()
	}

	printBudgets()
	printTransactions()
	printReport()
	return nil
}

func demoCheckValid() {
	fmt.Println("\n== CheckValid: одна функция, разные типы")
	date := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	items := []struct {
		name string
		v    ledger.Validatable
	}{
		{"Transaction (валидная)", ledger.Transaction{Amount: 100, Category: "food", Date: date}},
		{"Transaction (amount = NaN)", ledger.Transaction{Amount: math.NaN(), Category: "food", Date: date}},
		{"Budget (валидный)", ledger.Budget{Category: "food", Limit: 5000}},
		{"Budget (лимит 0)", ledger.Budget{Category: "food", Limit: 0}},
	}
	for _, it := range items {
		if err := ledger.CheckValid(it.v); err != nil {
			fmt.Printf("%-27s -> ошибка: %v\n", it.name, err)
			continue
		}
		fmt.Printf("%-27s -> OK\n", it.name)
	}
	fmt.Println()
}

func seedBudgets(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open budgets file: %w", err)
	}
	defer func() { _ = f.Close() }()

	budgets, err := repository.ParseBudgets(f)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	for _, b := range budgets {
		if err := ledger.SetBudget(b); err != nil {
			return fmt.Errorf("set budget %q: %w", b.Category, err)
		}
	}
	fmt.Printf("Загружено бюджетов из %s: %d\n", path, len(budgets))
	return nil
}

func runDemo() {
	now := time.Now().UTC()
	day := func(d int) time.Time {
		return time.Date(now.Year(), now.Month(), d, 0, 0, 0, 0, time.UTC)
	}
	steps := []struct {
		title string
		tx    ledger.Transaction
	}{
		{"в пределах лимита", ledger.Transaction{Amount: 450.50, Category: "food", Description: "Продукты", Date: day(1)}},
		{"превышает лимит", ledger.Transaction{Amount: 4800, Category: "food", Description: "Ресторан", Date: day(2)}},
		{"категория без бюджета", ledger.Transaction{Amount: 10000, Category: "health", Description: "Стоматолог", Date: day(3)}},
		{"невалидная (отрицательная сумма)", ledger.Transaction{Amount: -500, Category: "food", Description: "Возврат", Date: day(4)}},
	}
	for _, s := range steps {
		fmt.Printf("\n== Демо: %s\n", s.title)
		addTransaction(s.tx)
	}
}

func addTransaction(tx ledger.Transaction) {
	_, err := ledger.AddTransaction(tx)
	var ve *ledger.ValidationError
	switch {
	case err == nil:
		fmt.Printf("OK: %q (%s, %.2f) добавлена\n", tx.Description, tx.Category, tx.Amount)
	case errors.Is(err, ledger.ErrBudgetExceeded):
		fmt.Printf("ОТКАЗ: %q не добавлена: %v\n", tx.Description, err)
	case errors.As(err, &ve):
		fmt.Printf("ОТКАЗ (валидация): %q не добавлена: %v\n", tx.Description, err)
	default:
		fmt.Printf("ОШИБКА: %q: %v\n", tx.Description, err)
	}
}

func printBudgets() {
	budgets, err := ledger.ListBudgets()
	if err != nil {
		log.Printf("list budgets: %v", err)
		return
	}
	fmt.Println("\nБюджеты:")
	for _, b := range budgets {
		fmt.Printf("  %-13s %8.2f\n", b.Category, b.Limit)
	}
}

func printTransactions() {
	txs, err := ledger.ListTransactions()
	if err != nil {
		log.Printf("list transactions: %v", err)
		return
	}
	fmt.Println("\nТранзакции:")
	for _, t := range txs {
		fmt.Printf("#%d | %s | %-13s | %8.2f | %s\n",
			t.ID, t.Date.Format("2006-01-02"), t.Category, t.Amount, t.Description)
	}
}

func printReport() {
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, -1)

	fmt.Printf("\nОтчет за %s - %s (запрашиваем дважды, смотрите cache miss / cache hit в логе):\n",
		from.Format("2006-01-02"), to.Format("2006-01-02"))
	for range 2 {
		report, err := ledger.GetReportSummary(context.Background(), from, to)
		if err != nil {
			log.Printf("report summary: %v", err)
			return
		}
		fmt.Printf("  итого: %.2f\n", report.Total)
		for _, c := range report.Categories {
			fmt.Printf("  %-13s %8.2f\n", c.Category, c.Total)
		}
	}
}
