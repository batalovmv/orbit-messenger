# MST Integrations Guide

Гид для админов Orbit по подключению внешних систем MST через integrations-сервис. Закрывает Phase 8C из [PHASES.md](../PHASES.md).

## Архитектура в одном абзаце

Integrations-сервис ([services/integrations](../services/integrations)) принимает вебхуки от внешних систем (InsightFlow, Keitaro, Saturn.ac, ASA Analytics, generic), проверяет HMAC-подпись и timestamp, находит подходящие routes (куда доставлять) и публикует сообщение в нужный чат через messaging-сервис. Шаблон сообщения задаётся в routes как Go text/template с dot-notation по JSON payload (например `{{.offer_name}}`).

Пять предустановленных **пресетов** в [web/src/api/saturn/presets/integrations.ts](../web/src/api/saturn/presets/integrations.ts) избавляют админа от ручного вывода формата под каждую систему — достаточно выбрать тип в Settings → Integrations и форма автозаполняется.

## Как работает подпись

Orbit хранит у каждого коннектора секрет (при создании возвращается один раз, потом только ротация через "Rotate secret"). Провайдер считает подпись:

```
HMAC-SHA256(secret, timestamp + "." + payload)
```

где `payload` — это:
- **POST-коннекторы**: байт-в-байт содержимое JSON body
- **GET-коннекторы (Keitaro)**: канонический JSON-объект, построенный из query params (без `sign` и `ts`), с ключами **отсортированными по алфавиту**

`timestamp` может быть UNIX-секундами (целое число) или RFC3339. Окно валидности ±5 минут от текущего времени на сервере (защита от replay).

Где подпись и timestamp передаются — зависит от пресета. Два режима:

| Режим | Пресеты | Signature | Timestamp |
|-------|---------|-----------|-----------|
| **header** | Saturn.ac / InsightFlow / ASA / Generic | в HTTP header | в HTTP header |
| **query** | Keitaro | в query param `?sign=...` | в query param `?ts=...` |

Имена полей переопределяются через `connector.config.signature_param_name` / `timestamp_param_name`.

---

## Saturn.ac — Deploy status (статус: ready)

Единственный пресет где мы владеем обеими сторонами — это наш собственный PaaS.

### Поля пресета
- `preset_id`: `saturn_deploy`
- `http_method`: `POST`
- `signature_location`: `header`
- `signature_param_name`: `X-Orbit-Signature`
- `timestamp_param_name`: `X-Orbit-Timestamp`

### Дефолтный template
```
🚀 Deploy {{.service}} → {{.status}} ({{.commit_sha}}) by {{.user}}
```

### Event filter
```
deploy.started,deploy.succeeded,deploy.failed
```

### Пошаговая настройка
1. Settings → Integrations → **Create connector**
2. В выпадашке **Тип интеграции** → "Saturn.ac — Deploy status"
3. Имя: `saturn-deploy-dev` (или любой уникальный slug)
4. Display name пре-заполнится
5. **Create** → скопировать secret (показывается один раз!)
6. В Saturn.ac dashboard → Webhooks → Add webhook
7. URL: `https://orbit.mst/webhooks/in/{connectorId}` (из Orbit UI)
8. HMAC secret: вставить значение из шага 5
9. Subscribe events: deploy.started, deploy.succeeded, deploy.failed
10. В Orbit: создать **route** этого коннектора на канал `#dev` — template пре-заполнится, можно редактировать
11. Сделать тестовый deploy → сообщение должно прийти в `#dev`

### Пример payload (референс)
```json
{
  "event": "deploy.succeeded",
  "service": "gateway",
  "status": "succeeded",
  "commit_sha": "930da3a",
  "user": "batalovmv",
  "started_at": "2026-04-15T10:22:00Z",
  "finished_at": "2026-04-15T10:24:12Z",
  "external_event_id": "deploy-8234"
}
```

---

## InsightFlow — Conversions (статус: framework-only)

### Поля пресета
- `preset_id`: `insightflow`
- `http_method`: `POST`
- `signature_location`: `header`
- `signature_param_name`: `X-InsightFlow-Signature`
- `timestamp_param_name`: `X-InsightFlow-Timestamp`

### Дефолтный template (требует сверки)
```
💰 Конверсия: {{.offer_name}} — {{.amount}} {{.currency}}
```

### Event filter
```
conversion.lead,conversion.sale
```

