// Package ledger - публичная точка входа в учёт расходов и бюджетов.
//
// Весь код лежит в ledger/internal (domain, service, repository, app), а другие модули (gateway) не могут
// импортировать internal-пакеты. Поэтому этот пакет только выводит наружу нужные типы алиасами и фабрику.
// Алиасы дают те же самые типы, так что errors.Is и errors.As работают у вызывающего кода.
package ledger

import (
	"context"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/app"
	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/service"
)

type (
	Service         = service.Service
	Transaction     = domain.Transaction
	Budget          = domain.Budget
	Validatable     = domain.Validatable
	ValidationError = domain.ValidationError
	ReportSummary   = domain.ReportSummary
	CategoryTotal   = domain.CategoryTotal
)

var ErrBudgetExceeded = domain.ErrBudgetExceeded

// New подключается к PostgreSQL и Redis (настройки из окружения) и возвращает сервис
// и функцию закрытия соединений. Миграции не запускаются.
func New(ctx context.Context) (Service, func() error, error) {
	return app.New(ctx)
}

// NewInMemory возвращает сервис на репозиториях в памяти: для тестов и демо без БД.
func NewInMemory() Service {
	return app.NewInMemory()
}

func CheckValid(v Validatable) error {
	return domain.CheckValid(v)
}
