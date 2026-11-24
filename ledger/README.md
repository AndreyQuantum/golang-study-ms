# Ledger

Учёт транзакций и бюджетов по категориям. Данные хранятся в PostgreSQL, отчёты и список бюджетов кешируются в Redis на 30 секунд.
Код разложен по слоям (domain, repository, service, app), корневой пакет `ledger` - тонкая публичная точка входа, которую импортирует `gateway`;
демо-программа лежит в `cmd/ledger`. Глобального состояния нет: сервис создаёт фабрика `ledger.New(ctx)` (PostgreSQL и Redis)
или `ledger.NewInMemory()` (память, для тестов и демо).

## PostgreSQL и Redis

Через docker compose из корня репозитория:

```sh
docker compose up -d
```

или двумя командами `docker run`:

```sh
docker run -d --name cashapp-postgres -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=cashapp -p 5432:5432 postgres:17-alpine
docker run -d --name cashapp-redis -p 6379:6379 redis:7-alpine
```

## Миграции

```sh
go install github.com/pressly/goose/v3/cmd/goose@latest
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/cashapp?sslmode=disable"

goose -dir ./ledger/migrations postgres "$DATABASE_URL" up
goose -dir ./ledger/migrations postgres "$DATABASE_URL" down
```

Команды выполняются из корня репозитория. Таблицы: `budgets` (`category` уникальна, `limit_amount > 0`) и `expenses`
(`amount <> 0`, индекс `expenses_category_date_idx` по `(category, date)`).

## Переменные окружения

| Переменная       | По умолчанию | Описание                                                       |
|------------------|--------------|----------------------------------------------------------------|
| `DATABASE_URL`   | —            | полный DSN PostgreSQL, приоритетнее `DB_*`                     |
| `DB_HOST`        | `localhost`  | хост PostgreSQL                                                |
| `DB_PORT`        | `5432`       | порт PostgreSQL                                                |
| `DB_USER`        | `postgres`   | пользователь                                                   |
| `DB_PASS`        | `postgres`   | пароль                                                         |
| `DB_NAME`        | `cashapp`    | база данных (`sslmode=disable`)                                |
| `REDIS_ADDR`     | `localhost:6379` | адрес Redis                                                |
| `REDIS_DB`       | `0`          | номер базы Redis                                               |
| `REDIS_PASSWORD` | пусто        | пароль Redis, если нужен                                       |

## Запуск

Порядок: поднять PostgreSQL и Redis, применить миграции (`goose up`), запустить ledger, затем gateway.

```sh
cd ledger
go run ./cmd/ledger -seed budgets.json -demo
```

Флаги:

- `-seed budgets.json` — загрузить бюджеты из JSON-файла в базу через `Service.SetBudget`;
- `-demo` — добавить несколько тестовых транзакций за текущий месяц (в пределах лимита, сверх лимита, без бюджета, невалидную).

Программа показывает `CheckValid` на разных типах (без БД), подключается к PostgreSQL и Redis (`ledger.New`), печатает бюджеты,
транзакции и отчёт за текущий месяц. Отчёт запрашивается дважды: в логе первый запрос даёт `cache miss`, второй — `cache hit`.
Если подключиться не удалось, программа завершается с подсказкой: проверить, запущены ли PostgreSQL и Redis, верны ли
`DATABASE_URL`/`REDIS_*` и применены ли миграции.

## Кеш

| Ключ                              | Что хранит                 | TTL | Инвалидация                  |
|-----------------------------------|----------------------------|-----|------------------------------|
| `report:summary:<from>:<to>`      | отчёт за интервал          | 30s | только по истечении TTL      |
| `budgets:all`                     | список бюджетов            | 30s | `DEL` после успешного `SetBudget` |

Ошибки Redis только логируются, запрос выполняется напрямую через PostgreSQL.

## Слои и зависимости

