# golang-study

Учебный проект: набор Go-сервисов для учёта личных финансов.

| Сервис    | Описание                                                      |
|-----------|---------------------------------------------------------------|
| `gateway` | HTTP-шлюз (chi): REST API для транзакций и бюджетов поверх `ledger`. |
| `ledger`  | Учёт транзакций и бюджетов по категориям (консольная демо-программа). |

Каждый сервис — отдельный Go-модуль, поэтому все команды ниже выполняются из папки сервиса.

## Требования

- Go 1.25+ (`go version`)

## Gateway

```sh
cd gateway
go run .
```

Сервис слушает порт `8080` и останавливается по `Ctrl+C` (graceful shutdown по SIGINT/SIGTERM). Данные хранятся в памяти
пакета `ledger` (фасад `github.com/AndreyQuantum/golang-study-ms/ledger`, подключён через `replace` в `gateway/go.mod`)
и пропадают при перезапуске. Все ответы — JSON с `Content-Type: application/json; charset=utf-8`; каждый запрос
логируется (метод, путь, статус, длительность).

### Эндпоинты

| Метод  | Путь                 | Описание                          | Успех |
|--------|----------------------|-----------------------------------|-------|
| `GET`  | `/ping`              | Проверка живости: `{"message":"pong"}` | 200 |
| `POST` | `/api/transactions`  | Добавить транзакцию               | 201   |
| `GET`  | `/api/transactions`  | Список транзакций (пустой — `[]`) | 200   |
| `POST` | `/api/budgets`       | Создать или обновить бюджет (upsert) | 201 |
| `GET`  | `/api/budgets`       | Список бюджетов по категориям (пустой — `[]`) | 200 |

Тело транзакции: `{"amount": 450, "category": "еда", "description": "ланч", "date": "2025-09-10"}`, дата в формате `YYYY-MM-DD`.
Тело бюджета: `{"category": "еда", "limit": 5000}`. Неизвестные поля, пустое тело, лишние данные после объекта и тело больше 1 MiB отклоняются.

### Коды ответов и ошибки

| Код | Когда                                                                              |
|-----|------------------------------------------------------------------------------------|
| 201 | транзакция или бюджет сохранены                                                    |
| 400 | некорректный JSON, неверный формат даты, ошибка валидации (`amount must be positive`, ...) |
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
```

## Ledger

### Запуск

```sh
cd ledger
go run ./cmd/ledger
```

> Запускать нужно именно из папки `ledger/`: программа открывает `budgets.json` по относительному пути.
> Иначе она завершится с ошибкой `open budgets file: ... no such file or directory`.

При старте бюджеты загружаются из [`ledger/budgets.json`](ledger/budgets.json) — JSON-массив объектов:

```json
[
  {"category": "food", "limit": 5000},
  {"category": "transport", "limit": 3000}
]
```

Перед сценариями `main` показывает `CheckValid` на валидной и невалидной `Transaction` и на валидном и невалидном `Budget`:

```
== CheckValid: одна функция, разные типы
Transaction (валидная)      -> OK
Transaction (amount = NaN)  -> ошибка: amount must be positive
Budget (валидный)           -> OK
Budget (лимит 0)            -> ошибка: limit must be positive
```

Затем `main` добавляет бюджет `entertainment` через `SetBudget` и прогоняет сценарии:

1. Транзакция в пределах лимита — добавлена.
2. Транзакция сверх лимита — отказ (`budget exceeded`).
3. Лимит повышен через `SetBudget`, та же транзакция — добавлена.
4. Транзакция ровно на лимит — добавлена.
5. Бюджет задан в коде, транзакция сверх лимита — отказ.
6. Категория без бюджета — добавлена без ограничений.
7. Невалидная транзакция (отрицательная сумма) — отказ с ошибкой валидации `amount must be positive`.

В конце выводится список сохранённых транзакций — отклонённых в нём быть не должно:

```
== Сохранённые транзакции (отклонённых здесь быть не должно)
#1 | 2026-10-01 | food          |   450.50 | Продукты в магазине
#2 | 2026-10-02 | food          |  4800.00 | Ресторан
#3 | 2026-10-03 | transport     |  3000.00 | Такси в аэропорт
#4 | 2026-10-04 | health        | 10000.00 | Стоматолог
```

Чтобы проверить обработку ошибок загрузки, можно испортить `budgets.json` (например, указать отрицательный
`limit` или `0`, пустую `category` или опечатку в имени поля) и запустить снова — программа завершится с понятной ошибкой.

### Фасад

Корневой пакет `ledger` (`ledger/ledger.go`) — потокобезопасный фасад над репозиториями с хранилищем в памяти:
`AddTransaction`, `ListTransactions`, `SetBudget`, `ListBudgets`, `CheckValid`, а также алиасы типов и `ErrBudgetExceeded`.
Его импортирует `gateway`. Демо-программа лежит в `ledger/cmd/ledger` и работает через репозитории напрямую.

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

Валидация встроена в репозитории: `AddTransaction` и `SetBudget` сначала вызывают `Validate` и при ошибке ничего не сохраняют,
`LoadBudgets` проверяет каждый элемент (`budget #1: limit must be positive`) и остаётся атомарным. Лимит `0` теперь невалиден.
Превышение лимита сравнивается в центах, поэтому `0.1 + 0.2` укладывается в лимит `0.3`.

### Тесты

```sh
cd ledger
go test ./...        # кратко
go test -v ./...     # с перечнем тестов
```

Тесты покрывают `Validate` для `Transaction` и `Budget`, `CheckValid`, `SetBudget`, `LoadBudgets` (корректный JSON,
ошибки парсинга, валидации и чтения) и `AddTransaction` (в пределах лимита, ровно на лимит, превышение без сохранения,
категория без бюджета, повышение лимита, невалидные транзакции не сохраняются).
