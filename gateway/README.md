# Gateway

HTTP-шлюз (chi) поверх пакета `ledger`. Эндпоинты те же, что раньше: данные хранятся в PostgreSQL, отчёты кешируются в Redis на 30 секунд. Gateway работает только с интерфейсом `ledger.Service` и ничего не знает про базы данных.

## Запуск

1. Поднять PostgreSQL и Redis: `docker compose up -d` из корня репозитория (или `docker run`, см. [ledger/README.md](../ledger/README.md)).
2. Установить goose и применить миграции из корня репозитория:

```sh
go install github.com/pressly/goose/v3/cmd/goose@latest
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/cashapp?sslmode=disable"
goose -dir ./ledger/migrations postgres "$DATABASE_URL" up
```

3. Запустить gateway (ledger-демо при желании запускается до него, см. [ledger/README.md](../ledger/README.md)):

```sh
cd gateway
go run .
```

Подключение настраивается переменными `DATABASE_URL` (или `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASS`, `DB_NAME`) и
`REDIS_ADDR`, `REDIS_DB`, `REDIS_PASSWORD`. Если PostgreSQL или Redis недоступны, сервис завершается с понятной ошибкой.
Откатить миграции: `goose -dir ./ledger/migrations postgres "$DATABASE_URL" down`.

## Как gateway получает сервис

```go
svc, closeFn, err := ledger.New(ctx)   // подключение к PostgreSQL и Redis, ctx доходит до Ping
if err != nil { log.Fatalf(...) }
defer closeFn()
srv := &http.Server{Handler: api.NewRouter(svc)}
```

`api.NewRouter(svc)` создаёт `Handler` с полем `svc ledger.Service`, а пять обработчиков - его методы. В gateway нет импортов `database/sql`, pgx, Redis и
`ledger/internal/...` (последние Go запрещает для другого модуля); это проверяет `gateway/arch_test.go`. Типы `ledger.Transaction`, `ledger.Budget`,
`ledger.ValidationError` и `ledger.ErrBudgetExceeded` - алиасы публичного пакета `ledger`. HTTP-контракт (коды ответов, формат `{"error":"..."}`, DTO) не менялся.

### Контекст и отмена

В каждый вызов сервиса уходит контекст запроса `r.Context()` с таймаутом 5 секунд. Если клиент отключился, работа с БД отменяется и запрос завершается ответом 500.
Раньше записи и списки шли с `context.Background()` и таймаутом 5 секунд, а отчёт - с контекстом запроса без таймаута. Теперь семантика единая; это нужно для HW9.

## Эндпоинты

| Метод  | Путь                    | Описание                                         | Успех |
|--------|-------------------------|--------------------------------------------------|-------|
| `GET`  | `/ping`                 | `{"message":"pong"}`                             | 200   |
| `POST` | `/api/transactions`     | Добавить транзакцию                              | 201   |
| `GET`  | `/api/transactions`     | Список транзакций (дата по убыванию)             | 200   |
| `POST` | `/api/budgets`          | Создать или обновить бюджет                      | 201   |
| `GET`  | `/api/budgets`          | Список бюджетов                                  | 200   |
| `GET`  | `/api/reports/summary`  | Отчёт за интервал: `?from=YYYY-MM-DD&to=YYYY-MM-DD` | 200 |

Ответ отчёта: `{"from":"2026-10-01","to":"2026-10-31","total":450.5,"categories":[{"category":"food","total":450.5}]}`.
Нет параметра, неверный формат даты или `from` позже `to` — 400; ошибки хранилища — 500.

## Примеры cURL

```sh
curl -X POST http://localhost:8080/api/budgets -H "Content-Type: application/json" -d '{"category":"еда","limit":500}'
curl -X POST http://localhost:8080/api/transactions -H "Content-Type: application/json" -d '{"amount":450,"category":"еда","description":"ланч","date":"2026-10-10"}'

curl "http://localhost:8080/api/reports/summary?from=2026-10-01&to=2026-10-31"
curl "http://localhost:8080/api/reports/summary?from=2026-10-01&to=2026-10-31"
```

Второй запрос отчёта подряд берётся из Redis: в логе gateway видно `cache miss report:summary:...`, затем `cache hit report:summary:...`. Отчёт кешируется на 30 секунд и не сбрасывается при добавлении транзакций, поэтому может отставать до 30 секунд.

## Тесты

```sh
cd gateway
go test ./... -cover
```

Тесты gateway не требуют PostgreSQL и Redis. Большинство строит роутер на `ledger.NewInMemory()` (сервис на репозиториях в памяти), поэтому каждому тесту хватает
своего экземпляра и глобального сброса нет. Отдельный fake-сервис (`fake_test.go`) проверяет, что ошибки сервиса превращаются в 500, 409 и 400 и что
в сервис уходит контекст запроса с дедлайном не больше 5 секунд для всех пяти эндпоинтов.
