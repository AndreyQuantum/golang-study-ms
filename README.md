# golang-study

Учебный проект: набор Go-сервисов для учёта личных финансов.

| Сервис    | Описание                                                      |
|-----------|---------------------------------------------------------------|
| `gateway` | HTTP-шлюз (chi): REST API для транзакций, бюджетов и отчётов поверх `ledger`; данные хранятся в PostgreSQL через `ledger`. |
| `ledger`  | Учёт транзакций и бюджетов по категориям, хранение в PostgreSQL, кеш отчётов в Redis. |

Каждый сервис — отдельный Go-модуль, поэтому все команды ниже выполняются из папки сервиса.

## Требования

- Go 1.26+ (`go version`)
- Docker (для PostgreSQL и Redis) и `goose` для миграций

## PostgreSQL и Redis

Ledger хранит данные в PostgreSQL, отчёты и список бюджетов кешируются в Redis на 30 секунд. Gateway обращается к тем же эндпоинтам, но данные теперь лежат в PostgreSQL.

```sh
docker compose up -d

docker run -d --name cashapp-postgres -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=cashapp -p 5432:5432 postgres:17-alpine
docker run -d --name cashapp-redis -p 6379:6379 redis:7-alpine

go install github.com/pressly/goose/v3/cmd/goose@latest
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/cashapp?sslmode=disable"
goose -dir ./ledger/migrations postgres "$DATABASE_URL" up
goose -dir ./ledger/migrations postgres "$DATABASE_URL" down
```

Кеш бюджетов и отчётов может ненадолго отдать устаревшие данные: если `ListBudgets` прочитал старые строки, а параллельный `SetBudget` успел зафиксироваться и удалить `budgets:all`, первый вызов запишет старый список обратно, и он проживёт до истечения TTL (30 секунд).

Команды `docker run` — альтернатива `docker compose up -d`. Порядок запуска: `docker compose up -d`, `goose up`, затем ledger и gateway. Переменные окружения: `DATABASE_URL` (приоритетнее) или `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASS`, `DB_NAME` (по умолчанию `localhost`, `5432`, `postgres`, `postgres`, `cashapp`); `REDIS_ADDR` (`localhost:6379`), `REDIS_DB` (`0`), `REDIS_PASSWORD`. Подробности: [ledger/README.md](ledger/README.md), [gateway/README.md](gateway/README.md).

## Gateway

```sh
cd gateway
go run .
```

Перед запуском должны работать PostgreSQL и Redis, а миграции быть применены (см. раздел выше). Gateway вызывает `ledger.Init`
при старте (ошибка подключения — завершение с сообщением) и `ledger.Close` при остановке. Сервис слушает порт `8080` и
останавливается по `Ctrl+C` (graceful shutdown по SIGINT/SIGTERM). Данные хранятся в PostgreSQL и сохраняются между
перезапусками; отчёты кешируются в Redis на 30 секунд. Фасад `github.com/AndreyQuantum/golang-study-ms/ledger` подключён через
`replace` в `gateway/go.mod`. Все ответы — JSON с `Content-Type: application/json; charset=utf-8`; каждый запрос
логируется (метод, путь, статус, длительность).

### Эндпоинты

| Метод  | Путь                 | Описание                          | Успех |
|--------|----------------------|-----------------------------------|-------|
| `GET`  | `/ping`              | Проверка живости: `{"message":"pong"}` | 200 |
| `POST` | `/api/transactions`  | Добавить транзакцию               | 201   |
| `GET`  | `/api/transactions`  | Список транзакций (пустой — `[]`) | 200   |
| `POST` | `/api/budgets`       | Создать или обновить бюджет (upsert) | 201 |
| `GET`  | `/api/budgets`       | Список бюджетов по категориям (пустой — `[]`) | 200 |
| `GET`  | `/api/reports/summary` | Отчёт по категориям за интервал `?from=YYYY-MM-DD&to=YYYY-MM-DD` | 200 |

Тело транзакции: `{"amount": 450, "category": "еда", "description": "ланч", "date": "2025-09-10"}`, дата в формате `YYYY-MM-DD`.
Тело бюджета: `{"category": "еда", "limit": 5000}`. Неизвестные поля, пустое тело, лишние данные после объекта и тело больше 1 MiB отклоняются.

### Коды ответов и ошибки

