# Phase 7: E2E Encryption — Design Document

**Статус:** APPROVED — все открытые вопросы закрыты (см. §14 Decisions).

**Скоуп:** E2E шифрование для DM (один-на-один). Группы, каналы, боты, AI, integrations — не-E2E навсегда (или до Phase 7.2).

**Библиотека:** `@signalapp/libsignal-client` — официальная от Signal Foundation (Rust→WASM, audited). Подключается через Web Worker для отсутствия блокировки main thread.

**Критический вопрос Escrow Key:** **отложен**. [docs/SIGNAL_PROTOCOL.md:6](SIGNAL_PROTOCOL.md) явно фиксирует: `"Compliance/Escrow: deferred. Schema ready (compliance_keys table), flow not implemented in Phase 7."`. Phase 7 делается без интеграции с Super Access — `compliance_keys` таблица остаётся пустой.

---

## 1. Что уже готово на backend

**Финальный аудит backend'а (проведён в сессии 2026-04-15) показал что Phase 7 backend — на 100% готов.**

### 1.1 Проверено и работает

**Auth service (key server) — 9 endpoints:**
- `POST /keys/identity` — register device keys ([services/auth/internal/handler/key_handler.go](../services/auth/internal/handler/key_handler.go))
- `POST /keys/signed-prekey` — rotate signed prekey
- `POST /keys/one-time-prekeys` — upload batch (cap 100)
- `GET /keys/:userId/bundle` — fetch bundle для session init (атомарный consume one-time prekey)
- `GET /keys/:userId/identity` — fetch identity key для Safety Numbers
- `GET /keys/:userId/devices` — list user devices
- `GET /keys/count` — prekey count для replenish triggering
- `GET /keys/transparency-log` — key change audit log
- `DELETE /keys/device` — revoke device

**Атомарность one-time prekey consume** — канонический PostgreSQL паттерн `UPDATE ... WHERE id = (SELECT ... FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING ...` в [prekey_store.go:69-98](../services/auth/internal/store/prekey_store.go). Race-condition невозможен.

**Key validation:**
- `len(identityKey) == 32` (Ed25519 public)
- `len(signedPreKey) == 32` (X25519 public)
- `len(signedPreKeySignature) == 64` (Ed25519 signature)
- `len(oneTimePreKey.PublicKey) == 32`
- Batch cap 100

**Тесты:** 14 handler tests PASS, включая validation errors, missing headers, too many prekeys, invalid key sizes.

**Messaging service — encrypted message pipeline:**
- `POST /chats/:id/messages/encrypted` endpoint ([message_handler.go:78](../services/messaging/internal/handler/message_handler.go))
- `MessageService.SendEncryptedMessage` ([message_service.go:366+](../services/messaging/internal/service/message_service.go)) — создаёт с `Type=MessageTypeEncrypted`, `EncryptedContent=envelope`, применяет disappearing timer, публикует NATS event с envelope для fanout по member_ids
- `MessageStore.CreateEncrypted` — отдельный path для E2E
- `publishEncryptedMessageSent` — strips plaintext `Content` перед NATS publish

**Disappearing messages:**
- `CleanupService` ([cleanup_service.go](../services/messaging/internal/service/cleanup_service.go)) — goroutine ticker, interval 1 минута
- Запущен в [cmd/main.go:252](../services/messaging/cmd/main.go) через `go cleanupSvc.Start(cronCtx)`
- `DeleteExpired` store method: `DELETE FROM messages WHERE expires_at IS NOT NULL AND expires_at < NOW()`
- `expires_at` автоматически ставится в send path'ах (обоих — plaintext и encrypted) когда `chat.DisappearingTimer > 0`

**Push preview suppression:**
- [gateway/internal/ws/nats_subscriber.go:399-401](../services/gateway/internal/ws/nats_subscriber.go) — `if msg.Type == "encrypted" { payload.Body = "Новое сообщение" }`
- Plaintext не логируется в push delivery path

**Media guard in E2E chats:**
- `SendMediaMessage` rejects с `"Cannot send plaintext media to an E2E encrypted chat"` ([message_service.go:810](../services/messaging/internal/service/message_service.go))
- Enforces Phase 7.0 text-only contract — клиент не может случайно отправить plaintext media в E2E чат