```
gateway ──> ledger (публичный пакет: алиасы типов, Service, New, NewInMemory)
                │
                ▼
           internal/app        фабрика: окружение, подключение, сборка
              │     │
              ▼     ▼
  internal/service  internal/repository/{pg,memory}
              │            │
              └──> internal/domain <──┘     сущности, валидация, интерфейсы репозиториев
```

- `internal/domain` зависит только от стандартной библиотеки: сущности, валидация, `Money` (целые центы), ошибки `ErrBudgetExceeded` и `ErrNotFound`,
  интерфейсы `BudgetRepository`, `ExpenseRepository` и `TxManager`.
- `internal/repository/pg` и `internal/repository/memory` реализуют интерфейсы domain. В pg только SQL, бизнес-логики там нет. Они не знают про сервис и фабрику.
- `internal/service` - вся бизнес-логика. Знает только интерфейсы domain: ни `database/sql`, ни pgx, ни Redis, ни конкретные репозитории.
- `internal/app` - единственное место, где собирают всё вместе и знают про `internal/db` и `internal/cache`.
- Корневой пакет `ledger` зависит от `domain`, `service` и `app`.

Каталог `internal` закрыт для других модулей, а `gateway` - отдельный модуль (`replace => ../ledger`), поэтому он не может импортировать
`ledger/internal/...`. Корневой пакет `ledger` выводит наружу всё нужное алиасами типов: `ledger.Service = service.Service`,
`ledger.Transaction = domain.Transaction`, `ledger.Budget`, `ledger.ReportSummary`, `ledger.CategoryTotal`, `ledger.ValidationError`
и `ledger.ErrBudgetExceeded`. Алиас - это тот же самый тип, поэтому `errors.Is` и `errors.As` работают в gateway. Направление зависимостей
проверяют arch-тесты `ledger/arch_test.go` и `gateway/arch_test.go`.

## Публичный API

```go
type Service interface {
    SetBudget(ctx context.Context, b Budget) error
    ListBudgets(ctx context.Context) ([]Budget, error)
    AddTransaction(ctx context.Context, tx Transaction) (Transaction, error)
    ListTransactions(ctx context.Context) ([]Transaction, error)
    GetReportSummary(ctx context.Context, from, to time.Time) (ReportSummary, error)
}

func New(ctx context.Context) (Service, func() error, error) // PostgreSQL + Redis
func NewInMemory() Service                                     // репозитории в памяти, без кеша
func CheckValid(v Validatable) error
var ErrBudgetExceeded error
```

Первым параметром у всех методов идёт `ctx`. `SetBudget` и `AddTransaction` сначала вызывают `Validate`; `GetReportSummary` требует непустые
`from`/`to` и `from <= to`, иначе возвращает `*ValidationError`. Транзакция в категории без бюджета добавляется без ограничений.

### Фабрика

`ledger.New(ctx)` читает окружение (`DATABASE_URL` или `DB_*`, `REDIS_*`), подключается к PostgreSQL и Redis, проверяя оба соединения (`ctx` доходит
до `Ping`), пишет в лог `connected to PostgreSQL (...)` и `connected to Redis (...)` и возвращает `(Service, closeFn, error)`. Сервис собран
как `service.NewCached(service.New(repos, txManager), redis)`. При ошибке возвращается `nil, nil, err`, а уже открытые соединения закрываются.
`closeFn` закрывает Redis и пул PostgreSQL, повторный вызов безопасен. Миграции фабрика не запускает: схема применяется отдельно (`goose up`).

### Атомарность проверки лимита

Правило "сумма расходов по категории + новая сумма <= лимит" живёт в сервисе (`internal/service`), а не в SQL. Чтобы параллельные запросы
не превысили лимит вместе, сервис выполняет проверку и вставку внутри `TxManager.WithinTx`:

1. `Budgets.GetByCategoryForUpdate(category)` - читает бюджет и блокирует его;
2. `Expenses.SumByCategory(category)` - сумма в центах;
3. если `sum + новая > лимит` - `ErrBudgetExceeded`, иначе `Expenses.Add` (последняя операция внутри транзакции).

