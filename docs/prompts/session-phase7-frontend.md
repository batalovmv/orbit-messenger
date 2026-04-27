# Промпт для новой сессии: Phase 7 E2E Encryption — Frontend

> Скопируй весь блок ниже (от `---` до конца) в новый чат Claude Code. Контекст
> большой, потому что Phase 7 — это crypto-критический блок и фреш-сессии
> нужно полное введение без размышлений.

---

Мы в проекте **Orbit Messenger** (корпоративный мессенджер MST на 150 сотрудников, форк TG Web A на фронте, 8 Go микросервисов на бэке, self-hosted на Saturn.ac). Репозиторий: `D:\job\orbit`. Читай `CLAUDE.md` в корне для общих правил — там описаны конвенции Go, Saturn API naming, permissions, NATS subjects и т.д. Для фронта — `web/CLAUDE.md`.

## Задача сессии: Phase 7 E2E Encryption — реализация фронтенда

Backend Phase 7 **полностью готов** (я проверил в предыдущей сессии — см. commit `0be6da3`). Здесь работа только на клиентской стороне: libsignal через Web Worker, IndexedDB для ключей, Saturn methods, device enrollment, send/receive pipeline, Safety Numbers UI, disappearing messages UI, client-side search.

**Обязательно прочитай перед началом:**
- `docs/phase7-design.md` — **полный design документ**. Все архитектурные решения приняты, все 8 открытых вопросов закрыты в §14 Decisions. Не переоткрывай их. Если видишь противоречие — следуй design документу, а не своей интуиции.
- `docs/SIGNAL_PROTOCOL.md` — trust model, envelope format, key lifecycle (короткий ~110 строк).
- `PHASES.md` — раздел Phase 7 (строки ~1085-1180). Там отмечено что backend полностью готов.
- `web/CLAUDE.md` — Teact особенности, Saturn API конвенции, отказ от React типов, withGlobal/getActions паттерн.

## Ключевые архитектурные решения (из design doc §14, не обсуждаем)

1. **Scope: только текст в DM.** Media encryption — Phase 7.1, отдельный PR. Группы — не-E2E навсегда.
2. **Library:** `@signalapp/libsignal-client` (официальный Signal Foundation, Rust→WASM). НЕ `libsignal-protocol-javascript` (deprecated).
3. **Web Worker:** libsignal исполняется вне main thread — обязательно. Вся crypto работа через постмессадж.
4. **Load strategy:** background async `import()` **после auth success**, НЕ на boot, НЕ лениво при первом DM open. Device enrollment должен успеть до того как пользователь откроет первый DM.
5. **IndexedDB database:** `orbit-crypto`, 4 object stores: `identity` (single `self` row), `signed_prekeys`, `one_time_prekeys`, `sessions` (key: `peerUserId:peerDeviceId`). См. §5 дизайн-дока.
6. **No fallback "send unencrypted".** Если у peer'а нет ключей — блокируем отправку с понятным русским сообщением. НЕ добавляй кнопку "отправить незашифрованно" даже если очень хочется.
7. **Rekeying warning:** inline system message в чате + unobtrusive badge, НЕ modal.
8. **Client-side search:** persistent IndexedDB, store `search_index`, простой inverted index. См. §12.
9. **Envelope storage:** raw BYTEA JSON, server-opaque. Бэкенд уже так хранит — не трогай.
10. **Rollout:** глобальный `feature_flags.e2e_dm_enabled` флаг, НЕ per-DM opt-in. Все **новые** DM после флип флага автоматически E2E, старые остаются plaintext.

## Что уже готово на backend (не трогай, используй)

**Auth service (9 endpoints, 14 тестов PASS):**
- `POST /keys/identity` / `POST /keys/signed-prekey` / `POST /keys/one-time-prekeys`
- `GET /keys/:userId/bundle` (атомарный consume one-time prekey)
- `GET /keys/:userId/identity` / `GET /keys/:userId/devices`
- `GET /keys/count` / `GET /keys/transparency-log`
- `DELETE /keys/device`

Ключи валидируются: Ed25519 32B, X25519 32B, signature 64B, batch cap 100.

**Messaging service:**
- `POST /chats/:id/messages/encrypted` — принимает `{envelope: json.RawMessage}` в body + `X-Device-ID` header
- Disappearing cron worker запущен, `expires_at` автоматически ставится при `chat.disappearing_timer > 0`
- Media guard: `SendMediaMessage` rejects в E2E чате ("Cannot send plaintext media to an E2E encrypted chat") — это enforce'ит твой Phase 7.0 text-only контракт
- Meilisearch indexer skip для encrypted сообщений

**Gateway:**
- Push preview suppression для `type=encrypted` → body = "Новое сообщение"
- `X-Device-ID` header проксируется через все сервисы

## Порядок работ (из design doc §15)

Иди строго в этом порядке — предыдущие шаги являются зависимостями следующих.

### Шаг 1: libsignal + IndexedDB foundation (2-3 дня)

