# Gateway

HTTP-шлюз (chi) поверх пакета `ledger`. Эндпоинты те же, что раньше: данные хранятся в PostgreSQL, отчёты кешируются в Redis на 30 секунд. Gateway работает только с интерфейсом `ledger.Service` и ничего не знает про базы данных.

## Запуск

1. Поднять PostgreSQL и Redis: `docker compose up -d db redis` из корня репозитория (или `docker run`, см. [ledger/README.md](../ledger/README.md)).
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
`REDIS_ADDR`, `REDIS_DB`, `REDIS_PASSWORD`; таймаут запроса - `REQUEST_TIMEOUT` (по умолчанию `2s`, см. ниже). Если PostgreSQL или Redis недоступны, сервис завершается с понятной ошибкой.
Запуск в Docker (сервисы `db`, `redis`, `gateway` в compose, образ собирается из корня репозитория): раздел [Docker](../README.md#docker) в корневом README.
Откатить миграции: `goose -dir ./ledger/migrations postgres "$DATABASE_URL" down`.

## Как gateway получает сервис

```go
svc, closeFn, err := ledger.New(ctx)   // подключение к PostgreSQL и Redis, ctx доходит до Ping
if err != nil { log.Fatalf(...) }
defer closeFn()
srv := &http.Server{Handler: api.NewRouter(svc, api.WithTimeout(2*time.Second))}
```

`api.NewRouter(svc, opts...)` создаёт `Handler` с полем `svc ledger.Service`, а пять обработчиков - его методы. В gateway нет импортов `database/sql`, pgx, Redis и
`ledger/internal/...` (последние Go запрещает для другого модуля); это проверяет `gateway/arch_test.go`. Типы `ledger.Transaction`, `ledger.Budget`,
`ledger.ValidationError` и `ledger.ErrBudgetExceeded` - алиасы публичного пакета `ledger`. HTTP-контракт (коды ответов, формат `{"error":"..."}`, DTO) не менялся.

### Контекст и отмена

В каждый вызов сервиса уходит контекст запроса `r.Context()` с дедлайном от middleware `Timeout`. Если клиент отключился или время вышло, работа с БД отменяется
и запрос завершается ответом 504. Раньше записи и списки шли с `context.Background()` и таймаутом 5 секунд, а в HW8 таймаут 5 секунд стоял в каждом handler;
теперь единственный источник дедлайна - middleware. Ошибки сервиса превращает в HTTP-ответ один хелпер `writeServiceError` (`respond.go`):
`ErrBudgetExceeded` - 409, `*ValidationError` - 400, `context.DeadlineExceeded`/`context.Canceled` (и просроченный контекст запроса) - 504, остальное - 500.

## Таймауты запросов

Middleware `Timeout(d)` оборачивает все маршруты, включая `/ping`, и ставит в контекст запроса дедлайн. По умолчанию `d` = 2 секунды (`api.DefaultRequestTimeout`).
Меняется переменной окружения `REQUEST_TIMEOUT` (длительность Go: `50ms`, `2s`, `1m`): `REQUEST_TIMEOUT=50ms go run .`. Пустое значение - 2 секунды;
неверное или `<= 0` - gateway не стартует и пишет причину в лог. При превышении таймаута или отключении клиента любой endpoint отвечает
`504 Gateway Timeout` и `{"error":"request timeout"}`. Handler, который не смотрит в `ctx`, middleware не прервёт. Подробности и пример с `curl` -
в [корневом README](../README.md#таймауты-запросов).

## Эндпоинты

| Метод  | Путь                    | Описание                                         | Успех |
|--------|-------------------------|--------------------------------------------------|-------|
| `GET`  | `/ping`                 | `{"message":"pong"}`                             | 200   |
| `POST` | `/api/transactions`     | Добавить транзакцию                              | 201   |
| `POST` | `/api/transactions/bulk` | Пакетный импорт массива транзакций, `?workers=` | 200   |
| `GET`  | `/api/transactions`     | Список транзакций (дата по убыванию)             | 200   |
| `POST` | `/api/budgets`          | Создать или обновить бюджет                      | 201   |
| `GET`  | `/api/budgets`          | Список бюджетов                                  | 200   |
| `GET`  | `/api/reports/summary`  | Отчёт за интервал: `?from=YYYY-MM-DD&to=YYYY-MM-DD` | 200 |

Ответ отчёта: `{"from":"2026-10-01","to":"2026-10-31","total":450.5,"categories":[{"category":"food","total":450.5}]}`.
Нет параметра, неверный формат даты или `from` позже `to` — 400; время вышло или запрос отменён — 504; ошибки хранилища — 500.
Если отчёта нет в кеше, ledger считает его конкурентно (горутина на категорию, heartbeat в логе), см. [корневой README](../README.md#отчёт-по-категориям).

### Пакетный импорт

`POST /api/transactions/bulk`: тело - JSON-массив объектов того же вида, что у `POST /api/transactions`; разбор строгий (неизвестные поля, мусор после массива,
пустое тело и больше 1 MiB - 400). Не массив, `null` и больше 1000 элементов - 400. `?workers=N`: по умолчанию 4, от 1 до 32, иначе 400
`{"error":"workers must be an integer between 1 and 32"}`.

Ответ 200 (в том числе при частичных отказах): `{"accepted":N,"rejected":M,"errors":[{"index":i,"error":"..."}]}`. `index` - позиция в исходном массиве;
элементы с неверной датой отклоняются в gateway и в ledger не уходят, но индексы остаются исходными. `errors` - всегда массив. Тексты ошибок те же, что у одиночного endpoint;
неожиданная ошибка хранилища по элементу выдаётся как `internal error` (подробности в логе). Ошибки самого вызова идут через общий `writeServiceError` (400, 500).
При таймауте или отключении клиента ответ 504 `{"error":"request timeout","accepted":N,"rejected":M,"errors":[...]}` содержит частичную сводку; для больших пакетов поднимите `REQUEST_TIMEOUT`.
Подробнее - в [корневом README](../README.md#пакетный-импорт).

## Примеры cURL

```sh
curl -X POST http://localhost:8080/api/budgets -H "Content-Type: application/json" -d '{"category":"еда","limit":500}'
curl -X POST http://localhost:8080/api/transactions -H "Content-Type: application/json" -d '{"amount":450,"category":"еда","description":"ланч","date":"2026-10-10"}'

curl "http://localhost:8080/api/reports/summary?from=2026-10-01&to=2026-10-31"
curl "http://localhost:8080/api/reports/summary?from=2026-10-01&to=2026-10-31"

curl -X POST "http://localhost:8080/api/transactions/bulk?workers=8" -H "Content-Type: application/json" \
  -d '[{"amount":10,"category":"food","date":"2026-10-01"},{"amount":-1,"category":"food","date":"2026-10-01"}]'
# 200 {"accepted":1,"rejected":1,"errors":[{"index":1,"error":"amount must be positive"}]}
```

Второй запрос отчёта подряд берётся из Redis: в логе gateway видно `cache miss report:summary:...`, затем `cache hit report:summary:...`. Отчёт кешируется на 30 секунд и не сбрасывается при добавлении транзакций, поэтому может отставать до 30 секунд.

## Тесты

```sh
cd gateway
go test ./... -cover
```

Тесты gateway не требуют PostgreSQL и Redis. Большинство строит роутер на `ledger.NewInMemory()` (сервис на репозиториях в памяти), поэтому каждому тесту хватает
своего экземпляра и глобального сброса нет. Отдельный fake-сервис (`fake_test.go`) проверяет, что ошибки сервиса превращаются в 500, 409, 400 и 504 и что
в сервис уходит контекст запроса с дедлайном не больше 2 секунд для всех пяти эндпоинтов. `middleware_test.go` проверяет `Timeout`: дедлайн в контексте,
504 на каждом endpoint при медленном сервисе, отмену клиентом и отсутствие утечки горутин.

`bulk_test.go` проверяет разбор `workers` и массива, сводку на реальном сервисе в памяти (включая исходные индексы при ошибках конверсии), все варианты 400,
скрытие внутренних ошибок по элементам и 504 с частичной сводкой на fake-сервисе и при настоящем таймауте роутера.