| Код | Когда                                                                              |
|-----|------------------------------------------------------------------------------------|
| 200 | списки и отчёт |
| 201 | транзакция или бюджет сохранены                                                    |
| 400 | некорректный JSON, неверный формат даты, ошибка валидации (`amount must be positive`, ...), отчёт без `from`/`to` или с `from` позже `to` |
| 404 | неизвестный путь                                                                   |
| 405 | метод не поддерживается для пути                                                   |
| 409 | транзакция превышает бюджет категории: `{"error":"budget exceeded"}`               |
| 500 | внутренняя ошибка: `{"error":"internal server error"}`                             |

Все ошибки имеют вид `{"error":"текст"}`.

### Примеры cURL

```sh
curl -X POST http://localhost:8080/api/budgets -H "Content-Type: application/json" -d '{"category":"еда","limit":500}'

curl -X POST http://localhost:8080/api/transactions -H "Content-Type: application/json" -d '{"amount":450,"category":"еда","description":"ланч","date":"2025-09-10"}'
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

Два одинаковых запроса отчёта подряд: первый считается в PostgreSQL (`cache miss report:summary:...` в логе gateway), второй берётся
из Redis (`cache hit`). Отчёт кешируется на 30 секунд и не сбрасывается при добавлении транзакций, поэтому он может отставать до 30 секунд.

## Ledger

### Запуск

Нужны PostgreSQL, Redis и применённые миграции (раздел выше).

```sh
cd ledger
go run ./cmd/ledger -seed budgets.json -demo
```

Флаги:

- `-seed <файл>` — загрузить бюджеты из JSON-файла в PostgreSQL (например, [`ledger/budgets.json`](ledger/budgets.json)). Без флага бюджеты не загружаются, файл не нужен. Если загрузка не удалась, программа завершается с ненулевым кодом.
- `-demo` — добавить тестовые транзакции за текущий месяц: в пределах лимита, сверх лимита (отказ), категория без бюджета, невалидная сумма.

Формат файла бюджетов — JSON-массив объектов:

```json
[
  {"category": "food", "limit": 5000},
  {"category": "transport", "limit": 3000}
]
```

Сначала `main` показывает `CheckValid` на валидной и невалидной `Transaction` и на валидном и невалидном `Budget` (без базы):

```
== CheckValid: одна функция, разные типы
Transaction (валидная)      -> OK
Transaction (amount = NaN)  -> ошибка: amount must be positive
Budget (валидный)           -> OK
Budget (лимит 0)            -> ошибка: limit must be positive
```

Затем `ledger.Init` подключается к PostgreSQL и Redis (в логе `connected to PostgreSQL (...)`, `connected to Redis (...)`), после чего программа
печатает бюджеты, транзакции и отчёт за текущий месяц. Отчёт запрашивается дважды подряд: в логе первый запрос — `cache miss`, второй — `cache hit`.
Данные остаются в PostgreSQL после завершения программы и видны при следующем запуске и в gateway.

```
Отчёт за 2026-10-01 - 2026-10-31 (запрашиваем дважды, смотрите cache miss / cache hit в логе):
  итого: 450.50
  food            450.50
```

Если PostgreSQL или Redis недоступны, либо не применены миграции, программа завершится с понятной ошибкой `init ledger: ...`.

### Фасад

Корневой пакет `ledger` (`ledger/ledger.go`) — потокобезопасный фасад: `Init`, `Close`, `AddTransaction`, `ListTransactions`, `SetBudget`,
`ListBudgets`, `GetReportSummary`, `CheckValid`, `Reset`, а также алиасы типов и `ErrBudgetExceeded`. После `Init` данные хранятся в PostgreSQL,
отчёты и список бюджетов кешируются в Redis; по умолчанию (без `Init`) и после `Reset` используется хранилище в памяти — на нём работают unit-тесты.
Фасад не импортирует `gateway`, наоборот, `gateway` импортирует фасад `ledger`; демо-программа `ledger/cmd/ledger` использует тот же фасад.

### Валидация

Пакет `entity` содержит интерфейс `Validatable` с методом `Validate() error` и функцию `CheckValid(v Validatable) error`,
которая работает с любым типом, реализующим интерфейс (`Transaction` и `Budget`). Ошибка валидации — `*entity.ValidationError`
(поля `Field` и `Message`), её можно распознать через `errors.As`. `Validate` возвращает первую найденную ошибку.

| Тип           | Правило                                   | Сообщение                      |
|---------------|-------------------------------------------|--------------------------------|
| `Transaction` | `Amount` — не NaN/Inf и больше 0          | `amount must be positive`      |
| `Transaction` | `Category` не пустая (после `TrimSpace`)  | `category must not be empty`   |
| `Transaction` | `Date` задана                             | `date must be set`             |
| `Budget`      | `Limit` — не NaN/Inf и больше 0           | `limit must be positive`       |
| `Budget`      | `Category` не пустая (после `TrimSpace`)  | `category must not be empty`   |
| `Transaction`, `Budget` | сумма / лимит в диапазоне `0.01`..`999999999999.99` | `amount/limit must be between 0.01 and 999999999999.99` |

Валидация встроена в репозитории: `AddTransaction` и `SetBudget` сначала вызывают `Validate` и при ошибке ничего не сохраняют,
`LoadBudgets` проверяет каждый элемент (`budget #1: limit must be positive`) и остаётся атомарным. Лимит `0` невалиден; суммы и лимиты должны быть от `0.01` до `999999999999.99` (NUMERIC(14,2) в базе).
Превышение лимита в хранилище в памяти сравнивается в центах, поэтому `0.1 + 0.2` укладывается в лимит `0.3`; PostgreSQL сравнивает суммы в `NUMERIC`.