**Meilisearch indexer skip:**
- [indexer.go:122-123](../services/messaging/internal/search/indexer.go) — `if m.Type == "encrypted" || m.Content == nil { return }` — не индексирует E2E сообщения

**Feature flag:** `feature_flags.e2e_dm_enabled=false` ([migration 045](../migrations/045_feature_flags.sql)), guard в [chat_service.go:179](../services/messaging/internal/service/chat_service.go).

**Tables:** `user_keys`, `one_time_prekeys`, `key_transparency_log`, `compliance_keys` (schema-only, deferred), `chats.is_encrypted`, `chats.disappearing_timer` — все существуют.

### 1.2 Чего реально нет — ТОЛЬКО frontend

- Frontend crypto layer (libsignal-client через Web Worker, IndexedDB)
- Saturn методы — wrapper'ы для существующих backend endpoints
- Encrypt/decrypt pipeline в send/receive messages на клиенте
- Device enrollment flow (after auth success)
- Safety Numbers UI компонент + Safety Number hash algorithm
- Disappearing messages UI (dropdown в Chat settings)
- Client-side search fallback (IndexedDB inverted index)

### 1.3 Пересмотренная оценка

Phase 7 — **почти полностью frontend задача**. Backend нужно только мониторить. Пересмотренный объём:

| Задача | Backend | Frontend | Дней |
|--------|---------|----------|------|
| libsignal + IndexedDB foundation | 0 | весь | 2-3 |
| Saturn key methods wrapper | 0 | весь | 0.5 |
| Device enrollment flow | 0 | весь | 0.5 |
| Send/Receive encrypted DM | 0 | весь | 2 |
| Multi-device fanout | 0 | весь | 1 |
| Safety Numbers UI | 0 | весь | 0.5 |
| Disappearing messages UI | 0 | весь | 0.5 |
| Client-side search fallback | 0 | весь | 1 |
| Rollout + docs | 0 | 0.5 | 0.5 |
| **Итого** | **0 дней** | **~8-9 дней** | **~8-9 дней** |

Было 11-13 дней в первоначальной оценке. Экономия 3-4 дня за счёт того что backend **уже** реализован.

---

## 2. Трастовая модель (фиксируем явно)

Из [docs/SIGNAL_PROTOCOL.md](SIGNAL_PROTOCOL.md):

**Что сервер НЕ видит для E2E DM:**
- Plaintext содержимого сообщений
- Plaintext медиа (аудио/фото/видео до пре-шифрования клиентом)

**Что сервер ВИДИТ:**
- `chat_id`, `sender_id`, `timestamp`, `sequence_number`
- Размер encrypted blob
- Статус доставки
- Метаданные чата (имя, аватар — они не E2E)

**Sealed Sender:** не реализуем. `sender_id` в метаданных — это compliance-приемлемо для корпоративного мессенджера.

**Retroactive:** старые plaintext сообщения **не** перешифровываются. E2E применяется только к новым DM, созданным после flip флага.

---

## 3. Key lifecycle

### 3.1 Device enrollment (первый login на устройстве)

**Триггер:** пользователь успешно прошёл auth flow (login/register) на новом browser instance (нет записей в `orbit-crypto` IndexedDB database).

**Шаги:**
1. Сгенерировать **Identity Key Pair** — Ed25519, 32 B public / 64 B private. Живёт per-device навсегда.
2. Сгенерировать **Signed PreKey** — X25519, 32 B public / 32 B private. Подписать identity key'ом. `signed_prekey_id` = ротационный счётчик.
3. Сгенерировать **100 One-Time PreKeys** — X25519, 32 B каждая. У каждой уникальный `key_id`.
4. Сохранить private части в IndexedDB (`orbit-crypto.identity`, `orbit-crypto.signed_prekeys`, `orbit-crypto.one_time_prekeys`).
5. **Background upload** public частей на auth сервис:
   - `POST /keys/identity` → { identity_key, device_id }
   - `POST /keys/signed-prekey` → { signed_prekey, signature, signed_prekey_id }
   - `POST /keys/one-time-prekeys` → { keys: [{key_id, public_key}, ...] }
6. Failure не ломает auth flow — лог warning, пробуем в следующем session refresh.

### 3.2 Signed PreKey rotation

**Интервал:** раз в 7 дней.