### Пошаговая настройка
1. Settings → Integrations → **Create connector**
2. Тип → "InsightFlow — Conversions"
3. Имя: `insightflow-alerts`
4. **Create** → скопировать secret
5. В InsightFlow dashboard → Settings → Webhooks → New webhook
6. URL: `https://orbit.mst/webhooks/in/{connectorId}`
7. HMAC secret: из шага 4
8. **Важно:** нужно проверить формат payload — см. раздел "После получения реальных кредов" ниже
9. В Orbit: создать route на `#alerts`, при необходимости поправить template под реальные поля

### Что нужно сверить вживую
- Точное имя header'а подписи (возможно `X-Signature` или `X-Webhook-Sign` — InsightFlow документация)
- Формулу HMAC (мы предполагаем `HMAC-SHA256(timestamp + "." + body)` — это Orbit-native конвенция, InsightFlow может использовать просто `HMAC-SHA256(body)`)
- Структуру JSON (поля `offer_name`, `amount`, `currency` — предположения)
- Имена event type'ов (поле `event` в JSON) — шаблон фильтра `conversion.lead,conversion.sale` может не совпадать

Если Orbit-native HMAC формула не подходит — скажи, добавлю поддержку `HMAC-SHA256(body)` без timestamp через дополнительный флажок `signature_algorithm: "body-only"` в `connector.config`.

---

## ASA Analytics — Campaign alerts (статус: framework-only)

### Поля пресета
- `preset_id`: `asa_analytics`
- `http_method`: `POST`
- `signature_location`: `header`
- `signature_param_name`: `X-ASA-Signature`
- `timestamp_param_name`: `X-ASA-Timestamp`

### Дефолтный template
```
📊 Кампания {{.campaign_name}}: CPI ${{.cpi}}, {{.installs}} установок, ${{.spend}} потрачено
```

### Event filter
```
campaign.alert,campaign.limit_reached
```

### Пошаговая настройка
Аналогично InsightFlow:
1. Create connector → тип ASA Analytics → скопировать secret
2. В Apple Search Ads → Campaign settings → Webhooks → New
3. URL + HMAC secret
4. Route на `#marketing`

### Что нужно сверить вживую
- Точное имя header'а подписи
- Формулу HMAC (Apple часто использует собственные схемы)
- Поля payload (`campaign_name`, `cpi`, `installs`, `spend` — предположения)
- Корректные event type значения в поле `event`

---

## Keitaro — Postbacks (статус: framework-only)

**Сложность:** Keitaro работает через GET postbacks с параметрами в query string, не POST JSON body. Integrations-сервис специально расширен под этот кейс.

### Поля пресета
- `preset_id`: `keitaro`
- `http_method`: `GET`
- `signature_location`: `query`
- `signature_param_name`: `sign`
- `timestamp_param_name`: `ts`

### Дефолтный template
```
🎯 Postback: {{.campaign}} — {{.status}} ({{.payout}}$)
```

### Event filter
```
postback.approved,postback.rejected
```

### Пошаговая настройка
1. Create connector → тип "Keitaro — Postbacks" → скопировать secret
2. В Keitaro → Tracker → Postback URLs → New postback
3. URL format:
   ```
   https://orbit.mst/webhooks/in/{connectorId}?event=postback.{status}&campaign={campaign_name}&status={status}&payout={payout}&ts={unix}&sign={hmac}
   ```
4. Где `{hmac}` рассчитывается Keitaro как:
   ```
   HMAC-SHA256(secret, unix_ts + "." + canonical_json_of_params)
   ```
   — где `canonical_json` это JSON-объект из всех params кроме `sign`/`ts`, с ключами отсортированными по алфавиту.

### Как Keitaro должен считать подпись
Пример в псевдо-скрипте который нужно настроить в Keitaro postback settings:
```bash
TS=$(date +%s)
PAYLOAD='{"campaign":"$campaign","event":"postback.$status","payout":"$payout","status":"$status"}'
SIGN=$(echo -n "${TS}.${PAYLOAD}" | openssl dgst -sha256 -hmac "$SECRET" | cut -d' ' -f2)
curl "https://orbit.mst/webhooks/in/{connectorId}?campaign=$campaign&event=postback.$status&payout=$payout&status=$status&ts=$TS&sign=$SIGN"
```

**Важно:** ключи в `PAYLOAD` должны быть отсортированы алфавитно — именно так Orbit канонизирует query params перед верификацией. Без этого HMAC не совпадёт.

