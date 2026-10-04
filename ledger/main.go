package main

import (
	"fmt"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/ledger/entity"
	"github.com/AndreyQuantum/golang-study-ms/ledger/repository"
)

func main() {
	repo := repository.NewTransactionRepository()
	fmt.Println("Ledger service started")

	repo.AddTransaction(entity.Transaction{
		Amount:      450.50,
		Category:    "food",
		Description: "Продукты в магазине",
		Date:        time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	})
	repo.AddTransaction(entity.Transaction{
		Amount:      1200,
		Category:    "transport",
		Description: "Проездной на месяц",
		Date:        time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
	})
	repo.AddTransaction(entity.Transaction{
		Amount:      3500,
		Category:    "entertainment",
		Description: "Билеты в кино",
		Date:        time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC),
	})

	for _, t := range repo.ListTransactions() {
		fmt.Printf("#%d | %s | %-13s | %8.2f | %s\n",
			t.ID, t.Date.Format("2006-01-02"), t.Category, t.Amount, t.Description)
	}
}
