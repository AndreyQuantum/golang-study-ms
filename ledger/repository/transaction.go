package repository

import "github.com/AndreyQuantum/golang-study-ms/ledger/entity"

type TransactionRepository struct {
	transactions []entity.Transaction
}

func NewTransactionRepository() *TransactionRepository {
	return &TransactionRepository{
		transactions: []entity.Transaction{},
	}
}

func (r *TransactionRepository) AddTransaction(t entity.Transaction) entity.Transaction {
	t.ID = len(r.transactions) + 1
	r.transactions = append(r.transactions, t)
	return t
}

func (r *TransactionRepository) ListTransactions() []entity.Transaction {
	return r.transactions
}