**Триггер:** при auth refresh (раз в ~15 минут) — если `last_signed_prekey_rotation < 7 days ago`, сгенерировать новую Signed PreKey и upload'ить. Старая хранится ещё 48 часов для overlap (чтобы входящие сессии от peers, которые закешировали старый bundle, не ломались).

### 3.3 One-Time PreKey replenishment

**Триггер:** при auth refresh — если `fetchPreKeyCount()` < 20, сгенерировать и upload'ить новый batch из 100.

### 3.4 Session init (X3DH)

**Когда:** пользователь Alice впервые отправляет encrypted DM Bob'у (нет активной session записи для `bob_user_id:device_id`).

**Шаги:**
1. Alice: `fetchKeyBundle(bob_user_id)` → массив bundle'ов, по одному на каждый device Bob'а. Каждый bundle: `{identity_key, signed_prekey, signed_prekey_signature, signed_prekey_id, one_time_prekey}` (one-time prekey консьюмится атомарно на сервере — другой клиент не получит ту же самую).
2. Alice: для каждого device Bob'а прогоняет X3DH через libsignal → shared secret → инициализирует Double Ratchet state.
3. Alice: первый encrypted message становится `PreKeyWhisperMessage` (type=1) — контент включает enough info для Bob'а воссоздать session на своей стороне.
4. Bob: получает `PreKeyWhisperMessage`, libsignal автоматически создаёт reverse session из private материала у Bob'а.

### 3.5 Normal message exchange

**Double Ratchet:** каждое следующее сообщение использует новый derived key (symmetric + diffie-hellman ratchet). Forward secrecy + post-compromise security.

**Формат envelope** (из [SIGNAL_PROTOCOL.md:47](SIGNAL_PROTOCOL.md)):

```json
{
  "v": 1,
  "sender_device_id": "uuid",
  "devices": {
    "bob-device-1-uuid": { "type": 2, "body": "base64-ciphertext" },
    "bob-device-2-uuid": { "type": 2, "body": "base64-ciphertext" },
    "alice-device-2-uuid": { "type": 2, "body": "base64-ciphertext" }
  }
}
```

Включаем также другие устройства **самой Alice** чтобы её история была читаема на всех её девайсах.

---

## 4. Multi-device model

**Device identity:** `device_id = UUID per browser instance` (per auth session). Генерируется при первом enrollment, хранится в IndexedDB, биндится к JWT session.

**Persistence:** Identity Key live's в IndexedDB и переживает logout (чтобы при re-login тот же device_id не терял историю). Удаляется только при "Log out on all devices" или явной очистке browser storage.

**Fanout на отправке:** `MessageCrypto.encryptForChat()` возвращает envelope с entries для **всех** device'ов получателя (через `fetchKeyBundle`) плюс **всех СВОИХ** device'ов кроме текущего.

**Fanout на приёме:** backend публикует один envelope, все WS сессии (все devices) получают его, каждый находит свой `device_id` в `devices` map'е и decrypt'ит только свою entry.

**Изменение состава devices:** когда пользователь добавляет/удаляет device, peers узнают об этом лениво — в момент следующего send пересчитывают bundle. Нет broadcast'а "новое устройство", это upstream Signal Protocol design.

---

## 5. IndexedDB schema (клиентская)

**Database name:** `orbit-crypto`

**Object stores:**

| Store | Key | Value |
|-------|-----|-------|
| `identity` | `'self'` (constant) | `{ identityKeyPair: KeyPair, deviceId: UUID, createdAt: number }` |
| `signed_prekeys` | `signed_prekey_id INT` | `{ keyPair: KeyPair, signature: Uint8Array, createdAt: number }` |
| `one_time_prekeys` | `key_id INT` | `{ keyPair: KeyPair }` — удаляется после consumption |
| `sessions` | `{peer_user_id}:{peer_device_id}` | serialised Double Ratchet state (opaque blob от libsignal) |
| `verified` | `peer_user_id` | `{ identityKeyHash: string, verifiedAt: number }` |
| `message_cache` | `message_id` | `{ plaintext: string, decryptedAt: number }` — runtime-only, clear on logout |