## Тесты

Тесты написаны на стандартном `testing` с `github.com/stretchr/testify` (`assert`/`require`). Фасад `ledger` умеет сбрасывать
хранилище через `ledger.Reset()`, поэтому тесты, меняющие состояние, вызывают его в начале и в `t.Cleanup`.

```sh
cd ledger
go test ./... -cover -coverprofile=cover.out
go tool cover -func=cover.out | tail -1
go tool cover -html=cover.out -o cover.html

cd ../gateway
go test ./... -cover -coverprofile=cover.out
go tool cover -func=cover.out | tail -1
go tool cover -html=cover.out -o cover.html
```

`cover.out` и `cover.html` лежат в `.gitignore`.

Интеграционные тесты PostgreSQL запускаются на отдельной базе с применёнными миграциями (тесты делают `TRUNCATE budgets, expenses RESTART IDENTITY`):

```sh
docker compose exec postgres createdb -U postgres cashapp_test
goose -dir ledger/migrations postgres "postgres://postgres:postgres@localhost:5432/cashapp_test?sslmode=disable" up
cd ledger
LEDGER_TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/cashapp_test?sslmode=disable" go test ./internal/postgres -cover
```

Что покрыто:

- `ledger`: `Validate` для `Transaction` и `Budget` (табличные тесты с `t.Parallel`), `CheckValid`, сценарии бюджета через фасад
  (в пределах лимита, ровно на лимит, превышение без сохранения, категория без бюджета, невалидные объекты не сохраняются,
  повышение лимита, сортировка бюджетов, `Reset`, последовательные ID), конкурентное добавление;
- `ledger` (внутренние тесты): хранилище в памяти и отчёт по интервалу, кеширующий декоратор на фейковом кеше (cache hit/miss, инвалидация `budgets:all`, деградация при ошибке Redis), валидация `GetReportSummary`;
- `ledger/repository` и `ledger/entity`: репозитории, `LoadBudgets`, валидация;
- `ledger/internal/db`, `ledger/internal/cache`: `DSNFromEnv`, `Redact`, `ConfigFromEnv`, `GetJSON`/`SetJSON`/`Delete` на `miniredis`;
- `ledger/internal/postgres`: интеграционные тесты (upsert, список, лимит и превышение без вставки, категория без бюджета, порядок, отчёт по интервалу); пропускаются без `LEDGER_TEST_DATABASE_URL`;
- `gateway/internal/api`: `POST/GET /api/budgets` и `/api/transactions` (201, 400, 409), `GET /api/reports/summary` (200, 400), `404`/`405` в формате JSON, `GET /ping`,
  middleware `Logging` и `JSONContentType`, DTO-преобразователи.

Достигнутое покрытие:

| Модуль / пакет                    | Покрытие |
|-----------------------------------|----------|
| `ledger` (итог по модулю)         | 53.4%    |
| `ledger` (фасад)                  | 80.8%    |
| `ledger/entity`                   | 100.0%   |
| `ledger/repository`               | 98.3%    |
| `ledger/internal/cache`           | 97.2%    |
| `ledger/internal/db`              | 92.6%    |
| `ledger/internal/postgres`        | 0.0% (82.9% с `LEDGER_TEST_DATABASE_URL`) |
| `ledger/cmd/ledger` (демо-main)   | 0.0%     |
| `gateway` (итог по модулю)        | 74.7%    |
| `gateway/internal/api`            | 88.5%    |
| `gateway` (`main.go`)             | 0.0%     |

Итог по модулю занижен из-за `main`-пакетов без тестов и интеграционных тестов PostgreSQL, которые без базы пропускаются. Фасад не добирает `Init` и `Close`, потому что им нужны живые PostgreSQL и Redis.