Метод `GetByCategoryForUpdate` по контракту читает бюджет и гарантирует, что он не изменится и что параллельная проверка лимита по той же категории подождёт
до конца `WithinTx`. Это требование бизнес-правила, а не способ хранения. `TxManager` реализован дважды: в `pg` это одна транзакция БД (READ COMMITTED)
и `SELECT ... FOR UPDATE` по строке бюджета, в `memory` - общий mutex на всё время вызова. Если бюджета у категории нет, блокировки и лимита нет.
Порядок "блокировка, сумма, вставка" закреплён spy-тестом сервиса, отсутствие перерасхода при гонке проверяет интеграционный тест в `internal/app`.
Деньги сравниваются в целых центах (`domain.Money`), поэтому `0.1 + 0.2` укладывается в лимит `0.3`.

### Контекст и отмена

Gateway передаёт во все методы сервиса контекст запроса (`r.Context()`) с таймаутом 5 секунд. Раньше (HW7) записи и списки шли с `context.Background()` и таймаутом
5 секунд, отчёт - с контекстом запроса без таймаута. Теперь отключение клиента отменяет работу с БД (запрос завершается ответом 500), а отчёт ограничен 5 секундами.
Это осознанное изменение, оно нужно для HW9. Отмена в середине транзакции откатывает её, блокировка снимается. Ошибки Redis, в том числе из-за отменённого
контекста, только логируются.

## Валидация

Пакет `internal/domain` содержит интерфейс `Validatable` с методом `Validate() error` и функцию `CheckValid(v Validatable) error`, которая работает
с любым типом, реализующим интерфейс (`Transaction` и `Budget`). Ошибка валидации имеет тип `*domain.ValidationError` (в gateway - `*ledger.ValidationError`) с полями `Field`
и `Message` и распознаётся через `errors.As`. `Validate` только возвращает ошибку и ничего не логирует.

| Тип           | Правило                                              | Сообщение |
|---------------|------------------------------------------------------|-----------|
| `Transaction` | `Amount` не NaN/Inf и больше 0                       | `amount must be positive` |
| `Transaction` | `Amount` от `0.01` до `999999999999.99`              | `amount must be between 0.01 and 999999999999.99` |
| `Transaction` | `Category` не пустая после `TrimSpace`               | `category must not be empty` |
| `Transaction` | `Date` задана                                        | `date must be set` |
| `Budget`      | `Limit` не NaN/Inf и больше 0                        | `limit must be positive` |
| `Budget`      | `Limit` от `0.01` до `999999999999.99`               | `limit must be between 0.01 and 999999999999.99` |
| `Budget`      | `Category` не пустая после `TrimSpace`               | `category must not be empty` |

Диапазон сумм соответствует типу `NUMERIC(14,2)` в базе. Сравнение лимита идёт в целых центах и для памяти, и для PostgreSQL, поэтому
`0.1 + 0.2` укладывается в лимит `0.3`.

## Тесты

```sh
cd ledger
go test ./... -cover
```

Юнит-тесты сервиса работают на `internal/repository/memory` и spy-обёртке, репозитории memory и pg проходят один и тот же контракт-тест
(`internal/repository/repotest`). Интеграционные тесты `internal/repository/pg` и `internal/app` пропускаются, пока не задана `LEDGER_TEST_DATABASE_URL`.
Перед каждым тестом они выполняют `TRUNCATE budgets, expenses RESTART IDENTITY`, поэтому нужна отдельная база с применёнными миграциями.
БД-пакеты берут advisory-блокировку на весь прогон (`internal/testdb`) и идут по очереди; Redis в `app` заменён `miniredis`:

```sh
docker compose exec postgres createdb -U postgres cashapp_test
goose -dir migrations postgres "postgres://postgres:postgres@localhost:5432/cashapp_test?sslmode=disable" up
LEDGER_TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/cashapp_test?sslmode=disable" go test ./internal/repository/pg ./internal/app -v
```