**Versioning:** используем [idb](https://www.npmjs.com/package/idb) lib если уже в package.json, иначе тонкий ручной wrapper. Версия schema = 1, миграции делаем через upgrade callback.

---

## 6. Saturn API методы

Новый файл `web/src/api/saturn/methods/keys.ts`:

```
// Key upload (device enrollment)
uploadIdentityKey(identityKey: Uint8Array, deviceId: string): Promise<void>
uploadSignedPreKey(params: { signedPreKey, signature, signedPreKeyId, deviceId }): Promise<void>
uploadOneTimePreKeys(keys: Array<{keyId, publicKey}>, deviceId: string): Promise<void>

// Key lookup (session init)
fetchKeyBundle(userId: string): Promise<Array<{
  deviceId: string;
  identityKey: Uint8Array;
  signedPreKey: Uint8Array;
  signedPreKeySignature: Uint8Array;
  signedPreKeyId: number;
  oneTimePreKey?: { keyId: number; publicKey: Uint8Array };
}>>
fetchIdentityKey(userId: string): Promise<Uint8Array>  // для Safety Numbers

// Maintenance
fetchPreKeyCount(deviceId: string): Promise<number>  // для replenish triggering
fetchKeyTransparencyLog(userId?: string): Promise<LogEntry[]>  // compliance audit

// Encrypted message transport
sendEncryptedMessage(chatId: string, envelope: MessageEnvelope): Promise<void>

// Safety numbers — hash of identity keys, не server call
verifyIdentity(userId: string): Promise<string>  // возвращает 60-digit safety number

// Disappearing messages
setDisappearingTimer(chatId: string, seconds: number): Promise<void>
fetchDisappearingTimer(chatId: string): Promise<number>

// Multi-device management
fetchDevices(): Promise<Device[]>
deleteDevice(deviceId: string): Promise<void>
```

Зарегистрировать в [methods/index.ts](../web/src/api/saturn/methods/index.ts).

---

## 7. Send/Receive pipeline

### 7.1 Send

В `sendMessage` action'е на фронте (найти в `web/src/global/actions/api/messages.ts`):

```
if (chat.is_encrypted) {
  const peerDevices = await fetchKeyBundle(peer_user_id)
  const ownOtherDevices = await fetchKeyBundle(current_user_id)  // exclude self
  const envelope = await MessageCrypto.encryptForPeers(
    [...peerDevices, ...ownOtherDevices.filter(d => d.deviceId !== selfDeviceId)],
    plaintext
  )
  await sendEncryptedMessage(chatId, envelope)
} else {
  // existing non-E2E path
  await sendMessage({ chatId, text: plaintext })
}
```

Backend принимает `encrypted_content BYTEA` (колонка уже существует в таблице `messages`).

### 7.2 Receive

В WebSocket handler для `new_message` update:

```
if (message.is_encrypted) {
  const envelope = JSON.parse(message.encrypted_content)
  const entry = envelope.devices[selfDeviceId]
  if (!entry) {
    // No entry for us — maybe we're not in the envelope (sender filtered out).
    // Fall back to showing "[encrypted]" placeholder.
    return
  }
  const plaintext = await MessageCrypto.decryptMessage(envelope.sender_device_id, entry)
  // Store in runtime cache (IndexedDB message_cache store), NOT in global state
  await messageCrypto.cacheDecrypted(message.id, plaintext)
  // Dispatch to UI — global state holds placeholder "__E2E_DECRYPTED__",
  // UI renders from cache.
}
```

**Важно:** plaintext **никогда** не хранится в serialised global state (persisted в `orbit-global-state` IndexedDB). Только в runtime memory + short-lived `message_cache` store который чистится при logout.

---

## 8. Media encryption

**Deferred до Phase 7.1** — первая версия E2E только текстовые сообщения.

План (не в этом PR):
1. Клиент генерирует random AES-256-GCM ключ per-media
2. Шифрует байты перед `uploadMedia` call
3. Включает ключ в encrypted message envelope → получатели декодируют
4. R2 хранит encrypted blob, ключ сервер никогда не видит

---

## 9. Disappearing messages

**Backend cleanup cron:**
- Проверить существует ли worker в [services/messaging/internal/service/cleanup_service.go](../services/messaging/internal/service/cleanup_service.go)
- Если нет — реализовать tick раз в 30 секунд:
  ```sql
  DELETE FROM messages
  WHERE expires_at IS NOT NULL AND expires_at < NOW()
  RETURNING id, chat_id
  ```
- Для каждой удалённой публикуем NATS event `orbit.chat.{chat_id}.message.deleted`

**Send path:** при отправке сообщения в чат с `disappearing_timer > 0`, сервер автоматически ставит `expires_at = NOW() + timer * INTERVAL '1 second'`.

**Client-side:** runtime timer очищает decrypted plaintext из IndexedDB `message_cache` после истечения.

**UI:** в Chat settings новый dropdown "Disappearing messages: Off / 24h / 7d / 30d", вызывает `setDisappearingTimer()`. Badge в header "⏱ 24h" когда enabled.

---

## 10. Safety Numbers

**Алгоритм (Signal-compatible):**
1. Сортируем `user_id`'ы обеих сторон лексикографически → `(user_low, user_high)`
2. Строим input: `user_low_id || identity_key_low || user_high_id || identity_key_high`
3. SHA-256 → первые 30 байт
4. Каждые 5 байт → 12 digits (mod 10^12, padded zeros)
5. Результат: 5 групп по 12 цифр, итого 60 digits

**UI:** новый компонент `web/src/components/right/SafetyNumbersModal.tsx`, точка входа — кнопка "Verify Identity" в Profile (показывается только для E2E DM). Показывает:
- 60-digit number в 5 групп
- QR code (опционально — если в проекте есть QR lib; иначе только numbers)
- Кнопка "Verified" → сохраняет в `verified` store
- Badge "✓ Verified" в header чата когда verified

**Rekeying warning:** если identity key peer'а изменилась (новый девайс, reinstall) — показываем warning "Identity key changed", просим повторную верификацию.

---

## 11. Push preview suppression

В [services/gateway/internal](../services/gateway/internal) push handler: при формировании push payload'а для сообщения в чате с `is_encrypted=true`:
- `body = "Новое сообщение"` (или i18n "New message")
- Не включать `content`, `sender_name`
- Не логировать plaintext

Проверить grep'ом gateway логи на предмет утечки plaintext в error contexts.

---

## 12. Client-side search fallback

[services/messaging/internal/search/indexer.go](../services/messaging/internal/search/) — skip сообщения где `chat.is_encrypted = true`.

Новый клиент-сайд index:
- **Module:** `web/src/lib/search/client-index.ts`
- **Storage:** IndexedDB `search_index` object store, `{word: string → Array<{message_id, chat_id}>}`
- **Populate:** при decrypt каждого E2E сообщения на клиенте — tokenize plaintext (whitespace split + lowercase), добавлять в inverted index
- **Query:** при search в E2E чате — вместо gateway `/search` использовать локальный lookup. Простой substring match без morphology.

**Лимиты v1:**
- Только русский + английский
- Без stemming
- Только exact word match (не fuzzy)
- Persistent — индекс живёт дольше runtime cache, чтобы поиск работал после reload

---

## 13. Rollout

### Фаза 0: deploy code
1. PR merged, все сервисы задеплоены с `e2e_dm_enabled = false`.
2. Backend key endpoints доступны но unused.
3. Клиенты при auth refresh делают background key enrollment (silent, failure-tolerant). В логах auth service видим рост `user_keys` таблицы.

### Фаза 1: monitoring
4. Ждём **3-5 дней** чтобы >95% активных устройств сделали enrollment.
5. Мониторинг: grafana dashboard "E2E key coverage" — % online users с uploaded keys.
6. Если coverage низкое — investigate, не флипать флаг.

### Фаза 2: enable
7. `UPDATE feature_flags SET enabled=true WHERE key='e2e_dm_enabled';`
8. С этого момента **все новые DM** автоматически создаются как `is_encrypted=true`.
9. **Старые DM** остаются plaintext (не мигрируем — нет приватных ключей отправителей за прошлый период).

### Фаза 3: UX
10. В header нового DM — замок "🔒 End-to-end encrypted" badge.
11. Нет per-DM toggle в UI — детерминированное поведение "все новые DM = E2E после даты X".
12. **Fallback** если у получателя нет ключей (новый юзер, не успел auth'иться после rollout): warning "Получатель ещё не обновил ключи, подождите пока он зайдёт" + кнопка "Отправить незашифрованно" (создаёт non-E2E DM).

### Откат
- Флип `e2e_dm_enabled=false` → **новые** DM снова plaintext.
- Существующие E2E чаты остаются E2E (мы их серверно расшифровать не можем).
- Нет способа "разшифровать обратно" — только удалить.

---

## 14. Decisions (финальные, перед имплементацией)

Все решения приняты критически в контексте корпоративного мессенджера MST на 150+ пользователей, с приоритетом "детерминированность > гибкость" и "не обещай безопасности которой нет".

### 14.1 Media encryption — Phase 7.0 text-only, Phase 7.1 media (обязательно в течение 2 недель)

**Проблема:** если текст DM зашифрован, а PDF с HR-контрактом на R2 — в открытом виде, это ложь безопасности. Пользователь видит замочек, пересылает договор, админ R2 его читает.

**Решение:**
- **Phase 7.0 = только текст**
- В UI E2E DM явный disclaimer рядом с замочком: "Замок означает шифрование текста сообщений. Вложения хранятся на сервере отдельно."
- **Phase 7.1 = media encryption, обязательство запустить в течение 2 недель после 7.0.** Не откладываем "когда-нибудь".
- Если 7.1 не будет реализован — мы обязаны либо запустить 7.0+7.1 вместе, либо **вообще не делать E2E** и сказать честно "мы шифруем transport + at-rest, но не end-to-end". Половинчатая защита — антипаттерн.

**Phase 7.1 scope (кратко, для планирования):** per-file AES-256-GCM ключ, ключ в message envelope, клиент дешифрует blob после download из R2. Ломает server-side thumbnails — временно показываем placeholder для E2E медиа, потом добавляем client-side thumbnail generation.

### 14.2 QR code для Safety Numbers — Nice-to-have, skip в 7.0

60 digits читаются голосом за 30 секунд. 150 корп-юзеров созваниваются регулярно — могут зачитать вслух. Или встретились в офисе и показали экран друг другу.

**Решение:** 7.0 = только 60-digit number (5 групп по 12 цифр) + кнопка "Copy". QR — отдельная задача если пользователи попросят.

### 14.3 Crypto library strategy — custom X3DH+Double Ratchet on `@noble/*` primitives

**Ревизия от 2026-04-15 (session Phase 7 frontend):** оригинальное решение было использовать `@signalapp/libsignal-client` как "Rust→WASM". **Это оказалось ошибкой факта:** npm-пакет `@signalapp/libsignal-client` — это Node.js native addon через `node-gyp-build` (AGPL-3.0, 130 MB unpacked), а не wasm-сборка для браузера. Публичной wasm-версии в npm registry не существует — Signal Desktop собирает её из исходников собственным build pipeline.

**Рассмотренные альтернативы:**
1. `libsignal-protocol@1.3.15` — браузерный форк оригинального WhisperSystems/libsignal-protocol-javascript. Feb 2024 релиз, GPL-3.0, но тянет устаревший стек (`protobufjs@5.0.1`, `bytebuffer@3.5.5`, `long@3.1.0`, CommonJS).
2. `@privacyresearch/libsignal-protocol-typescript@0.0.16` — TS-переписывание оригинала, May 2023, выглядит заброшенно.
3. **Custom implementation на `@noble/*` primitives (выбрано).**

**Решение: custom Signal Protocol implementation поверх `@noble/curves` + `@noble/hashes` + `@noble/ciphers`**

- `@noble/curves` — Ed25519 + X25519 (нативно, нужные кривые), MIT, Paul Miller, audited, используется MetaMask/Ethers.js/Viem
- `@noble/hashes` — SHA-256 + HKDF, MIT
- `@noble/ciphers` — AES-256-GCM, MIT
- `idb` — тонкая обёртка над IndexedDB для key store, ISC, ~1 KB

Бэкенд envelope format opaque (BYTEA JSON), так что выбор конкретной библиотеки на клиенте не влияет на auth/messaging сервисы. Wire-совместимость с Signal protocol сохраняется через размеры ключей (Ed25519 32B pub / 64B priv, X25519 32B, Ed25519 signature 64B).

**Что это значит на практике:**
- Crypto layer — наш код, ~500 строк, audit-friendly (X3DH спек — 10 страниц, Double Ratchet спек — 25 страниц, оба публично доступны от Signal)
- Зависимостей немного, все maintained и small
- Bundle impact: ~50 KB gzipped для всех noble-пакетов, вместо 6.4 MB у libsignal-protocol
- `@noble/*` уже доступны в браузерах нативно — не требует wasm, не требует специального webpack experiment flag
- Round-trip + vector tests в CI обязательны как acceptance gate

**Load strategy (без изменений от первоначального замысла):**
- `@noble/*` импортируются лениво после auth success (dynamic `import()` → отдельный chunk)
- Web Worker для offload тяжёлых операций (batch генерация 100 one-time prekeys, session init)
- К моменту первого DM open — crypto layer готов
- Enrollment запускается как только chunk загрузился

Не на boot (экономим bandwidth на auth screen), не на первый E2E DM open (слишком поздно). **Между auth success и первым навигационным действием.**

**Audit strategy:** round-trip unit tests (Alice↔Bob), X3DH test vectors из Signal spec, ratchet step tests, safety numbers determinism. Для 150-user корп-мессенджера этого достаточно; при росте до внешних клиентов — security review внешним аудитором.

### 14.4 "Отправить незашифрованно" fallback — НЕТ. Блокируем с понятным сообщением

**Проблема:** silent downgrade UX opens door to compromise. Пользователь видит "⚠️ нет ключей, отправить незашифрованно?", жмёт "да" потому что "срочно", считает что всё хорошо (он же в "зашифрованном чате"), админ читает конфиденциал.

**Критический контекст:** в корп-окружении gap window "юзер создан в админке → юзер первый раз зашёл" = часы, максимум сутки. Это **не постоянное** состояние.

**Решение:**
- НЕТ кнопки "отправить незашифрованно"
- Блокируем send с сообщением: "Получатель [Имя] ещё не настроил ключи шифрования. Попросите его открыть приложение — после первого входа (обычно несколько секунд) сообщение можно будет отправить."
- Если отправитель считает что это критично — звонит по телефону, или ждёт 10 минут пока peer зайдёт
- Детерминированность > гибкости. Безопасность > удобства.

Исключение: можно создать **отдельный** не-E2E DM с тем же peer'ом (обычный нешифрованный чат), отправить туда. Это явное осознанное решение, не силент downgrade.

### 14.5 Rekeying warning — inline system message, НЕ modal

Identity key меняется: новый девайс (легитимно, частый случай), переустановка браузера (легитимно), attacker (редко но критично). Modal на каждую смену = раздражает при законных переустановках.

**Решение:**
- **Inline system message в чате:** "Ключи безопасности с [Имя] изменились. Нажмите чтобы проверить." — заметно, но не блокирует
- **Unobtrusive badge** "identity changed" на peer'е в header'е до первой ре-верификации через Safety Numbers modal
- Если пользователь проигнорировал inline message — badge остаётся navigational hint
- Если открыл Safety Numbers modal и повторно verified — inline message и badge снимаются

Не modal. Modal = disruptive для частого легитимного случая.

### 14.6 Client-side search — Persistent IndexedDB

**Ephemeral рассмотрение:** rebuild на каждом reload. 2000 сообщений × decrypt × tokenize = 5-10 секунд на reload. Неюзабельно.

**Persistent security analysis:** decrypted index в IndexedDB = plaintext копия на диске. Но атакующий с доступом к browser уже имеет доступ к `orbit-crypto.sessions` (Double Ratchet keys) → может расшифровать всю историю напрямую. Search index не добавляет attack surface material'но.

**Storage:** 150 users × 500 avg msgs × ~500 B index entry = ~40MB. Приемлемо для modern IndexedDB.

**Решение:** Persistent IndexedDB `search_index` store. Инвалидация при "Log out of all devices" — чистим crypto + search index atomic'но вместе.

### 14.7 CI стратегия — unit тесты для crypto, manual smoke для browser E2E

**Решение:**
- **В CI (на каждый PR):**
  - Unit tests для libsignal wrapper: Alice.encrypt → Bob.decrypt round-trip
  - Envelope format serialize/parse
  - Safety Number algorithm (SHA-256 → 60 digits)
  - Key serialization round-trip
  - Session state persistence
- **Вне CI (manual перед release/flag flip):**
  - Две вкладки в dev окружении, реальный messaging backend
  - Alice + Bob, создание DM, отправка, чтение, reload, rekeying
- **НЕ делаем** автоматизированные Playwright-based browser E2E тесты — непропорциональная сложность для 150-user корпоратива

### 14.8 `encrypted_content` storage — raw BYTEA с JSON-serialized envelope, server-opaque

Колонка уже существует как `BYTEA` в schema.

**Решение:**
- Храним envelope как raw bytes: `json.Marshal(envelope) → BYTEA`
- Server **не парсит** структуру — treat as opaque blob
- Envelope routing (который device_id дешифрует какую entry) — **исключительно клиентская** логика
- Можем менять envelope format (`v=1 → v=2`, добавлять поля, менять алгоритмы) без SQL migrations

Не структурированные колонки. Не JSONB. Raw bytes.

---

## 14.1 Implementation invariants (следствия из decisions)

Эти инварианты следует соблюдать при имплементации каждой секции:

1. **Замочек в UI = только текст защищён.** Рядом с замочком disclaimer. Без него — обманываем пользователя.
2. **libsignal load — background async после auth.** До первого user action успеет.
3. **Send блокируется при missing peer keys.** Никаких fallback'ов. Сообщение пользователю — понятное, на русском.
4. **System messages в E2E чате = inline**, modal только для Safety Numbers verification (user-initiated).
5. **Search index = persistent IndexedDB**, cleanup только при "Log out of all devices".
6. **CI gate = crypto unit tests PASS**. Без них не merge.
7. **Backend envelope = opaque BYTEA.** Messaging service никогда не импортирует envelope types.
8. **Media encryption = обязательство на Phase 7.1**, не "когда-нибудь".

---

## 15. Оценка объёма работ (пересмотрено после backend audit)

| Секция | Backend | Frontend | Дней |
|--------|---------|----------|------|
| 3.1 Проработка + design doc | — | — | DONE |
| 3.2 Backend audit key server | — | — | DONE (100% готов) |
| 3.3 Disappearing cron | — | — | DONE (worker running, auto expires_at) |
| 3.4 libsignal + IndexedDB foundation | 0 | весь | 2-3 |
| 3.5 Saturn methods keys.ts | 0 | весь | 0.5 |
| 3.6 Registration flow wiring | 0 | весь | 0.5 |
| 3.7 Send/Receive encrypted DM | 0 | весь | 2 |
| 3.8 Multi-device fanout | 0 | весь | 1 |
| 3.9 Safety Numbers UI | 0 | весь | 0.5 |
| 3.10 Disappearing messages UI | 0 | весь | 0.5 |
| 3.11 Push preview suppression | — | — | DONE |
| 3.12 Client-side search fallback | — | весь | 1 |
| 3.13 Rollout coordination + user-guide docs | — | 0.5 | 0.5 |
| **Итого осталось** | **0 дней** | **~8-9 дней** | **~8-9 дней** |

**Экономия vs первоначальная оценка (11-13 дней):** 3-4 дня за счёт того что backend audit показал полную готовность серверной части.

Не включено:
- Медиа encryption (Phase 7.1) — обязательно в течение 2 недель после 7.0
- Групповой E2E / Sender Keys (Phase 7.2 или никогда)
- QR code library (nice-to-have)
- Automated browser-level E2E tests — unit-тесты на crypto round-trip в CI, manual smoke для browser flow

---

## 16. Что НЕ делаем в Phase 7.0 (явно)

- **Escrow key flow** — deferred. `compliance_keys` таблица остаётся пустой.
- **Sealed Sender** — не реализуем. Sender_id в метаданных acceptable.
- **Медиа encryption** — Phase 7.1.
- **Групповой E2E** — Phase 7.2 (и возможно никогда, потому что ломает AI/search/bots/webhooks).
- **Retroactive migration** старых plaintext сообщений.
- **Per-DM opt-in toggle** в UI — детерминированное поведение через глобальный флаг.
- **Key backup в облако** (cross-device key sync) — Signal тоже этого не делает, user должен re-verify на новом девайсе.

---

## Аппрув

**Все 8 открытых вопросов закрыты** — см. §14 Decisions. Документ готов к реализации.

Порядок работ (из §15):
1. **3.2 Backend audit key server** — валидация что auth endpoints работают, тесты PASS, никаких бугов в атомарности one-time prekey consume
2. **3.3 Disappearing cron** — проверить наличие worker'а, реализовать если нет
3. **3.4 libsignal foundation** — отдельная сессия, contextually isolated, crypto-critical focus
4. → дальше по списку 3.5-3.13

**Phase 7.1 media encryption** — обязательно в течение 2 недель после merge'а Phase 7.0. Не откладываем.
