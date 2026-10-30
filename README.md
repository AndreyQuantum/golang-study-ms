# golang-study

Учебный проект: набор Go-сервисов для учёта личных финансов.

| Сервис    | Описание |
|-----------|----------|
| `gateway` | HTTP-шлюз на chi: REST API для транзакций, бюджетов и отчётов. Вызывает функции пакета `ledger`. |
| `ledger`  | Учёт транзакций и бюджетов по категориям с проверкой лимитов. Данные хранятся в PostgreSQL, отчёты и список бюджетов кешируются в Redis. |

Каждый сервис — отдельный Go-модуль. `gateway` подключает `ledger` через `replace => ../ledger` в `gateway/go.mod`, `go.work` не нужен.
Подробности по сервисам: [ledger/README.md](ledger/README.md), [gateway/README.md](gateway/README.md).

## Структура

```
docker-compose.yml        PostgreSQL 17 и Redis 7
ledger/
  ledger.go, store.go     фасад: Init, Close, AddTransaction, ListTransactions, SetBudget, ListBudgets, GetReportSummary, CheckValid, Reset
  entity/                 Transaction, Budget, ReportSummary, интерфейс Validatable, ValidationError
  repository/             хранилище в памяти (unit-тесты и режим без Init)
  internal/db/            подключение к PostgreSQL (pgx), DSN из окружения, пул соединений
  internal/cache/         Redis-клиент (go-redis v9), JSON с TTL
  internal/postgres/      хранилище на PostgreSQL
  migrations/             миграции goose
  cmd/ledger/             демо-программа
gateway/
  main.go                 HTTP-сервер на :8080, graceful shutdown
  internal/api/           роутер, обработчики, DTO, middleware
```

## Требования

- Go 1.26+
- Docker для PostgreSQL и Redis
- goose: `go install github.com/pressly/goose/v3/cmd/goose@latest`

## Быстрый старт

Команды выполняются из корня репозитория.

1. Поднять PostgreSQL и Redis:

   ```sh
   docker compose up -d
   ```

   Вместо compose можно запустить контейнеры вручную:

   ```sh
   docker run -d --name cashapp-postgres -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=cashapp -p 5432:5432 postgres:17-alpine
   docker run -d --name cashapp-redis -p 6379:6379 redis:7-alpine
   ```

2. Применить миграции (откат — та же команда с `down`):

   ```sh
   export DATABASE_URL="postgres://postgres:postgres@localhost:5432/cashapp?sslmode=disable"
   goose -dir ./ledger/migrations postgres "$DATABASE_URL" up
   ```

3. Запустить ledger: он покажет демонстрацию `CheckValid`, подключится к базам, загрузит бюджеты из `budgets.json` и напечатает данные.

   ```sh
   cd ledger && go run ./cmd/ledger -seed budgets.json && cd ..
   ```

4. Запустить gateway:

   ```sh
   cd gateway && go run .
   ```

Если порт 5432 уже занят, поменяйте проброс порта в `docker-compose.yml` и порт в `DATABASE_URL`.

### Переменные окружения

| Переменная       | По умолчанию     | Описание |
|------------------|------------------|----------|
| `DATABASE_URL`   | —                | полный DSN PostgreSQL, приоритетнее `DB_*` |
| `DB_HOST`, `DB_PORT` | `localhost`, `5432` | адрес PostgreSQL |
| `DB_USER`, `DB_PASS` | `postgres`, `postgres` | учётные данные |
| `DB_NAME`        | `cashapp`        | база данных, `sslmode=disable` |
| `REDIS_ADDR`     | `localhost:6379` | адрес Redis |
| `REDIS_DB`       | `0`              | номер базы Redis |
| `REDIS_PASSWORD` | пусто            | пароль Redis |

Ledger и gateway при старте вызывают `ledger.Init`: подключаются к PostgreSQL и Redis и пишут в лог `connected to PostgreSQL (...)` и
`connected to Redis (...)`. Если подключиться не удалось, процесс завершается с понятной ошибкой.

## API

Все ответы — JSON с `Content-Type: application/json; charset=utf-8`. Каждый запрос логируется: метод, путь, статус, длительность.

| Метод  | Путь                   | Описание | Успех |
|--------|------------------------|----------|-------|
| `GET`  | `/ping`                | `{"message":"pong"}` | 200 |
| `POST` | `/api/transactions`    | добавить транзакцию | 201 |
| `GET`  | `/api/transactions`    | список транзакций, новые сверху; пустой список — `[]` | 200 |
| `POST` | `/api/budgets`         | создать или обновить бюджет категории | 201 |
| `GET`  | `/api/budgets`         | список бюджетов по категориям; пустой список — `[]` | 200 |
| `GET`  | `/api/reports/summary` | суммы по категориям за интервал `?from=YYYY-MM-DD&to=YYYY-MM-DD` | 200 |

Тело транзакции: `{"amount":450,"category":"еда","description":"ланч","date":"2025-09-10"}`. Тело бюджета: `{"category":"еда","limit":5000}`.
Неизвестные поля, пустое тело, лишние данные после объекта и тело больше 1 MiB отклоняются с кодом 400.

Ошибки имеют вид `{"error":"текст"}`:

