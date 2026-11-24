# golang-study

Учебный проект: набор Go-сервисов для учёта личных финансов.

| Сервис    | Описание |
|-----------|----------|
| `gateway` | HTTP-шлюз на chi: REST API для транзакций, бюджетов и отчётов. Работает с интерфейсом `ledger.Service` и ничего не знает про PostgreSQL и Redis. |
| `ledger`  | Учёт транзакций и бюджетов по категориям с проверкой лимитов. Слои domain, repository, service и app; данные хранятся в PostgreSQL, отчёты и список бюджетов кешируются в Redis. |

Каждый сервис — отдельный Go-модуль. `gateway` подключает `ledger` через `replace => ../ledger` в `gateway/go.mod`, `go.work` не нужен.
Подробности по сервисам: [ledger/README.md](ledger/README.md), [gateway/README.md](gateway/README.md).

## Структура

```
docker-compose.yml           PostgreSQL 17 и Redis 7
ledger/
  ledger.go                  публичная точка входа: алиасы типов, Service, New, NewInMemory, CheckValid, ErrBudgetExceeded
  internal/domain/           сущности и валидация, Money, ошибки, интерфейсы репозиториев и TxManager, ParseBudgets
  internal/repository/memory/  репозитории в памяти (юнит-тесты, режим без БД)
  internal/repository/pg/    репозитории PostgreSQL: только SQL, транзакция и FOR UPDATE
  internal/repository/repotest/  общий контракт-тест репозиториев для memory и pg
  internal/service/          интерфейс Service, бизнес-логика (правило бюджета, конкурентный отчёт с heartbeat), кеш-декоратор
  internal/app/              фабрика New(ctx) и NewInMemory(): окружение, подключение, сборка
  internal/db/               подключение к PostgreSQL (pgx), DSN из окружения, пул соединений
  internal/cache/            Redis-клиент (go-redis v9), JSON с TTL
  internal/testdb/           хелперы интеграционных тестов (используются только из _test.go)
  migrations/                миграции goose
  cmd/ledger/                демо-программа
gateway/
  main.go                    HTTP-сервер на :8080, graceful shutdown; сервис берёт из ledger.New
  internal/api/              роутер, Handler поверх ledger.Service, DTO, middleware (логирование, Content-Type, таймаут запроса)
```

Зависимости направлены внутрь: `gateway` -> `ledger` (публичный пакет) -> `app` -> `service` -> `domain` <- `repository/*`.
`domain` и `service` не знают про базы данных, `repository/*` не знают про сервис, а про подключение к PostgreSQL и Redis знает только `app`.
Это проверяют arch-тесты (`ledger/arch_test.go`, `gateway/arch_test.go`).

Gateway - отдельный модуль и не может импортировать `ledger/internal/...` (правило Go для каталогов `internal`). Поэтому корневой пакет
`ledger` - тонкая публичная обёртка: он выводит наружу нужные типы алиасами (`ledger.Service = service.Service`,
`ledger.Transaction = domain.Transaction`, ...) и фабрику `ledger.New`. Алиасы дают те же типы, поэтому `errors.Is(err, ledger.ErrBudgetExceeded)`
и `errors.As` работают в gateway как раньше.

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
| `REQUEST_TIMEOUT` | `2s`            | только gateway: таймаут запроса, длительность Go (`50ms`, `2s`, `1m`) |

Ledger и gateway при старте вызывают фабрику `ledger.New(ctx)`: она подключается к PostgreSQL и Redis, пишет в лог `connected to PostgreSQL (...)` и
`connected to Redis (...)` и возвращает сервис и `closeFn` для закрытия соединений. Если подключиться не удалось, процесс завершается с понятной ошибкой.
Миграции фабрика не запускает: схему применяют отдельно через `goose up`.

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
| 504 | время обработки вышло или запрос отменён: `{"error":"request timeout"}` |

Транзакция в категории без бюджета добавляется без ограничений.

