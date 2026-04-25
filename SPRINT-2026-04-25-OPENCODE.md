# OpenCode Autonomous Sprint — 2026-04-25

> Этот файл — самодостаточное задание для автономной работы OpenCode. Никакой внешней информации не нужно: вся локальная конвенция, контекст и ссылки внутри. Работай по списку, делай self-critique после каждой задачи, коммить и пушь маленькими порциями.

---

## 0. Общие правила

- **Язык**: общение в комментариях/коммитах — английский, документация — русский где принято.
- **Коммиты**: conventional commits, маленькие (одна задача = один коммит). После каждого commit делай `git push origin master` сразу.
- **Тесты**: для нового Go-кода обязателен handler-тест. Для UI-кода — нет (репо запрещает).
- **Линт**: после правки фронта запускай `cd web && npm run check` (tsc + stylelint + eslint). Baseline TypeScript ошибок — 32, не превышай.
- **Сборка фронта**: `docker compose build web` должна пройти зелёной перед коммитом фронт-задач.
- **CLAUDE.md** в корне репо и в `web/CLAUDE.md` — изучи перед началом, там конвенции Teact, RBAC, миграции, сервисная структура.
- **Фиксированные модели LLM**: gpt = `gpt-5.4`, gemini = `gemini-3.1-pro-preview`. Не выдумывай суффиксов.
- **Если задача блокирована или есть архитектурный выбор** — НЕ догадывайся. Спроси у `mcp__gpt__gpt_call` (gpt-5.4) или `mcp__gemini__gemini_call` (gemini-3.1-pro-preview), приложив релевантные файлы через `files`. Это бесплатно по флэт-рейту.
- **Боты** — идёт расширение существующего сервиса `services/bots/`. НЕ создавай новый сервис.
- **Не трогай**: payments/stars/gifts/stories/channels — они удалены, любой их код = регрессия.
- **Цель спринта**: довести ботов до уровня TG Bot API (relevant subset), добавить ru-локализацию, реализовать AI Usage UI panel.

---

## 1. Локализация — RU fallback (~1209 ключей)

### Контекст

Сейчас `web/src/assets/localization/fallback.ru.strings` — короткий: 1041 строка. В коде используется ~1591 ключ; из них **1209** используются в наших экранах, есть в `fallback.strings` (англ), но отсутствуют в ru → отображаются raw key.

Полный список лежит в `.swarm/i18n_missing_ru.txt` (1209 строк, по ключу на строку).

### Шаги

1. **Сначала отфильтруй мусор** (ключи которые точно не нужны в Orbit, потому что фичу удалили):
   - prefix `Stars`, `Premium`, `Gift`, `Giveaway`, `Story`/`Stories`, `Auction`, `Suggested`, `Channel`, `Boost`, `WebApp`, `BotInline`/`Inline`, `Game`/`Games`, `Sticker`Premium, `Birthday`Premium, `Saved`Gif (если без чата), `Polls.AnonymousPaid` и т.п.
   - prefix `lng_premium_`, `lng_stories_`, `lng_gift_`, `lng_channel_`, `lng_payments_`, `lng_currency_`
   - Сохрани отфильтрованный список в `.swarm/i18n_to_translate.txt`. Печатай счёт: исходно/осталось.
2. **Батч-перевод через GPT** (по 80-100 ключей за вызов — чтобы укладываться в `max_tokens=8000`):
   - Для каждого батча собирай данные: `<key> = "<en value>"` (en value достаёшь из `fallback.strings` через grep).
   - Запрос к `mcp__gpt__gpt_call` с моделью `gpt-5.4`, `max_tokens=10000`, `schema` форсирует JSON `{translations: {key: ru_value}}`.
   - System prompt: «Translate UI strings for an enterprise messenger to Russian. Keep variable placeholders intact (`{name}`, `%1$s`, etc), keep markdown markers, plural keys append `_one`/`_other` correctly, keep tone neutral-formal. Don't translate brand names (Orbit, Saturn). Output JSON only».
