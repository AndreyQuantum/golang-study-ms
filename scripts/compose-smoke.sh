#!/usr/bin/env bash
# Проверка стенда docker compose (HW11): образы, db + redis + gateway, healthcheck, эндпоинты, graceful shutdown.
# Поднимает отдельный проект hw11smoke на нестандартных портах и сносит только его.
# KEEP=1 - не сносить стенд в конце. Код выхода: 0 все проверки прошли, 1 есть провалы, 2 не выполнены предусловия.
set -u

cd "$(dirname "$0")/.." || exit 2

P=hw11smoke
export DB_PORT=15432 REDIS_PORT=16379 GATEWAY_PORT=18080
DC="docker compose -p $P"
DSN="postgres://postgres:postgres@localhost:${DB_PORT}/cashapp?sslmode=disable"
BASE="http://localhost:${GATEWAY_PORT}"
S=0
STARTED=0
TMPBODY=$(mktemp)

ok()   { echo "ok    $1"; }
fail() { echo "FAIL  $1"; S=1; }

cleanup() {
  if [ "$STARTED" = 1 ] && [ "${KEEP:-0}" != 1 ]; then
    $DC down -v --remove-orphans >/dev/null 2>&1
  fi
}
trap 'cleanup; rm -f "$TMPBODY"' EXIT

# check "описание" "подстрока" "фактический вывод"
contains() {
  case "$3" in *"$2"*) ok "$1" ;; *) fail "$1 (ожидалось '$2', получено '$3')" ;; esac
}

# http "описание" код метод путь [тело]
http() {
  local desc=$1 want=$2 method=$3 path=$4 body=${5:-} out code
  if [ -n "$body" ]; then
    out=$(curl -s -o "$TMPBODY" -w '%{http_code}' -X "$method" "$BASE$path" -H 'Content-Type: application/json' -d "$body")
  else
    out=$(curl -s -o "$TMPBODY" -w '%{http_code}' -X "$method" "$BASE$path")
  fi
  code=$out
  BODY=$(cat "$TMPBODY" 2>/dev/null)
  if [ "$code" = "$want" ]; then ok "$desc: $code"; else fail "$desc: ожидался $want, получен $code, тело: $BODY"; fi
}

wait_healthy() { # сервис, секунд
  local svc=$1 limit=$2 i st
  for ((i = 0; i < limit; i++)); do
    st=$(docker inspect -f '{{.State.Health.Status}}' "$($DC ps -q "$svc")" 2>/dev/null)
    [ "$st" = healthy ] && { ok "$svc healthy"; return 0; }
    sleep 1
  done
  fail "$svc не стал healthy за ${limit}с (статус: $st)"
  return 1
}

echo "== 0. сборка образов из корня репозитория"
if docker build -q -f gateway/Dockerfile -t hw11-gateway:smoke . >/dev/null; then ok "образ gateway"; else fail "сборка gateway/Dockerfile"; fi
if docker build -q -f ledger/Dockerfile -t hw11-ledger:smoke . >/dev/null; then ok "образ ledger"; else fail "сборка ledger/Dockerfile"; fi
[ "$S" = 0 ] || exit 1

echo "== 1. предусловия"
for tool in docker goose curl; do
  command -v "$tool" >/dev/null || { echo "нет утилиты $tool"; exit 2; }
done
for port in "$DB_PORT" "$REDIS_PORT" "$GATEWAY_PORT"; do
  if curl -s -m 1 -o /dev/null "http://localhost:$port" ; then echo "порт $port занят"; exit 2; fi
done

echo "== 2. db и redis"
STARTED=1
$DC up -d db redis || { fail "docker compose up -d db redis"; exit 1; }
wait_healthy db 60 || exit 1
wait_healthy redis 60 || exit 1

echo "== 3. миграции"
if goose -dir ledger/migrations postgres "$DSN" up >/dev/null; then ok "goose up"; else fail "goose up"; exit 1; fi

echo "== 4. gateway"
$DC up -d --build gateway || { fail "docker compose up -d --build gateway"; exit 1; }
wait_healthy gateway 90 || { $DC logs gateway | tail -20; exit 1; }

echo "== 5. эндпоинты снаружи"
http "GET /ping" 200 GET /ping
contains "ping содержит pong" pong "$BODY"
http "POST /api/budgets" 201 POST /api/budgets '{"category":"smoke","limit":500}'
http "GET /api/budgets" 200 GET /api/budgets
contains "бюджеты содержат категорию" smoke "$BODY"
http "POST /api/transactions" 201 POST /api/transactions '{"amount":450,"category":"smoke","description":"lunch","date":"2025-09-10"}'
http "GET /api/transactions" 200 GET /api/transactions
contains "транзакции содержат lunch" "lunch" "$BODY"
http "POST сверх лимита" 409 POST /api/transactions '{"amount":450,"category":"smoke","description":"dinner","date":"2025-09-11"}'
http "GET /api/reports/summary" 200 GET "/api/reports/summary?from=2025-09-01&to=2025-09-30"
contains "total равен 450" '"total":450' "$BODY"
http "POST /api/transactions/bulk" 200 POST /api/transactions/bulk \
  '[{"amount":5,"category":"nolimit","date":"2025-09-12"},{"amount":5,"category":"nolimit","date":"12.09.2025"}]'
contains "bulk: принят один" '"accepted":1' "$BODY"
contains "bulk: отклонён один" '"rejected":1' "$BODY"

echo "== 6. внутри контейнера"
inside=$($DC exec -T gateway wget -qO- http://localhost:8080/ping 2>&1)
contains "wget внутри контейнера" pong "$inside"

echo "== 7. данные в БД"
cnt=$($DC exec -T db psql -U postgres -d cashapp -tAc "select count(*) from expenses" 2>&1 | tr -d '[:space:]')
if [ "${cnt:-0}" -gt 0 ] 2>/dev/null; then ok "в expenses строк: $cnt"; else fail "expenses пуста или недоступна: $cnt"; fi

echo "== 8. перезапуск gateway"
$DC restart gateway >/dev/null 2>&1
wait_healthy gateway 90
http "GET /api/transactions после рестарта" 200 GET /api/transactions
contains "транзакция сохранилась" "lunch" "$BODY"

echo "== 9. graceful shutdown"
t0=$SECONDS
$DC stop gateway >/dev/null 2>&1
dt=$((SECONDS - t0))
if [ "$dt" -lt 10 ]; then ok "stop за ${dt}с"; else fail "stop занял ${dt}с (SIGTERM не дошёл до процесса?)"; fi
contains "лог gateway stopped" "gateway stopped" "$($DC logs gateway 2>&1)"

echo
if [ "$S" = 0 ]; then echo "RESULT compose-smoke PASS"; else echo "RESULT compose-smoke FAIL"; fi
exit $S
