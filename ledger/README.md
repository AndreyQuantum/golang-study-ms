# Ledger

Учёт транзакций и бюджетов по категориям. Данные хранятся в PostgreSQL, отчёты и список бюджетов кешируются в Redis на 30 секунд.
Корневой пакет `ledger` — фасад, который импортирует `gateway`; демо-программа лежит в `cmd/ledger`.
Без вызова `ledger.Init` фасад работает на хранилище в памяти (так устроены unit-тесты).

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

- `-seed budgets.json` — загрузить бюджеты из JSON-файла в базу через `ledger.SetBudget`;
- `-demo` — добавить несколько тестовых транзакций за текущий месяц (в пределах лимита, сверх лимита, без бюджета, невалидную).

Программа показывает `CheckValid` на разных типах (без БД), подключается к PostgreSQL и Redis (`ledger.Init`), печатает бюджеты,
транзакции и отчёт за текущий месяц. Отчёт запрашивается дважды: в логе первый запрос даёт `cache miss`, второй — `cache hit`.
Если подключиться не удалось, программа завершается с подсказкой: проверить, запущены ли PostgreSQL и Redis, верны ли
`DATABASE_URL`/`REDIS_*` и применены ли миграции.

## Кеш

| Ключ                              | Что хранит                 | TTL | Инвалидация                  |
|-----------------------------------|----------------------------|-----|------------------------------|
| `report:summary:<from>:<to>`      | отчёт за интервал          | 30s | только по истечении TTL      |
| `budgets:all`                     | список бюджетов            | 30s | `DEL` после успешного `SetBudget` |

Ошибки Redis только логируются, запрос выполняется напрямую через PostgreSQL.

## Публичный API фасада

`Init`, `Close`, `SetBudget`, `ListBudgets`, `AddTransaction`, `ListTransactions`, `GetReportSummary(ctx, from, to)`, `CheckValid`, `Reset`
(сброс на чистое хранилище в памяти). `SetBudget` и `AddTransaction` сначала вызывают `Validate`; `GetReportSummary` требует непустые
`from`/`to` и `from <= to`, иначе возвращает `*ValidationError`. Превышение бюджета в PostgreSQL проверяется в одной транзакции БД
с блокировкой строки бюджета (`FOR UPDATE`), сравнение суммы с лимитом выполняется в `NUMERIC`. Транзакция в категории без бюджета
добавляется без ограничений.

## Валидация

Пакет `entity` содержит интерфейс `Validatable` с методом `Validate() error` и функцию `CheckValid(v Validatable) error`, которая работает
с любым типом, реализующим интерфейс (`Transaction` и `Budget`). Ошибка валидации имеет тип `*entity.ValidationError` с полями `Field`
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

Диапазон сумм соответствует типу `NUMERIC(14,2)` в базе. В хранилище в памяти превышение лимита сравнивается в центах, поэтому
`0.1 + 0.2` укладывается в лимит `0.3`.

## Тесты

```sh
cd ledger
go test ./... -cover
```

Интеграционные тесты `internal/postgres` пропускаются, пока не задана `LEDGER_TEST_DATABASE_URL`. Перед каждым тестом они
выполняют `TRUNCATE budgets, expenses RESTART IDENTITY`, поэтому нужна отдельная база с применёнными миграциями:

```sh
docker compose exec postgres createdb -U postgres cashapp_test
goose -dir migrations postgres "postgres://postgres:postgres@localhost:5432/cashapp_test?sslmode=disable" up
LEDGER_TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/cashapp_test?sslmode=disable" go test ./internal/postgres -v
```