**Файлы (все новые):**
- `web/src/lib/crypto/libsignal-wrapper.ts` — lazy dynamic import `@signalapp/libsignal-client`, инициализация WASM
- `web/src/lib/crypto/crypto.worker.ts` — Web Worker entry point, принимает сообщения `{op, payload}`, все libsignal calls идут тут
- `web/src/lib/crypto/worker-proxy.ts` — main-thread wrapper который постит в worker и возвращает Promise
- `web/src/lib/crypto/key-store.ts` — IndexedDB через `idb` lib (проверь есть ли в package.json, если нет — добавь). 4 object stores из дизайн-дока §5. Versioned schema (version=1).
- `web/src/lib/crypto/device-manager.ts` — `initializeIfNeeded()`, `getDeviceId()`, `generateAndUploadKeys()`, `rotateSignedPreKey()`, `replenishOneTimePreKeys()`
- `web/src/lib/crypto/session-manager.ts` — X3DH bootstrap, Double Ratchet state load/persist
- `web/src/lib/crypto/message-crypto.ts` — `encryptForPeers(peerBundles[], plaintext) → envelope`, `decryptMessage(envelope, senderDeviceId) → plaintext`
- `web/src/lib/crypto/safety-numbers.ts` — `computeSafetyNumber(selfIdentity, peerIdentity) → string` (SHA-256, 60 digits, 5 groups by 12)
- `web/src/lib/crypto/__tests__/crypto.test.ts` — **CRITICAL:** unit test Alice ↔ Bob round-trip в jsdom. Без этого не merge.

**Webpack:** `web/webpack.config.ts` может потребовать `experiments: { asyncWebAssembly: true }` если ещё нет. Проверь.

**Dependency:** `pnpm add @signalapp/libsignal-client` в `web/`. Проверь bundle size через `pnpm build` — должен лениво грузиться, не тянуться в main chunk.

**Acceptance:** unit test Alice encrypt → Bob decrypt round-trip PASS. Bundle не растёт в main chunk.

### Шаг 2: Saturn methods keys.ts (0.5 день)

**Файл:** `web/src/api/saturn/methods/keys.ts` (новый). Обёртки над готовыми backend endpoint'ами. Используй существующий `request()` helper из `client.ts`:

- `uploadIdentityKey({ identityKey: Uint8Array, deviceId: string })`
- `uploadSignedPreKey({ signedPreKey, signature, signedPreKeyId, deviceId })`
- `uploadOneTimePreKeys({ keys: Array<{keyId, publicKey}>, deviceId })`
- `fetchKeyBundle(userId: string)` → массив bundle'ов (по одному на device)
- `fetchIdentityKey(userId: string)`
- `fetchPreKeyCount(deviceId: string)` → number
- `fetchKeyTransparencyLog(userId?: string)`
- `fetchDevices()` → Device[]
- `deleteDevice(deviceId: string)`
- `sendEncryptedMessage({ chatId, envelope })` — POST /chats/:id/messages/encrypted
- `setDisappearingTimer({ chatId, seconds })` — PUT /chats/:id/disappearing (или существующий endpoint, найди через grep)
- `fetchDisappearingTimer(chatId)` — может быть в getChat response уже

**Base64url:** все ключи передаются по сети как base64url (RFC 4648 без padding). Backend делает decode. На фронте кодируй через стандартный `btoa` + replace или `TextEncoder`. Не используй стандартный `base64` с `+/=`.

**Зарегистрируй** методы в `web/src/api/saturn/methods/index.ts`.

### Шаг 3: Device enrollment flow (0.5 день)

**Место:** найди где происходит auth success — скорее всего в `web/src/global/actions/api/saturnAuth.ts` или в callback `updateAuthorizationState → authorizationStateReady`.

**После успешного login/register:**
1. Background `import('../../lib/crypto/libsignal-wrapper')` — ленивая загрузка WASM
2. После загрузки: `deviceManager.initializeIfNeeded()` — проверка IndexedDB на identity key
3. Если нет — generate + upload (через Saturn methods из шага 2)
4. Если есть — проверить `fetchPreKeyCount()` < 20 → replenish
5. Проверить `last_signed_prekey_rotation` — если > 7 дней назад → rotate

**Non-blocking:** ошибки только в лог (`console.warn`), не ломай auth flow. Пользователь может пользоваться мессенджером без E2E — просто E2E DM не будут доступны до успешного enrollment.

### Шаг 4: Send/Receive encrypted DM (2 дня)

**Send path — `web/src/global/actions/api/messages.ts`:**
Найди `sendMessage` action handler. Добавь branch:
```
const chat = selectChat(global, chatId)
if (chat?.isEncrypted) {
  // 1. Fetch bundle для peer (+ cache в sessions)
  // 2. encryptForPeers → envelope
  // 3. callApi('sendEncryptedMessage', { chatId, envelope })
  // 4. Optimistic update в UI (показать как обычное сообщение)
  return
}
// existing non-E2E path
```

**Receive path — `web/src/api/saturn/updates`:**
Найди WS handler для `new_message` update. Если `message.is_encrypted`:
1. `decryptMessage(envelope)` → plaintext
2. Store в **runtime-only** cache (IndexedDB `message_cache` store из шага 1)
3. В global state храни placeholder (не plaintext!)
4. UI читает из runtime cache по id сообщения при render

