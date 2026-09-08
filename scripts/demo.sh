#!/usr/bin/env bash
#
# Прогоняет все бизнес-сценарии Авито.Кухни по живому API.
# Требует поднятого стека: make up
#
set -uo pipefail

API=${API_URL:-http://localhost:8080}
CONTROL=${CONTROL_URL:-http://localhost:8081}

PIZZERIA=11111111-1111-1111-1111-111111111111
MARGHERITA=31111111-1111-1111-1111-111111111111
COLA=31111111-1111-1111-1111-444444444444
USER_IVAN=41111111-1111-1111-1111-111111111111

step()  { printf '\n\033[1m== %s\033[0m\n' "$1"; }
say()   { printf '   %s\n' "$1"; }
show()  { printf '   \033[2m%s\033[0m\n' "$1"; }

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "нужна утилита $1"; exit 1; }
}
need curl
need jq

# client METHOD PATH [BODY] [EXTRA_HEADERS...]
client() {
  local method=$1 path=$2 body=${3:-}; shift 3 2>/dev/null || shift 2
  if [ -n "$body" ]; then
    curl -s -o /tmp/demo.json -w '%{http_code}' -X "$method" "$API$path" \
      -H 'Content-Type: application/json' "$@" -d "$body"
  else
    curl -s -o /tmp/demo.json -w '%{http_code}' -X "$method" "$API$path" "$@"
  fi
}

control() {
  curl -s -X POST "$CONTROL$1" -H 'Content-Type: application/json' -d "${2:-\{\}}" >/dev/null
}

order_body() {
  cat <<JSON
{"restaurant_id":"$PIZZERIA",
 "items":[{"menu_item_id":"$MARGHERITA","quantity":2},{"menu_item_id":"$COLA","quantity":1}],
 "delivery_address":"Москва, ул. Ленина, 5","customer_phone":"+79001112233"}
JSON
}

place_order() { # $1 — ключ идемпотентности
  client POST /orders "$(order_body)" \
    -H "X-User-Id: $USER_IVAN" -H "X-Idempotency-Key: $1" >/dev/null
  jq -r '.id' /tmp/demo.json
}

status_of() {
  curl -s "$API/orders/$1" -H "X-User-Id: $USER_IVAN" | jq -r '.status'
}

printf '\033[1mАвито.Кухня — демонстрация бизнес-сценариев\033[0m\n'
say "платформа: $API, управление заведением: $CONTROL"

for _ in $(seq 1 30); do
  curl -sf "$API/healthz" -o /dev/null && break
  sleep 2
done || true

# ---------------------------------------------------------------- поиск

step "1. Пользователь ищет, где заказать"
client GET "/restaurants?city=Москва&open_only=true" >/dev/null
show "$(jq -r '.items[] | "\(.name) — \(.cuisine_type), принимает заказы: \(.is_accepting_orders)"' /tmp/demo.json)"

say ""
say "Ищет конкретное блюдо по всем заведениям сразу:"
client GET "/menu/search?query=Пицца" >/dev/null
show "$(jq -r '.items[] | "\(.item.name) — \(.item.price_cents/100) руб. в «\(.restaurant_name)»"' /tmp/demo.json)"

step "2. Открывает меню заведения"
client GET "/restaurants/$PIZZERIA/menu" >/dev/null
show "$(jq -r '.categories[] | "\(.name): \([.items[].name] | join(", "))"' /tmp/demo.json)"

# ---------------------------------------------------------------- корзина

step "3. Проверяет корзину до оформления"
CART="{\"restaurant_id\":\"$PIZZERIA\",\"items\":[{\"menu_item_id\":\"$MARGHERITA\",\"quantity\":2,\"expected_price_cents\":45000}]}"
client POST /cart/validate "$CART" >/dev/null
show "$(jq -r '"корзина валидна: \(.valid), сумма: \(.total_price_cents/100) руб."' /tmp/demo.json)"

say ""
say "Та же корзина, но клиент видел старую цену — 100 руб. вместо 450:"
STALE="{\"restaurant_id\":\"$PIZZERIA\",\"items\":[{\"menu_item_id\":\"$MARGHERITA\",\"quantity\":2,\"expected_price_cents\":10000}]}"
client POST /cart/validate "$STALE" >/dev/null
show "$(jq -r '.problems[] | "\(.name): \(.reason), было \(.expected_price_cents/100), стало \(.actual_price_cents/100)"' /tmp/demo.json)"