3. **Сохраняй после каждого батча** в `fallback.ru.strings` (append, проверяй что ключ ещё не в файле). Если уже есть — не перезаписывай.
4. **Проверка**: после ВСЕГО батча запусти повторный grep+comm: должно остаться 0 missing real keys по нашим экранам (`web/src/components/left/settings/`, `web/src/components/right/`, `web/src/components/middle/`, `web/src/components/main/`, `web/src/components/auth/`).
5. **Build & smoke**: `docker compose build web && docker compose up -d --force-recreate web`. Открой `http://localhost:3000`, залогинься `test@orbit.local / TestPass123!`, переключи язык на русский (Settings → Language), пройдись по: Settings root, Privacy, Notifications, Chat folders, Active Sessions, Stickers and Emoji, AI usage. Всё что не русское — добей точечно.
6. **Коммит**: `feat(i18n): batch ru translation for ~N keys (Settings/Privacy/Notifications/etc)`.

### Acceptance criteria
- 0 raw keys на скринах Settings главного меню, Language, Privacy, Notifications, Chat folders, Active Sessions, AI usage.
- `comm -23 (used keys in our active screens) (ru keys)` даёт ≤ 50 ключей (хвост — реально неиспользуемые TG-only).
- Сборка `docker compose build web` зелёная.

---

## 2. AI Usage Panel — Settings page

### Контекст

Backend готов: `GET /api/v1/ai/usage` возвращает `AiUsageStats` (см. `web/src/api/saturn/methods/ai.ts:42-57`). Тип:
```ts
type AiUsageStats = {
  total_requests: number;
  by_endpoint: Record<string, number>;
  input_tokens: number;
  output_tokens: number;
  period_start: string;       // ISO
  cost_cents?: Record<string, number>;
  recent_samples?: Array<{
    endpoint: string;
    model: string;
    input_tokens: number;
    output_tokens: number;
    cost_cents: number;
    created_at: string;       // ISO
  }>;
};
```

Frontend method `fetchAiUsage()` уже зарегистрирован в `web/src/api/saturn/methods/index.ts`.

i18n ключи для панели уже есть в `fallback.ru.strings`: `AiUsageTitle`, `AiUsageUnavailable`, `AiUsageEmpty`, `AiUsageTotalRequests`, `AiUsageInputTokens`, `AiUsageOutputTokens`, `AiUsageCost`, `AiUsagePeriodFallback`, `AiUsageTotalsHeader`, `AiUsageByFeatureHeader`, `AiUsagePeriodSince`, `AiUsageEndpoint{Summarize|Translate|Transcribe|Ask}`. Если каких-то не хватает — добавь и en, и ru.

В `SettingsMain.tsx` уже есть пункт меню `lang('AiUsageTitle')` ведущий на `'AiUsage'` screen — проверь, и если ведёт на чужой экран, переподключи на новый.

### Шаги

1. **Создай компонент** `web/src/components/left/settings/SettingsAiUsage.tsx` по образцу `SettingsBotManagement.tsx` или `SettingsActiveSessions.tsx` (посмотри как они подгружают данные через actions/withGlobal).
2. **State**: храни `{stats, isLoading, error}`. На mount → вызови `fetchAiUsage` (через action или через прямой `callApi('fetchAiUsage')`). Если `error === 'ai_unavailable' || 503` → показывай `AiUsageUnavailable` баннер.
3. **UI** (одной колонкой, mobile-friendly):
   - **Header** "Использование AI"
   - **Period chip** (read-only) "С {date}" — `lang('AiUsagePeriodSince', { date: stats.period_start })`. Если `period_start` пуст — `AiUsagePeriodFallback`.
   - **Totals card**: `lang('AiUsageTotalsHeader')` + 3 строки:
     - Всего запросов: `stats.total_requests`
     - Входные токены: `lang.number(stats.input_tokens)`
     - Выходные токены: `lang.number(stats.output_tokens)`
     - Стоимость (если `cost_cents` есть и сумма >0): `$X.XX` через `(sumCents/100).toFixed(2)`
   - **By feature card**: `lang('AiUsageByFeatureHeader')` + список endpoint → count. Для каждого endpoint используй ключ `AiUsageEndpoint{Pascal(endpoint)}` если есть, иначе сам endpoint в монохроме.
   - **Recent samples** (если есть, max 10): таблица `endpoint | model | input/output | cost | дата` (формат через `formatTime` из `web/src/util/dateFormat.ts`).
   - **Empty state**: если `total_requests === 0` — показать `AiUsageEmpty`.