Контекст запроса `r.Context()` с дедлайном от middleware `Timeout` (по умолчанию 2 секунды, см. [Таймауты запросов](#таймауты-запросов)) передаётся
во все методы сервиса: если клиент отключился или время вышло, работа с БД отменяется и запрос завершается ответом 504. В HW7 записи и списки шли
с `context.Background()` и таймаутом 5 секунд, а отчёт - с контекстом запроса без таймаута; в HW8 таймаут 5 секунд стоял в каждом handler.
Теперь единственный источник дедлайна - middleware.

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

### Отчёт по категориям

```sh
curl "http://localhost:8080/api/reports/summary?from=2025-09-01&to=2025-09-30"
# 200 {"from":"2025-09-01","to":"2025-09-30","total":450,"categories":[{"category":"еда","total":450}]}
```

Коды: 200 - отчёт; 400 - нет `from`/`to`, неверный формат даты или `from` позже `to`; 504 - время вышло или запрос отменён. Формат ответа и кеш на 30 секунд
остались такими же, как в HW7: сохранён уже существующий контракт (список категорий с суммами и общий `total`).

Если отчёта нет в кеше, он считается конкурентно (HW9):

1. Репозиторий отдаёт список категорий, по которым есть расходы за интервал (`SELECT DISTINCT category ... ORDER BY category`).
2. Для каждой категории запускается своя горутина: она считает сумму за период (`SumByCategoryBetween`). Работа координируется через `sync.WaitGroup`.
   Каждая горутина пишет результат в свой слот заранее выделенного среза, общего `map` и мьютекса нет; после `Wait` результаты сводятся в ответ.
   Одновременно к БД идёт не больше 8 запросов (горутина берёт слот семафора до запроса), чтобы пул соединений не исчерпывался при сотнях категорий.
3. Отмена `ctx` (таймаут или отключение клиента) останавливает воркеров: они проверяют контекст до работы, а запросы к БД идут с `ctx`
   (`QueryContext`/`QueryRowContext`), поэтому pgx при отмене прерывает запрос и на стороне PostgreSQL: закрывает соединение и шлёт серверу запрос отмены (`CancelRequest`).
   Первая ошибка воркера отменяет остальных. Метод возвращает ошибку, оборачивающую причину (`errors.Is(err, context.DeadlineExceeded)` работает),
   а отменённый отчёт в кеш не пишется.
4. Пока идёт расчёт, фоновая горутина на `time.Ticker` раз в 400 мс пишет в лог `report: calculating 2025-09-01..2025-09-30`.
   В конце, при любом исходе, тикер останавливается и пишется `report: heartbeat stopped`; при отмене рядом пишется `report: calculation aborted: context deadline exceeded`.
   Метод дожидается завершения этой горутины до возврата, поэтому ни одна горутина не переживает вызов.

Суммы категорий считаются отдельными запросами, а не одним снимком: расход, добавленный во время расчёта, может попасть в одну категорию и не попасть в другую.
Для отчёта с кешем на 30 секунд это допустимо.

В демо `ledger/cmd/ledger` при `cache miss` отчёт считается этим же кодом, поэтому в stderr после `cache miss ...` появляется строка `report: heartbeat stopped`.

### Таймауты запросов

Middleware `Timeout` оборачивает все маршруты, включая `/ping`: кладёт в контекст запроса `context.WithTimeout(r.Context(), timeout)` (через `r.WithContext`)
и освобождает таймер после ответа. Дедлайн по умолчанию - 2 секунды. Значение меняется переменной окружения `REQUEST_TIMEOUT` (длительность Go):

```sh
cd gateway
REQUEST_TIMEOUT=50ms go run .
```

Пустая переменная означает 2 секунды. Неверное значение (`abc`, `10` без единицы) или `<= 0` - gateway не стартует и пишет причину в лог, ещё до подключения к БД
(выбрано «упасть», чтобы опечатка не давала незаметный другой таймаут). После старта в логе `request timeout: 2s`.

Когда время вышло или клиент отключился, любой endpoint отвечает `504 Gateway Timeout` и `{"error":"request timeout"}`: сервис возвращает `context.DeadlineExceeded`
или `context.Canceled`, а общий хелпер `writeServiceError` превращает их в 504. Ошибки предметной области (409, 400) приоритетнее. Middleware сам ответ не пишет и
handler, который не смотрит в `ctx`, не прервёт: все handler'ы передают контекст в сервис, а сервис и БД его уважают.

```sh
REQUEST_TIMEOUT=1ms go run .
curl -i "http://localhost:8080/api/reports/summary?from=2025-08-01&to=2025-08-31"
# HTTP/1.1 504 Gateway Timeout
# {"error":"request timeout"}
```

В логе при этом: `report: calculation aborted: context deadline exceeded`, `report: heartbeat stopped`, `GET /api/reports/summary 504 ...`.

## Тесты

Тесты написаны на `testing` и `github.com/stretchr/testify`. Юнит-тесты сервиса и тесты HTTP работают на репозиториях в памяти
(`ledger.NewInMemory()`) и не требуют PostgreSQL и Redis; глобального состояния нет, поэтому каждому тесту хватает своего экземпляра сервиса.
В gateway есть и fake-реализация `ledger.Service`: на ней проверяются ошибки сервиса, передача контекста запроса, middleware таймаута и ответ 504.
Тесты отчёта и middleware проверяют отсутствие утечек горутин, а гонки ловит `go test -race` (нужен cgo; на Windows без gcc его удобнее запускать в docker-образе `golang`).

```sh
cd ledger
go test ./... -cover -coverprofile=cover.out
go tool cover -html=cover.out -o cover.html

cd ../gateway
go test ./... -cover -coverprofile=cover.out
go tool cover -html=cover.out -o cover.html
```

`cover.out` и `cover.html` лежат в `.gitignore`.

Интеграционные тесты `ledger/internal/repository/pg` и `ledger/internal/app` пропускаются, пока не задана `LEDGER_TEST_DATABASE_URL`. Перед каждым тестом они очищают
таблицы, поэтому им нужна отдельная база с применёнными миграциями. Пакеты с БД-тестами берут блокировку (`pg_advisory_lock`) на весь свой прогон
(`internal/testdb`), поэтому `go test ./...` не гоняет их параллельно друг с другом:

```sh
docker compose exec postgres createdb -U postgres cashapp_test
goose -dir ./ledger/migrations postgres "postgres://postgres:postgres@localhost:5432/cashapp_test?sslmode=disable" up
cd ledger
LEDGER_TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/cashapp_test?sslmode=disable" go test ./internal/repository/pg ./internal/app -cover
```

Для `app` Redis поднимается внутри теста (`miniredis`), отдельный сервер не нужен. Тест `TestConcurrentAddRespectsLimit` запускает 10 параллельных
транзакций на лимит 100 и проверяет, что принято ровно три и сумма не превысила 90.

Покрытие тестами (с `LEDGER_TEST_DATABASE_URL`):

| Модуль / пакет                         | Покрытие |
|----------------------------------------|----------|
| `ledger`, итог по модулю               | 63.7%    |
| `ledger` (публичный пакет)             | 100.0%   |
| `ledger/internal/domain`               | 97.0%    |
| `ledger/internal/service`              | 97.9%    |
| `ledger/internal/repository/memory`    | 98.9%    |
| `ledger/internal/repository/pg`        | 90.7% с `LEDGER_TEST_DATABASE_URL`, без нее тесты пропускаются |
| `ledger/internal/app`                  | 100.0% с `LEDGER_TEST_DATABASE_URL`, без нее 24.0% |
| `ledger/internal/cache`                | 97.2%    |
| `ledger/internal/db`                   | 97.0%    |
| `ledger/cmd/ledger`                    | 61.4%    |
| `gateway`, итог по модулю              | 82.0%    |
| `gateway/internal/api`                 | 99.3%    |

Итог по модулю ниже, чем по пакетам, потому что в него входят `main`-пакеты (`ledger/cmd/ledger`, `gateway/main.go`), у которых тестами покрыты
только вспомогательные функции, и `internal/testdb` с общим тестовым кодом.

## Домашние задания

| ДЗ | Что сделано |
|----|-------------|
| 2-3 | транзакции и бюджеты в памяти, проверка лимита, загрузка бюджетов из JSON |
| 4  | интерфейс `Validatable`, `Validate()` у `Transaction` и `Budget`, `CheckValid`, валидация перед сохранением |
| 5  | HTTP-слой gateway: DTO, маршруты `/api`, коды 400/409/500, middleware логирования и `Content-Type` |
| 6  | unit-тесты домена и тесты HTTP через `httptest`, `ledger.Reset()`, покрытие |
| 7  | PostgreSQL и миграции goose, кеш Redis для отчёта и списка бюджетов, эндпоинт `/api/reports/summary` |
| 8  | чистая архитектура: domain, repository, service, app; интерфейсы репозиториев и `ledger.Service`, фабрика `ledger.New`, gateway работает через интерфейс сервиса |
| 9  | конкурентный отчёт по категориям (горутины, `sync.WaitGroup`, `ctx`), heartbeat на `time.Ticker`, middleware таймаута (504), `REQUEST_TIMEOUT` |