# ------------------------------------------------------------- стоп-лист

step "4. Заведение отправляет позицию в стоп-лист"
control /control/stop-list "{\"external_id\":\"pizza-margherita\",\"is_available\":false}"
say "Пользователь пытается оформить заказ с этой позицией:"
code=$(client POST /orders "$(order_body)" -H "X-User-Id: $USER_IVAN" -H "X-Idempotency-Key: demo-stop-$RANDOM")
show "HTTP $code"
show "$(jq -r '"\(.code): \(.problems[0].name) — \(.problems[0].reason)"' /tmp/demo.json)"
control /control/stop-list "{\"external_id\":\"pizza-margherita\",\"is_available\":true}"
say "Позиция возвращена в продажу."

# ---------------------------------------------------------- идемпотентность

step "5. Идемпотентность оформления"
control /control/pause
say "Кухня поставлена на паузу, чтобы заказ не забрали во время демонстрации."
KEY="demo-idem-$(date +%s%N)"
FIRST=$(place_order "$KEY")
SECOND=$(place_order "$KEY")
show "первый запрос:  $FIRST"
show "повтор с тем же X-Idempotency-Key: $SECOND"
if [ "$FIRST" = "$SECOND" ]; then
  say "Заказ один. Двойное нажатие кнопки или ретрай сети не создают дубль."
else
  say "ОШИБКА: создались разные заказы"
fi

# ------------------------------------------------------------------ отмена

step "6. Отмена заказа до и после принятия"
say "Заказ $FIRST в статусе $(status_of "$FIRST"), заведение его ещё не видело."
code=$(client POST "/orders/$FIRST/cancel" "" -H "X-User-Id: $USER_IVAN")
show "отмена до принятия: HTTP $code, статус $(jq -r '.status' /tmp/demo.json)"

CANCEL_LATE=$(place_order "demo-late-$(date +%s%N)")
control /control/resume
say ""
say "Кухня снята с паузы, ждём, пока заведение примет заказ..."
for _ in $(seq 1 15); do
  [ "$(status_of "$CANCEL_LATE")" != "created" ] && break
  sleep 2
done
say "Заказ $CANCEL_LATE теперь в статусе $(status_of "$CANCEL_LATE"). Пользователь пытается отменить:"
code=$(client POST "/orders/$CANCEL_LATE/cancel" "" -H "X-User-Id: $USER_IVAN")
show "HTTP $code — $(jq -r '.message' /tmp/demo.json)"

# ------------------------------------------------------------ полный цикл

step "7. Полный жизненный цикл заказа"
say "Заведение ведёт заказ по статусам само, опрашивая платформу."
LAST=""
for _ in $(seq 1 40); do
  s=$(status_of "$CANCEL_LATE")
  if [ "$s" != "$LAST" ]; then show "$s"; LAST=$s; fi
  [ "$s" = "delivered" ] && break
  sleep 2
done

# --------------------------------------------------------------- отказ

step "8. Заведение отклоняет заказ"
control /control/reject-next '{"reason":"закончилось тесто"}'
REJECTED=$(place_order "demo-reject-$(date +%s%N)")
for _ in $(seq 1 15); do
  [ "$(status_of "$REJECTED")" = "rejected" ] && break
  sleep 2
done
client GET "/orders/$REJECTED" "" -H "X-User-Id: $USER_IVAN" >/dev/null
show "$(jq -r '"статус \(.status), причина: \(.rejection_reason)"' /tmp/demo.json)"

# ------------------------------------------------------- закрытое заведение

step "9. Заведение закрывает приём заказов"
control /control/accepting-orders '{"is_accepting_orders":false}'
code=$(client POST /orders "$(order_body)" -H "X-User-Id: $USER_IVAN" -H "X-Idempotency-Key: demo-closed-$RANDOM")
show "HTTP $code — $(jq -r '"\(.code): \(.message)"' /tmp/demo.json)"
control /control/accepting-orders '{"is_accepting_orders":true}'
say "Приём заказов возвращён."

step "10. История заказов пользователя"
client GET "/orders?limit=5" "" -H "X-User-Id: $USER_IVAN" >/dev/null
show "$(jq -r '.items[] | "\(.status): \(.total_price_cents/100) руб., позиций \(.items|length)"' /tmp/demo.json)"

printf '\n\033[1mДемонстрация завершена.\033[0m\n'
say "Спецификации API: http://localhost:8082"