4. **Маршрутизация**: в `web/src/components/left/settings/Settings.tsx` (или где у нас `screen` switch) добавь case для `SettingsScreens.AiUsage` → `<SettingsAiUsage />`. Проверь enum `SettingsScreens` в `web/src/types/index.ts` — должен быть `AiUsage`. Если нет — добавь.
5. **Стили**: используй существующие паттерны settings — `.settings-content`, `.settings-item`, `.settings-item-header`. НЕ создавай новый scss модуль если можно переиспользовать.
6. **Mobile**: на 390px ширине таблица `recent_samples` должна скроллиться горизонтально (не ломать layout).
7. **Smoke**: залогинься `test@orbit.local`, открой Settings → AI usage. Должна показаться панель с текущей статистикой (или баннер «AI не настроен», если `ANTHROPIC_API_KEY=placeholder`).
8. **Коммит**: `feat(web): AI usage Settings panel with totals, by-feature breakdown, recent samples`.

### Acceptance criteria
- Панель открывается без ошибок в консоли.
- При 503 от backend — graceful banner.
- При пустом результате — `AiUsageEmpty`.
- Все строки переведены на ru (нет raw keys).
- TypeScript ошибки ≤ 32.

---

## 3. Боты — TG Bot API gaps (P0)

### Контекст

Анализ от GPT-5.4 (cross-checked) даёт MUST-have endpoints для корпорат:

**Сообщения:**
- `copyMessage` — relay без указания на оригинал, для compliance forward
- `editMessageReplyMarkup` — disable/swap кнопок после approve (КРИТИЧНО для approval flows)

**Чаты (read):**
- `getChat` — метаданные чата
- `getChatMember` — проверка членства/роли (КРИТИЧНО для access control)

**Чаты (write):**
- `banChatMember` — удалить юзера (увольнение, abuse)
- `restrictChatMember` — mute/limit (incident response)

**Invite:**
- `createChatInviteLink` — invite-only onboarding с auditability
- `revokeChatInviteLink` — ротация при offboarding

**Approve flow:**
- `approveChatJoinRequest` / `declineChatJoinRequest` — onboarding гейт

**Commands:**
- `setMyCommands` / `getMyCommands` / `deleteMyCommands` — discoverability

**Хорошо бы (P1):**
- `forwardMessage`, `editMessageCaption`, `sendMediaGroup` (album), `sendChatAction` (typing), `pinChatMessage`/`unpinChatMessage`, `getChatAdministrators`, `getChatMemberCount`, `sendPoll` (если poll API уже есть в messaging)

**SKIP:** `sendLocation`, `sendVenue`, `sendDice`, `unpinAllChatMessages`, `exportChatInviteLink`, `setChatStickerSet`, `setChatMenuButton`, `getChatMenuButton`, `answerInlineQuery`, `answerWebAppQuery`, `setMyName/Description` (косметика).

### Шаги

1. **Reuse существующие сервисы** в orbit. Не нужно дублировать логику:
   - chat metadata — есть в messaging service `GET /chats/:id` и `GET /chats/:id/members`
   - join request approve — `POST /chats/:id/join-requests/:userId/{approve,decline}` уже есть в messaging
   - invite link — `POST /chats/:id/invite-link` уже есть
   - ban/restrict — RBAC bitmask, проверь есть ли уже endpoint в messaging
   - pin/unpin — посмотри messaging handler
