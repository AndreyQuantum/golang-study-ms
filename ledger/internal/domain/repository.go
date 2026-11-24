package domain

import (
	"context"
	"time"
)

// BudgetRepository - хранилище бюджетов. Описывает то, что нужно бизнес-логике, а не способ хранения.
type BudgetRepository interface {
	// Upsert добавляет бюджет или обновляет существующий по category.
	Upsert(ctx context.Context, b Budget) error
	// GetByCategory возвращает ErrNotFound, если бюджета нет.
	GetByCategory(ctx context.Context, category string) (Budget, error)
	// GetByCategoryForUpdate читает бюджет и гарантирует, что он не изменится и что параллельная
	// проверка лимита по той же категории подождёт до конца WithinTx. Вызывать только внутри
	// TxManager.WithinTx. Это не способ хранения, а требование правила "проверка лимита и вставка
	// расхода атомарны" (PostgreSQL: SELECT ... FOR UPDATE; память: уже под mutex транзакции).
	GetByCategoryForUpdate(ctx context.Context, category string) (Budget, error)
	// List возвращает бюджеты по category по возрастанию, не nil.
	List(ctx context.Context) ([]Budget, error)
}

// ExpenseRepository - хранилище расходов.
type ExpenseRepository interface {
	// Add сохраняет расход и присваивает ID. Сумма и дата уже нормализованы сервисом.
	Add(ctx context.Context, t Transaction) (Transaction, error)
	// List возвращает расходы, не nil. Порядок определяет реализация.
	List(ctx context.Context) ([]Transaction, error)
	// SumByCategory возвращает сумму расходов категории, 0 если расходов нет.
	SumByCategory(ctx context.Context, category string) (Money, error)
	// TotalsByCategory возвращает суммы по категориям за интервал дат (включительно, дни UTC)
	// по category по возрастанию, не nil. Отчёт сервиса им больше не пользуется (он считает через
	// Categories и SumByCategoryBetween), но метод остаётся контрактом и эталоном в тестах.
	TotalsByCategory(ctx context.Context, from, to time.Time) ([]CategoryTotal, error)
	// Categories возвращает различные категории расходов за интервал дат (включительно, дни UTC),
	// по возрастанию, не nil.
	Categories(ctx context.Context, from, to time.Time) ([]string, error)
	// SumByCategoryBetween возвращает сумму расходов категории за интервал дат (включительно, дни UTC),
	// 0 если расходов нет.
	SumByCategoryBetween(ctx context.Context, category string, from, to time.Time) (Money, error)
}

// Repos - набор репозиториев, привязанных к одному контексту хранения (пулу или транзакции).
type Repos struct {
	Budgets  BudgetRepository
	Expenses ExpenseRepository
}

// TxManager выполняет группу операций атомарно.
type TxManager interface {
	// WithinTx выполняет fn атомарно: PostgreSQL - одна транзакция БД (READ COMMITTED), память - под общим mutex.
	// repos в fn привязаны к этой транзакции. Ошибка fn откатывает транзакцию и возвращается как есть (errors.Is работает).
	WithinTx(ctx context.Context, fn func(ctx context.Context, repos Repos) error) error
}