### Что нужно сверить вживую
- Поддерживает ли Keitaro кастомные подпись-функции в postback settings (если нет — нужен промежуточный скрипт)
- Точный набор доступных placeholder-токенов в Keitaro
- Нужно ли нам добавить поддержку подписи в стиле `HMAC-SHA256(raw_query_string)` вместо canonical JSON (Keitaro может быть заточен под такой формат)

---

## Generic webhook (статус: ready)

Запасной пресет для любой системы не из списка. Полностью ручная настройка под Orbit-native HMAC конвенцию.

### Поля пресета
- `http_method`: `POST`
- `signature_location`: `header`
- `signature_param_name`: `X-Orbit-Signature`
- `timestamp_param_name`: `X-Orbit-Timestamp`

### Как подписать
```bash
TS=$(date +%s)
BODY='{"event":"my.event","field":"value"}'
SIGN=$(echo -n "${TS}.${BODY}" | openssl dgst -sha256 -hmac "$SECRET" | cut -d' ' -f2)
curl -X POST https://orbit.mst/webhooks/in/{connectorId} \
  -H "Content-Type: application/json" \
  -H "X-Orbit-Signature: $SIGN" \
  -H "X-Orbit-Timestamp: $TS" \
  -d "$BODY"
```

---

## После получения реальных кредов

Когда MST передаст настоящие API-ключи и sample payload'ы для InsightFlow / Keitaro / ASA Analytics:

1. **Создать тестовый коннектор** под каждую систему в dev-окружении Orbit
2. **Прислать sample payload** от провайдера (один event через их test-кнопку) — посмотреть в Settings → Integrations → Deliveries лог
3. **Сверить три вещи** для каждого провайдера:
   - Действительно ли header называется `X-InsightFlow-Signature` (и аналогично для ASA) — если нет, исправить preset
   - Действительно ли HMAC считается как `HMAC-SHA256(timestamp + "." + body)` — если провайдер использует другую формулу, расширить `connector.config` новым полем `signature_algorithm`
   - Совпадают ли field names с шаблоном — скорректировать `defaultTemplate` в [web/src/api/saturn/presets/integrations.ts](../web/src/api/saturn/presets/integrations.ts)
4. **Обновить статус пресета** `framework-only` → `ready` в том же файле
5. **Поправить документ выше** — заменить раздел "Что нужно сверить вживую" на подтверждённые значения
6. **Live smoke test** — сделать реальный event у провайдера → проверить доставку в Orbit

Оценка работ на одного провайдера: 1-2 часа (если формат близок к Orbit-native), до полдня если формула HMAC или payload shape сильно отличается.

---

## Ограничения и что НЕ поддерживается

- **Outbound webhooks** (Orbit шлёт наружу при событиях в Orbit) — пока не реализовано, нужно отдельно
- **HR-бот миграция** — deferred, у MST нет исходной системы для переноса
- **Retry с dead letter queue после N фейлов** — retry есть (exponential backoff, 3 attempts), но DLQ пока в логах, нет отдельного UI
- **Dynamic HMAC algorithms** — только `HMAC-SHA256(ts + "." + body)`. Если провайдер требует что-то другое (например `HMAC-SHA256(body)` без timestamp, или SHA-1) — нужно расширить `ConnectorConfig.SignatureAlgorithm`
- **Rate limit на провайдера** — 60 req/min per connector. Если нужно больше — увеличить `webhookRateLimitPerMinute` в [services/integrations/internal/handler/webhook_handler.go](../services/integrations/internal/handler/webhook_handler.go)

## Справка

- Webhook handler: [services/integrations/internal/handler/webhook_handler.go](../services/integrations/internal/handler/webhook_handler.go)
- Signature verification: [services/integrations/internal/service/integration_service.go:319](../services/integrations/internal/service/integration_service.go#L319)
- Frontend admin UI: [web/src/components/left/settings/SettingsIntegrations.tsx](../web/src/components/left/settings/SettingsIntegrations.tsx)
- Preset definitions: [web/src/api/saturn/presets/integrations.ts](../web/src/api/saturn/presets/integrations.ts)
- Connector config typing (backend): `ConnectorConfig` в [services/integrations/internal/model/models.go](../services/integrations/internal/model/models.go)
- Regression tests: [services/integrations/internal/handler/connector_handler_test.go](../services/integrations/internal/handler/connector_handler_test.go) — `TestReceiveWebhook_CustomHeaderName`, `TestReceiveWebhook_QueryKeitaro`, `TestReceiveWebhook_MethodMismatch`