2. **Bot API endpoints — это тонкие proxy** к messaging service с переводом UUID и проверкой scope бота.
3. **Минимально (P0)** — добавь в `services/bots/internal/botapi/handler.go`:
   ```go
   router.Post("/copyMessage", h.copyMessage)
   router.Post("/forwardMessage", h.forwardMessage)
   router.Post("/editMessageReplyMarkup", h.editMessageReplyMarkup)
   router.Post("/editMessageCaption", h.editMessageCaption)
   router.Post("/sendChatAction", h.sendChatAction)
   router.Post("/pinChatMessage", h.pinChatMessage)
   router.Post("/unpinChatMessage", h.unpinChatMessage)
   router.Get("/getChat", h.getChat)
   router.Get("/getChatMember", h.getChatMember)
   router.Get("/getChatAdministrators", h.getChatAdministrators)
   router.Get("/getChatMemberCount", h.getChatMemberCount)
   router.Post("/setMyCommands", h.setMyCommands)
   router.Get("/getMyCommands", h.getMyCommands)
   router.Post("/deleteMyCommands", h.deleteMyCommands)
   router.Post("/banChatMember", h.banChatMember)
   router.Post("/restrictChatMember", h.restrictChatMember)
   ```
4. **Не делай** `sendMediaGroup` без обсуждения (нужен album support в messaging — отдельная задача). Спроси через gpt_call перед началом.
5. **Каждый endpoint** должен:
   - Проверить bot installed в chat (метод `h.svc.IsBotInstalled` уже есть)
   - Проверить scope (`CheckBotScope`) — для read endpoints добавь новый scope `ScopeReadChat`, для admin endpoints — `ScopeManageMembers` (новый bit, см. `ScopePostMessages` как референс)
   - Rate-limit (уже работает через `checkRateLimit`)
   - Возвращать TG-style response через `botSuccess(c, result)` / `botError`
6. **Models** — расширь `models.go`:
   ```go
   type CopyMessageRequest struct { ChatID, FromChatID, MessageID string; Caption *string `json:"caption,omitempty"`; ReplyMarkup json.RawMessage; ReplyToMessageID *string }
   type ForwardMessageRequest struct { ChatID, FromChatID, MessageID string }
   type EditReplyMarkupRequest struct { ChatID, MessageID string; ReplyMarkup json.RawMessage }
   type EditCaptionRequest struct { ChatID, MessageID, Caption string; ReplyMarkup json.RawMessage }
   type SendChatActionRequest struct { ChatID, Action string } // typing|upload_photo|upload_document|upload_video|upload_voice
   type PinChatMessageRequest struct { ChatID, MessageID string; DisableNotification bool `json:"disable_notification,omitempty"` }
   type UnpinChatMessageRequest struct { ChatID, MessageID string }
   type SetMyCommandsRequest struct { Commands []model.BotCommand `json:"commands"`; Scope *CommandScope `json:"scope,omitempty"` } // scope optional, default=all
   type CommandScope struct { Type string `json:"type"` } // default|all_private_chats|all_group_chats — без chat-specific scope для упрощения
   type BanChatMemberRequest struct { ChatID, UserID string; UntilDate *int64 `json:"until_date,omitempty"` }
   type RestrictChatMemberRequest struct { ChatID, UserID string; UntilDate *int64; Permissions *Permissions `json:"permissions,omitempty"` }
   ```
7. **Messaging client** — расширь `services/bots/internal/client/messaging_client.go` методами:
   - `CopyMessage(ctx, fromChatID, toChatID, msgID, caption, replyMarkup) (*Message, error)` — внутренне читает source message и постит копию через существующий `SendMessage`. Атомарность не нужна.
   - `ForwardMessage(...)` — то же что copyMessage, но с пометкой `forward_from_id` в metadata (надо проверить есть ли поле в messages таблице, если нет — пометка в text "Переслано от: ..." как fallback).
   - `EditReplyMarkup(ctx, msgID, markup)` — `PATCH /messages/:id` с partial body.
   - `EditCaption(ctx, msgID, caption)` — то же что edit text но для media сообщения.
   - `SendChatAction(ctx, chatID, action)` — публикует WS event `typing`/`upload_*` через `POST /chats/:id/typing` (если такого endpoint нет — добавь minimal в messaging, fire-and-forget event через NATS).
   - `PinMessage(ctx, msgID)` / `UnpinMessage(ctx, msgID)` — `POST /messages/:id/pin` / `DELETE`.
   - `GetChat(ctx, chatID) (*Chat, error)`, `GetChatMember(...)`, `GetChatAdministrators(...)`, `GetChatMemberCount(...)`.
   - `BanMember(ctx, chatID, userID, until *int64)` — `DELETE /chats/:id/members/:userId` + audit.
   - `RestrictMember(...)` — обновляет RBAC bitmask конкретного member.