**CRITICAL:** plaintext **никогда** не сохраняется в сериализованный global state. Проверь `web/src/global/cache.ts` — там migration pattern. Добавь filter что E2E сообщения сохраняются как placeholder `__E2E_ENCRYPTED__`.

**Smoke test:** две вкладки (Alice + Bob), SQL flip `UPDATE feature_flags SET enabled=true WHERE key='e2e_dm_enabled'`, создать DM, отправить, проверить что в БД `messages.content IS NULL` и `encrypted_content IS NOT NULL`, в UI обоих — plaintext.

### Шаг 5: Multi-device fanout (1 день)

`encryptForPeers` уже должен принимать массив bundle'ов из шага 1 — расширь чтобы включать **все** devices peer'а + **все** свои other devices (кроме текущего device_id).

Envelope format из `docs/SIGNAL_PROTOCOL.md:47`:
```json
{
  "v": 1,
  "sender_device_id": "uuid",
  "devices": { "device-uuid-1": { "type": 2, "body": "base64" }, ... }
}
```

Receive path: читает `envelope.devices[selfDeviceId]`. Если его там нет (например sender забыл включить) — показать `[зашифровано: не для этого устройства]`.

### Шаг 6: Safety Numbers UI (0.5 день)

**Файл:** `web/src/components/right/SafetyNumbersModal.tsx` (новый).

Точка входа: в `Profile.tsx` добавить кнопку "Verify Identity" (только для E2E DM). Modal показывает:
- 60-digit safety number из `computeSafetyNumber(...)` — 5 групп по 12 цифр
- Кнопка "Copy"
- Кнопка "Mark as Verified" → сохраняет в `verified` store (IndexedDB)
- При verified — badge "✓" в header чата

### Шаг 7: Disappearing messages UI (0.5 день)

В Chat settings (правая панель) — новый dropdown "Disappearing messages: Off / 24h / 7d / 30d". Вызов `setDisappearingTimer(chatId, seconds)`.

При `disappearing_timer > 0` — badge "⏱ 24h" в header чата.

### Шаг 8: Client-side search fallback (1 день)

**Файл:** `web/src/lib/search/client-index.ts` (новый).

- IndexedDB store `search_index`: `word → Array<{message_id, chat_id}>`
- `addToIndex(messageId, chatId, plaintext)` — вызывается при каждом decrypt в шаге 4
- `searchClient(query, chatId)` → matched message IDs
- Простой tokenize: lowercase + split whitespace, без stemming

В search action: если `chat.isEncrypted` → использовать `searchClient` вместо backend `/search`.

### Шаг 9: Rollout docs (0.5 день)

**Файл:** `docs/e2e-user-guide.md` — короткий гид для юзеров. Что такое замочек, как проверить Safety Numbers, что делать если ключи изменились.

## Что НЕ делать

- **Media encryption** — Phase 7.1, не сейчас.
- **Групповой E2E / Sender Keys** — deferred или никогда.
- **Escrow key flow** — deferred per SIGNAL_PROTOCOL.md. `compliance_keys` таблица существует но пустая, не трогай.
- **QR code для Safety Numbers** — nice-to-have, пропусти.
- **Automated browser E2E тесты** — только unit tests на crypto round-trip. Никаких Playwright.
- **Backend изменения** — backend полностью готов. Если кажется что нужно — сначала проверь что именно отсутствует, спроси.
- **Retroactive migration** старых plaintext сообщений — старые DM остаются plaintext навсегда.

## Checklist перед merge

- [ ] `cd web && pnpm test -- crypto` — crypto unit tests PASS
- [ ] `cd web && pnpm build` — собирается без ошибок, libsignal в отдельном chunk, main chunk не разросся
- [ ] `tsc --noEmit` — 0 новых ошибок
- [ ] Manual smoke (две вкладки, Alice+Bob): enroll → send → receive → reload → decrypt → Safety Numbers → disappearing 30s
- [ ] Feature flag проверен: с `e2e_dm_enabled=false` старые DM работают как раньше, новые DM создаются как plaintext
- [ ] `PHASES.md` обновлён — отметить выполненные пункты

## Стиль работы в этой сессии

- Russian неформально, как с CTO (см. CLAUDE.md §"Роль AI")
- **Каждый шаг коммить отдельно** с conventional commit (`feat(crypto): ...`, `feat(web): ...`). Не один гигантский коммит в конце.
- **Context7 MCP** для актуальной документации libsignal/@signalapp — гугли через `resolve-library-id` → `get-library-docs`
- Если есть сомнение по дизайн-решению — **сверяйся с `docs/phase7-design.md`**, не пересобирай на ходу
- **Crypto critical:** не полагайся на "наверное правильно". Читай код, запускай тесты. Каждое изменение в `web/src/lib/crypto/` — пересобирай unit tests

Старт: прочитай `docs/phase7-design.md`, `docs/SIGNAL_PROTOCOL.md`, затем покажи план Шага 1 (libsignal foundation) и приступай.