| Код | Когда |
|-----|-------|
| 400 | некорректный JSON, неверный формат даты, ошибка валидации (`amount must be positive`, `category must not be empty`, ...), отчёт без `from`/`to` или с `from` позже `to` |
| 404 | неизвестный путь |
| 405 | метод не поддерживается для пути |
| 409 | транзакция превышает бюджет категории: `{"error":"budget exceeded"}` |
| 500 | внутренняя ошибка: `{"error":"internal server error"}` |

Транзакция в категории без бюджета добавляется без ограничений.

### Примеры cURL

```sh
curl -X POST http://localhost:8080/api/budgets -H "Content-Type: application/json" -d '{"category":"еда","limit":500}'
# 201 {"category":"еда","limit":500}

curl -X POST http://localhost:8080/api/transactions \
  -H "Content-Type: application/json" \
  -d '{"amount":450,"category":"еда","description":"ланч","date":"2025-09-10"}'
# 201 {"id":1,"amount":450,"category":"еда","description":"ланч","date":"2025-09-10"}

curl -X POST http://localhost:8080/api/transactions -H "Content-Type: application/json" -d '{"amount":450,"category":"еда","description":"ужин","date":"2025-09-11"}'
# 409 {"error":"budget exceeded"}

curl -X POST http://localhost:8080/api/transactions -H "Content-Type: application/json" -d '{"amount":-1,"category":"еда","date":"2025-09-10"}'
# 400 {"error":"amount must be positive"}

curl http://localhost:8080/api/transactions
curl http://localhost:8080/api/budgets

curl "http://localhost:8080/api/reports/summary?from=2025-09-01&to=2025-09-30"
curl "http://localhost:8080/api/reports/summary?from=2025-09-01&to=2025-09-30"
# 200 {"from":"2025-09-01","to":"2025-09-30","total":450,"categories":[{"category":"еда","total":450}]}
```

Первый запрос отчёта считается в PostgreSQL, в логе gateway появляется `cache miss report:summary:2025-09-01:2025-09-30`. Повторный запрос
в течение 30 секунд отдаётся из Redis (`cache hit`). Кеш отчёта не сбрасывается при добавлении транзакций, поэтому отчёт может отставать
от данных до 30 секунд. Список бюджетов кешируется под ключом `budgets:all` на 30 секунд и удаляется после каждого `SetBudget`.
Если Redis недоступен во время работы, ошибка пишется в лог, а запрос выполняется напрямую через PostgreSQL.

Данные хранятся в PostgreSQL и переживают перезапуск ledger и gateway.

## Тесты

Тесты написаны на `testing` и `github.com/stretchr/testify`. Unit-тесты и тесты HTTP работают на хранилище в памяти и не требуют
PostgreSQL и Redis. Тесты, которые меняют состояние фасада, вызывают `ledger.Reset()` в начале и в `t.Cleanup`.

```sh
cd ledger
go test ./... -cover -coverprofile=cover.out
go tool cover -html=cover.out -o cover.html

cd ../gateway
go test ./... -cover -coverprofile=cover.out
go tool cover -html=cover.out -o cover.html
```

`cover.out` и `cover.html` лежат в `.gitignore`.

Интеграционные тесты `ledger/internal/postgres` пропускаются, пока не задана `LEDGER_TEST_DATABASE_URL`. Перед каждым тестом они очищают
таблицы, поэтому им нужна отдельная база с применёнными миграциями:

```sh
docker compose exec postgres createdb -U postgres cashapp_test
goose -dir ./ledger/migrations postgres "postgres://postgres:postgres@localhost:5432/cashapp_test?sslmode=disable" up
cd ledger
LEDGER_TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/cashapp_test?sslmode=disable" go test ./internal/postgres -cover
```

Покрытие тестами:

| Модуль / пакет                  | Покрытие |
|---------------------------------|----------|
| `ledger`, итог по модулю        | 53.7%    |
| `ledger` (фасад)                | 78.9%    |
| `ledger/entity`                 | 100.0%   |
| `ledger/repository`             | 98.3%    |
| `ledger/internal/cache`         | 97.2%    |
| `ledger/internal/db`            | 93.9%    |
| `ledger/internal/postgres`      | 83.1% с `LEDGER_TEST_DATABASE_URL`, без нее тесты пропускаются |
| `gateway`, итог по модулю       | 74.7%    |
| `gateway/internal/api`          | 88.5%    |

Итог по модулям ниже, чем по пакетам, потому что в него входят `main`-пакеты без тестов (`ledger/cmd/ledger`, `gateway/main.go`)
и пропущенные без базы интеграционные тесты. `Init` и `Close` в фасаде тестами не покрыты: им нужны работающие PostgreSQL и Redis.

## Домашние задания

| ДЗ | Что сделано |
|----|-------------|
| 2-3 | транзакции и бюджеты в памяти, проверка лимита, загрузка бюджетов из JSON |
| 4  | интерфейс `Validatable`, `Validate()` у `Transaction` и `Budget`, `CheckValid`, валидация перед сохранением |
| 5  | HTTP-слой gateway: DTO, маршруты `/api`, коды 400/409/500, middleware логирования и `Content-Type` |
| 6  | unit-тесты домена и тесты HTTP через `httptest`, `ledger.Reset()`, покрытие |
| 7  | PostgreSQL и миграции goose, кеш Redis для отчёта и списка бюджетов, эндпоинт `/api/reports/summary` |