8. **Tests** — для каждого endpoint один happy path + один error case (bot not installed → 403). Шаблон бери из `handler_ratelimit_test.go`.
9. **Reply-markup validator** — каждый edit*/copy* endpoint должен прогонять reply_markup через `ValidateReplyMarkup`, как делают существующие sendMessage/edit.
10. **Если упёрся в архитектурный вопрос** (например: «как мапить TG int64 chat_id на наш UUID если бот пишет с TG SDK?») — НЕ выдумывай. Спроси gpt-5.4 с приложением `services/bots/internal/botapi/handler.go` + `services/messaging/internal/handler/chat_handler.go`. Получи second opinion от gemini, выбери победившее решение, реализуй.
11. **Коммиты** дроби по логическим блокам:
    - `feat(bots): copyMessage and forwardMessage in TG-compatible Bot API`
    - `feat(bots): editMessageReplyMarkup and editMessageCaption`
    - `feat(bots): chat read endpoints (getChat/getChatMember/getChatAdministrators/getChatMemberCount)`
    - `feat(bots): sendChatAction (typing indicator)`
    - `feat(bots): pinChatMessage and unpinChatMessage`
    - `feat(bots): setMyCommands per scope`
    - `feat(bots): banChatMember and restrictChatMember (admin bots)`

### Acceptance criteria
- Все 16 endpoints отвечают корректно (не 404/500).
- Для каждого есть unit-test happy path + 1 error case.
- Reply-markup валидируется на edit/copy endpoints.
- `cd services/bots && go test ./...` зелёный.
- Build: `docker compose build bots` зелёный.
- Smoke: создай бота, установи в чат, прогони copyMessage из чата A в B → сообщение появилось в B без указания на A. Прогони editMessageReplyMarkup → кнопки изменились.

---

## 4. Боты — Security hardening (P0)

### Контекст

Gemini-3.1-pro-preview подтвердил три обязательные фичи для enterprise:
1. Webhook destination control — allow-list + replay protection
2. Audit log всех admin действий
3. Per-IP/per-token/per-endpoint rate limit + secret hygiene

### Задачи

#### 4.1. Webhook URL allow-list

- Env var: `BOT_WEBHOOK_ALLOWLIST` (comma-separated list of allowed hostnames, e.g. `mst.local,internal.mst.local,*.saturn.ac`).
- Проверка при `setWebhook`: parse URL, проверить host в allow-list. Wildcard `*.X` матчит любой subdomain X.
- Если env пуст — всё разрешено (dev mode), лог WARN при старте сервиса.
- Тест: `setWebhook` с URL вне allow-list → 400.
- Коммит: `feat(bots): webhook URL allow-list to prevent SSRF/exfiltration`.

#### 4.2. Audit log админ-действий ботов

- Новая таблица миграция `migrations/063_bot_audit_log.sql`:
  ```sql
  CREATE TABLE bot_audit_log (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      actor_id UUID NOT NULL REFERENCES users(id) ON DELETE SET NULL,
      bot_id UUID REFERENCES bots(id) ON DELETE SET NULL,
      action TEXT NOT NULL CHECK (action IN ('create','update','delete','token_rotate','install','uninstall','set_webhook','delete_webhook','set_commands')),
      details JSONB NOT NULL DEFAULT '{}'::jsonb,
      source_ip INET,
      user_agent TEXT,
      created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );
  CREATE INDEX idx_bot_audit_actor ON bot_audit_log(actor_id, created_at DESC);
  CREATE INDEX idx_bot_audit_bot ON bot_audit_log(bot_id, created_at DESC);
  ```
- Store: `services/bots/internal/store/audit_store.go` с методами `Log(ctx, entry)`, `ListByBot(ctx, botID, limit)`, `ListByActor(ctx, actorID, limit)`.
- Подключи в каждый admin handler (`bot_handler.go` createBot/updateBot/deleteBot/rotateToken/installBot/uninstallBot, и в Bot API setWebhook/deleteWebhook/setMyCommands).
- Endpoint для просмотра: `GET /bots/:id/audit?limit=50` — owner или superadmin only.
- Acceptance: после create/delete бота в БД появляется запись с правильным action.
- Коммит: `feat(bots): audit log for admin actions (create/update/delete/rotate/install/uninstall/webhook/commands)`.

#### 4.3. Per-IP rate limit на /bot/:token/*

- Уже есть per-bot 30 req/sec. Добавь параллельно per-IP — 60 req/sec на ВСЕ /bot/:token/*.
- Lua script отдельный, ключ `ratelimit:botapi:ip:<source_ip>` (используй `c.IP()` после `EnableTrustedProxyCheck`).
- Хук: middleware `enforceIPRateLimit` перед `enforceBotRateLimit`.
- При срабатывании: 429 Too Many Requests с Retry-After.
- Test: 100 запросов с одного IP за 1 sec → последний должен быть 429.
- Коммит: `feat(bots): per-IP rate limit on Bot API in addition to per-bot`.

### Acceptance criteria всего блока 4
- Все 3 фичи задеплоены, тесты зелёные.
- Bot Webhook без allow-list → 400 (dev unless env пуст).
- Audit log за 5 действий = 5 строк в БД.
- IP-rate limit срабатывает при превышении.

---

## 5. Боты — Generic Approvals template (опционально, P1)

### Контекст

HR-bot — частный случай approval workflow. Gemini рекомендует extract в generic Approvals Bot, потому что 80% корпоративных bot use-cases это «согласование» (доступы, закупки, командировки, отгулы, бюджет, исключения).

### Schema

- Миграция `migrations/064_bot_approvals.sql`:
  ```sql
  CREATE TABLE bot_approval_requests (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      bot_id UUID NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
      chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
      requester_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      approval_type TEXT NOT NULL CHECK (char_length(approval_type) BETWEEN 1 AND 64),
      subject TEXT NOT NULL CHECK (char_length(subject) BETWEEN 1 AND 200),
      payload JSONB NOT NULL DEFAULT '{}'::jsonb,
      status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected','cancelled')),
      version INT NOT NULL DEFAULT 1,
      decided_by UUID REFERENCES users(id) ON DELETE SET NULL,
      decided_at TIMESTAMPTZ,
      decision_note TEXT,
      created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );
  CREATE INDEX idx_bot_approvals_chat ON bot_approval_requests(chat_id, status);
  CREATE INDEX idx_bot_approvals_requester ON bot_approval_requests(requester_id, status);
  CREATE INDEX idx_bot_approvals_pending ON bot_approval_requests(status) WHERE status = 'pending';
  CREATE TRIGGER trg_bot_approvals_updated_at
      BEFORE UPDATE ON bot_approval_requests
      FOR EACH ROW EXECUTE FUNCTION update_updated_at();
  ```
- В коде:
  - `services/bots/internal/model/approval.go`
  - `services/bots/internal/store/approval_store.go` — Create/GetByID/List/Decide(version-CAS)/Cancel
  - `services/bots/internal/handler/approval_handler.go` — REST как у HR:
    - `POST /bots/:botID/approvals` — create (любой пользователь чата)
    - `GET /bots/:botID/approvals?chat_id=...&status=...` — list (filter scope как в HR)
    - `PATCH /bots/:botID/approvals/:id` — decide (owner-only) `{decision: approve|reject, note}`
    - `POST /bots/:botID/approvals/:id/cancel` — cancel (только requester или owner)
- Тесты — копи-пасту структуры из `hr_handler_test.go`.

### Acceptance criteria
- Approve flow: create → list → decide → 409 при повторном decide.
- Cancel: только requester or bot-owner.
- Тесты зелёные.

### Коммит
`feat(bots): Generic Approvals template (migration 064 + REST + tests)`.

---

## 6. Финальный smoke + checklist

После завершения 1-5 (или просто 1-4 если 5 решишь отложить) сделай:

1. `cd services/bots && go test ./...` — все зелёные.
2. `docker compose build` — все 8 сервисов + web собираются.
3. `docker compose up -d` локально, проверь endpoints через curl:
   - `GET /api/v1/ai/usage` (с JWT) — 200 либо 503 ai_unavailable
   - `POST /api/v1/bot/<token>/copyMessage` — happy + error
   - `POST /api/v1/bot/<token>/sendChatAction` — 200
   - `POST /api/v1/bot/<token>/setMyCommands` — 200
   - `POST /api/v1/bots/<id>/audit` — список 5+ записей
4. Создай файл `.swarm/sprint-2026-04-25-report.md` с отчётом:
   - Что сделано
   - Что отложено и почему
   - Где спрашивал gpt/gemini
   - Список коммитов

5. `git push origin master` — финальный.

---

## 7. Что делать при ошибках

| Симптом | Действие |
|---------|---------|
| Не уверен в архитектурном решении | gpt-5.4 + gemini параллельно, выбери совпавшее |
| TS-ошибки выросли > 32 | Откатить изменения в frontend или починить точечно |
| Тест Go не проходит | НЕ удалять тест. Починить код. Если тест неправильный — обосновать в коммите |
| Миграция не накатывается | Проверь что function `update_updated_at()` есть (в 010_create_updated_at_trigger.sql) — НЕ `update_updated_at_column()` |
| Webhook к URL не отправляется | Проверить `BOT_WEBHOOK_ALLOWLIST` в .env |
| `gpt_call` timeout | Уменьши batch size; или используй `gemini-3.1-pro-preview` (1M контекст, обычно стабильнее) |

---

## 8. Не делай

- НЕ создавай новые сервисы
- НЕ ставь новые npm/go зависимости (если только не упёрся в стену — тогда обоснуй)
- НЕ трогай go.mod версии без объяснения причины
- НЕ удаляй существующие тесты, если они проходили
- НЕ оставляй TODO/FIXME в коде, который коммитишь
- НЕ создавай 5 новых bot-templates сразу — только Approvals (опционально). Helpdesk/Expense/Meeting/On-call отложи на следующий спринт
- НЕ трогай мою работу: миграцию 061 (фикс trigger), auth IP/UA NULL handling, web build dead imports — это уже закоммичено
- НЕ делай git rebase/force-push
- НЕ коммить с `--no-verify` или `--amend`

---

## 9. Финальная проверка перед каждым `git push`

```bash
cd services/bots && go test ./... && go vet ./...
cd web && npm run check
docker compose build bots web
```

Все три зелёные → push.

---

## Резюме

Спринт = 4 блока обязательных + 1 опциональный:
1. **i18n ru** — батч-перевод 1209 ключей через gpt-5.4
2. **AI Usage Panel** — Settings UI компонент
3. **Bot API gaps** — 16 TG-методов: copy/forward/editReplyMarkup/editCaption/sendChatAction/pin/unpin/getChat/getChatMember/getChatAdministrators/getChatMemberCount/setMyCommands/getMyCommands/deleteMyCommands/banChatMember/restrictChatMember
4. **Bot Security** — webhook allow-list, audit log (миграция 063), per-IP rate limit
5. **Generic Approvals template** (опционально) — миграция 064, REST endpoints, тесты

Цель: после спринта боты — production-ready для корпорат на 150 человек, без overkill, без public-discovery фич.

Работай автономно, спрашивай советников при сомнениях, коммить и пушь часто. Удачи.
