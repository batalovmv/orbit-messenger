# План работ — pilot launch sprint

> Self-contained brief для следующего агента. Прочитай это **полностью**
> до первого touch-а в коде, потом `docs/canon/state.json` +
> `docs/canon/divergences.md`. Решения по архитектуре уже приняты —
> не переобсуждай, иди делать.
>
> Создано 2026-05-06 после двух сессий (2026-05-05 + 2026-05-06).
> Обновлено 2026-05-06 в третьей сессии: фазы A → C полностью закрыты,
> D1 (миграция 071) тоже. Коммиты локальные, **не запушены** — Saturn-side
> ресурсы (`backup-cron`, `nats-exporter`) пока не созданы.

---

## Статус на 2026-05-06 после третьей сессии

| Фаза | Состояние | Коммиты |
|---|---|---|
| A1 Smart Notifications C1-C5 | ✅ done | `6812a39` |
| A2 SFU 6-bug fix + e2e | ✅ done | `6bc6d39` |
| A3 Live Translate + p2p tweaks | ✅ done | `ec71937` |
| A4 Saturn infra (backup-cron + jetstream) | ✅ done locally, **не пушить до Saturn-кликов** | `f5407d5` |
| A5 OIDC backend MVP + ICE watchdog | ✅ done | `d44f4f2` |
| B2 `/auth/oidc/config` endpoint | ✅ done | `9c3ccb0` |
| B1 SSO кнопка на login screen | ✅ done | `856ac48` |
| B3 Local Dex profile (`oidc-dev`) | ✅ done | `1fe9c7e` |
| B4 OIDC sync worker + Google directory client | ✅ done | `560d5a3` |
| C1+C2 per-participant indicators + reconnect toast | ✅ done | `330285e` |
| C3 server-side `restart_ice` handler | ✅ done | `69e94fd` |
| D1 migration 071 (`call_recordings`) | ✅ done | `62d9de0` |
| **D2 Pion track recording** | ⏭ deferred — следующая сессия | — |
| **D3 Compliance плашка** | ⏭ deferred | — |
| **D4 Admin endpoint скачивания** | ⏭ deferred | — |
| **D5 GC ретеншен 90 дней** | ⏭ deferred | — |
| **D6 Storage budget alert** | ⏭ deferred | — |

Локальная проверка по каждой фазе сделана: `go test ./...` для auth/calls,
`tsc --noEmit` чистый, `docker compose --profile oidc-dev config` валиден,
миграция 071 применена в локальный postgres и схема совпадает с тем, что
ждёт `call_recordings`-FK у будущего D2 publisher'а.

**12 коммитов на master, не запушены.** Tree clean.

---

## TL;DR

- **150-юзер корпоративный пилот, счёт идёт на дни.**
- Compliance-модель: **админ читает чаты И слышит звонки** — оба тракта на
  бэкап. Юзеры это знают (плашка перед звонком, ToS в инвайт-флоу).
- **42 dirty файла локально, ничего не запушено.** До push нужны
  Saturn-side ресурсы (`backup-cron`, `nats-exporter`) — клики юзера, не
  агента.
- Работа разбита на фазы A→F, каждая — отдельный pushable unit.
  Делать строго в порядке. После каждой фазы — чек-поинт «прод не упал».

---

## Что готово на старте сессии

### Локально готово (uncommitted)

1. **Smart Notifications C1-C5** (предыдущая сессия) — backend+SW+UI
2. **SFU group calls 6-bug fix** + e2e спека (`tests/calls-e2e/sfu-3-call.spec.ts`)
3. **Live Translate i18n** — `*Other` ключи добавлены
4. **Saturn infra декларации:**
   - `.saturn.yml` декларирует `backup-cron`
   - `docker-compose.yml` добавляет `nats-exporter` (profile `monitoring`)
   - `monitoring/prometheus/rules/orbit.yml` — alert rules JetStream
   - 3 runbooks `docs/runbooks/saturn-{backup-cron-enablement,jetstream-prom,perf-smoke}.md`
   - **Backup-cron live-tested против FirstVDS S3** локально — full
     pg_dump → upload → download → decrypt → SQL verify roundtrip green
5. **OIDC SSO backend MVP** (сегодня) — ADR 006 + миграция 070 + сервис
   + handler + 10 unit-тестов с настоящей RS256/JWKS подписью. Routes
   404 без `OIDC_PROVIDER_KEY` env. Подробнее ниже в фазе B.
6. **SFU client-side ICE restart watchdog** — `web/src/lib/secret-sauce/sfu.ts`

### Backend test status

```
ok  services/auth/...        (OIDC новые + всё старое — 11s)
```

Остальные сервисы не трогали в этой сессии, но в начале были зелёные.

### Frontend

`tsc --noEmit` чистый. 28+ dirty файлов — не трогать, кроме как в рамках
фаз ниже.

---

## Saturn-side prerequisites (USER, не agent)

**До push любого PR с `.saturn.yml` изменениями (фаза A4) — сделай:**

### 1. Создать сервис `orbit-backup-cron` в Saturn UI

- Add Service → custom container
- Dockerfile path: `deploy/backup-cron/Dockerfile`
- No exposed ports
- Env vars (полный список — `docs/runbooks/saturn-backup-cron-enablement.md`):
  - `R2_ENDPOINT=https://s3.firstvds.ru`
  - `R2_ACCESS_KEY_ID` / `R2_SECRET_ACCESS_KEY` (FirstVDS, в 1Password)
  - `R2_BACKUP_BUCKET=orbit-postgres-backups`
  - `BACKUP_ENCRYPTION_PASSPHRASE` — `openssl rand -base64 48`,
    **сохранить в 1Password** — без неё бэкапы нерасшифровываемы
  - `DATABASE_URL` — копия с любого другого сервиса
  - `BACKUP_CRON=0 */4 * * *`

### 2. Создать сервис `orbit-nats-exporter` в Saturn UI

- Image: `natsio/prometheus-nats-exporter:0.17.3`
- Port: 7777
- Args: `-port=7777 -jsz=all -varz -connz -subz http://nats-js:8222`

### 3. После пилота — для фазы B

Зарегистрировать OAuth client в IdP (Google Workspace по умолчанию):

- Cloud Console → APIs & Services → Credentials → OAuth client ID
- Type: Web app
- Redirect URI: `https://new-tg-gwcikm.saturn.ac/api/v1/auth/oidc/google/callback`
- В Saturn env (`orbit-auth`):
  - `OIDC_PROVIDER_KEY=google`
  - `OIDC_ISSUER=https://accounts.google.com`
  - `OIDC_CLIENT_ID=...`
  - `OIDC_CLIENT_SECRET=...` (из 1Password)
  - `OIDC_REDIRECT_URL=...` (точно совпадает)
  - `OIDC_ALLOWED_EMAIL_DOMAINS=yourcompany.com`
  - `OIDC_FRONTEND_URL=https://new-tg-gwcikm.saturn.ac/`
- Restart `orbit-auth`. Лог должен показать
  `oidc: provider ready key=google issuer=...`

### 4. Для фазы B4 (sync worker)

Включить Google Workspace Directory API + создать service account с
domain-wide delegation на `https://www.googleapis.com/auth/admin.directory.user.readonly`.
Положить JSON ключ в Saturn env как `OIDC_SYNC_GOOGLE_SA_JSON`.

---

## Принятые решения (не переобсуждать)

Эти точки прошли пользовательский апрув в сессии 2026-05-06:

| # | Решение | Почему |
|---|---|---|
| 1 | **Запись звонков ДА, но только аудио, только SFU group, не P2P** | Storage budget. P2P не идёт через сервер — потребует MediaRecorder на клиенте. Видео → ~800GB/мес vs аудио ~50GB/мес. |
| 2 | **Retention записей звонков 90 дней** | Compliance baseline. Auto-GC через backup-cron. |
| 3 | **Compliance-плашка перед стартом звонка обязательна** | ФЗ-152 + GDPR требуют явного информирования участников даже при corp-модели. |
| 4 | **3c per-participant mute/share — Map в `useSfuStreamManager`** (не глобальный store) | UI-индикатор тайла, не нужен другим компонентам. Минимум кода. |
| 5 | **3b WS-reconnect — UX-плашка «переподключиться?»**, не auto-resume | Auto-resume — инженерное удовольствие. Пилот 150 юзеров переживёт ручной клик. |
| 6 | **3a server-side ICE restart — отложить за пилот** | Нет ICE-fail в логах = нет проблемы. Watchdog клиента уже стоит, на серверной стороне gap задокументирован. |
| 7 | **OIDC: один env-конфиг провайдер**, без admin UI и БД-таблицы | YAGNI пока нет второго заказчика. ADR 006 имеет explicit cut list. |
| 8 | **OIDC: silent linking by email** | Для корп-мессенджера IdP — source of truth. Если у атакующего есть Google-аккаунт жертвы в корп-домене, у нас проблемы похуже Orbit-логина. |
| 9 | **OIDC: F3 multi-provider DB-таблица — выкинуть из бэклога** | Не возвращаться пока не появится второй tenant с другим IdP. |
| 10 | **OIDC: `?access_token` в query**, не fragment | Прокси-логи под нашим контролем. Fragment ломается в HTTP redirect chain. |

---

## ФАЗА A — Commit hygiene (1 ч)

Превратить 42 dirty файла в логические PR'ы. **Каждый — отдельный
коммит с conventional message.**

| PR | Что | Files | Risk | Saturn-блок? |
|---|---|---|---|---|
| **A1** | Smart Notifications C1-C5 | `services/{messaging,gateway}/*` (изменения за вчера), `web/src/components/left/settings/SettingsSmartNotifications.tsx`, `web/src/components/middle/message/{ContextMenuContainer,MessageContextMenu}.tsx`, `web/src/serviceWorker/pushNotification.ts`, `Settings.scss`, `language.d.ts`, fallback strings | Low | Нет |
| **A2** | SFU 6-bug fix + 3-browser e2e | `services/calls/internal/{handler,service}/*`, `services/gateway/internal/ws/*`, `web/src/hooks/useSfuStreamManager.ts`, `web/src/api/saturn/{methods,updates}/calls.ts`, `web/src/components/calls/group/GroupCall.tsx`, `web/src/global/actions/ui/calls.ts`, `tests/calls-e2e/{sfu-3-call.spec.ts,seed-sfu-group.sql}` | Medium — критический путь звонков | Нет |
| **A3** | Live Translate i18n + p2p tweaks + остальной мусор | `web/src/lib/secret-sauce/p2p.ts`, fallback.strings, p2p-call.spec.ts, `services/messaging/internal/store/chat_store.go`, `services/messaging/internal/service/recording_publisher_test.go` | Low | Нет |
| **A4** | Saturn infra (backup-cron + jetstream metrics + runbooks) | `.saturn.yml`, `docker-compose.yml`, `monitoring/prometheus/rules/orbit.yml`, `docs/runbooks/saturn-{backup-cron-enablement,jetstream-prom,perf-smoke}.md` | Medium | **ДА — пушить только после Saturn-side кликов** |
| **A5** | OIDC SSO backend MVP + ICE watchdog + ADR 006 + mig 070 | `docs/canon/adr/006-oidc-sso.md`, `migrations/070_users_oidc_identity.sql`, `migrations/CHANGELOG.md`, `services/auth/{cmd,internal/handler,internal/service,internal/store}/*` (новые файлы и моки), `services/auth/go.{mod,sum}`, `web/src/lib/secret-sauce/sfu.ts`, `docs/canon/state.json` | Low — routes 404 без env | Нет |

### Правила коммитов

- Conventional commits на английском (`feat(auth): ...`, `fix(calls): ...`, `chore(infra): ...`)
- Никаких squash через rebase — отдельные коммиты, потом отдельные PR
- Перед push каждого PR прогнать `go test ./...` в задетых сервисах
- A1→A2→A3→A5 можно пушить **до** Saturn-кликов. A4 — только после.
- **Удалить `docs/NEXT-SESSION-PLAN-2026-05-06.md`** — он перекрывается этим файлом, путаница.

---

## ФАЗА B — SSO до конца (1 рабочий день)

Без этого OIDC backend из A5 — мёртвый камень.

### B1. FE-кнопка «Войти через {provider}» (~2-3 ч)

- На login screen добавить блок над email/password формой
- Кнопка conditioned на новый GET `/auth/oidc/config` (см. B2) — если
  `enabled=false`, не рендерить
- Click → `window.location = ${API}/auth/oidc/google/authorize?return_to=${encodeURIComponent(currentPath)}`
- После redirect'а с провайдера — на любой странице фронта первым делом
  читать `?access_token=` и `?expires_in=` из URL, передать в
  существующую token-management infra (см. `web/src/api/saturn/client.ts`),
  потом `history.replaceState` стирает params
- **Файлы:** `web/src/components/auth/AuthPhoneNumber.tsx` (~50 строк),
  `web/src/api/saturn/client.ts` (helper-функция absorb)
- Локализация: `OIDCSignInButton` → "Войти через {provider}" / "Sign in with {provider}"

### B2. Public OIDC config endpoint (~30 мин)

- `GET /auth/oidc/config` (no auth) → `{enabled: bool, providerKey: string, displayName: string}`
- 5 строк в `services/auth/internal/handler/oidc_handler.go`
- `displayName` из нового env `OIDC_PROVIDER_DISPLAY_NAME` (default
  capitalize providerKey: "google" → "Google")

### B3. Локальный Dex для smoke-тестов (~1.5-2 ч)

- В `docker-compose.yml` profile `oidc-dev` добавить сервис `dex` на
  основе `ghcr.io/dexidp/dex:v2.41.1`
- Config файл `deploy/dex/config.yaml`: static-passwords backend с
  `alice@orbit.local` / `LoadTest!2026`, single OIDC client с
  redirect-URL'ом на локальный auth (`http://localhost:8080/api/v1/auth/oidc/dex/callback`)
- README-абзац: `docker compose --profile oidc-dev up -d dex` →
  выставить `OIDC_*` env в auth → пройти flow в браузере
- Фактический smoke не входит в B3 — это для следующего, кто будет
  отлаживать продовую интеграцию

### B4. Sync worker для деактивации (~3-4 ч)

- Новый файл `services/auth/internal/service/oidc_sync.go` (~150 строк)
- Интерфейс `DirectoryClient { ListActiveSubjects(ctx) ([]string, error) }`
- Реализация `googleDirectoryClient` через `google.golang.org/api/admin/directory/v1`
- Горутина в `services/auth/cmd/main.go`: `time.NewTicker(1 * time.Hour)`,
  на тик — для каждого юзера с `oidc_provider IS NOT NULL AND is_active=true`
  проверить, есть ли subject в provider. Нет → `userSvc.Deactivate(uid)` +
  `s.sessions.DeleteAllByUser(uid)` + добавить ВСЕ jti в blacklist (см.
  memory `Day 5.2 session revoke` — gateway-кэш закрывается per-jti
  blacklist)
- Unit-тесты с mock'нутым DirectoryClient в `oidc_sync_test.go`
- Env: `OIDC_SYNC_ENABLED=true`, `OIDC_SYNC_INTERVAL=1h`,
  `OIDC_SYNC_GOOGLE_SA_JSON` (multiline JSON ключ service account)

### Готовность B

- Локально: `docker compose --profile oidc-dev up -d` → пройти OIDC-flow
  в браузере → юзер создан + добавлен в default chats + JWT в куках
- Существующий invite-юзер (test@orbit.local) логинится через Dex →
  привязка через email-match
- Ручной тест: удалить юзера в Dex (через config reload) → подождать
  тик → юзер в Orbit `is_active=false`, новый login отбит

### Push после B

- Разбить на 4 PR: B1, B2 — отдельно (мелкие FE+BE), B3 — отдельно
  (infra), B4 — отдельно (worker)

---

## ФАЗА C — Pilot-quality калибровка звонков (~3 ч суммарно)

### C1. Per-participant mute/screenshare UI (~1.5 ч)

- В `useSfuStreamManager.ts` добавить `participantStates: Map<userId, {muted: boolean, sharing: boolean}>`
  как `useState` или ref+forceUpdate
- Подписаться на apiUpdates (через `addCallback` или новый emitter) для
  событий `call_muted` / `call_unmuted` / `screen_share_*` — обновлять
  Map с правильным userId
- Заэкспортить из хука + протянуть в `GroupCall.tsx` тайлы
- В тайле participant'а отрисовать иконки `mic-off` / `screen-share-on`
- **Не лезть** в `wsHandler.ts:737` (там сейчас баг — broken P2P-shape).
  Просто перестать туда диспатчить для group calls; для P2P оставить
  как есть.
- ~1 файл FE + минимальный диспатчинг в хук

### C2. WS-reconnect UX-плашка (~1 ч)

- Использовать существующий `reconnectingWS` ивент (см.
  `web/src/api/saturn/client.ts` — там уже есть событие при
  reconnect). При активном звонке (`global.calls.activeCallId`) и
  `disconnect` event — toast «Связь потеряна. Нажмите чтобы
  переподключиться» с кнопкой
- При клике: `actions.leaveCall()` потом `actions.joinActiveCall(callId)`
- 1 файл FE, ~30 строк

### C3. (опционально) Server-side restart_ice handler

- В `services/calls/internal/handler/sfu_handler.go:134` добавить
  `case "restart_ice":` который вызывает `peer.RestartICE()` (Pion умеет)
  и триггерит свежий offer через существующий offer pump в
  `services/calls/internal/webrtc`
- Unit-тест в `services/calls/internal/webrtc/sfu_test.go`
- ~30-60 строк
- **Если время поджимает — отложить за пилот.** Watchdog клиента уже
  есть, без серверной части он просто no-op (server warn'ит «unknown
  signal event»).

### Push после C

- C1+C2 одним PR (FE only)
- C3 отдельным PR (Go only)

---

## ФАЗА D — Запись звонков (compliance, ~2 рабочих дня)

### D1. Миграция 071 (~30 мин)

```sql
CREATE TABLE call_recordings (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    call_id              uuid NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
    participant_user_id  uuid NOT NULL REFERENCES users(id),
    s3_key               text NOT NULL,
    encryption_key_id    text NOT NULL,  -- KMS key ref
    started_at           timestamptz NOT NULL,
    ended_at             timestamptz,
    duration_sec         int,
    size_bytes           bigint,
    created_at           timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_call_recordings_call ON call_recordings(call_id, started_at);
CREATE INDEX idx_call_recordings_retention ON call_recordings(ended_at) WHERE ended_at IS NOT NULL;
```

Только аудио → одна строка per participant per call. Не per track.

### D2. Pion track recording (~4-5 ч)

- В `services/calls/internal/webrtc/peer.go`: при `pc.OnTrack` для
  audio kind → создать `oggwriter.NewWith(...)` пишет в локальный
  `os.CreateTemp("", "orbit-rec-*.ogg")`
- Сохранить хендл в peer struct
- При `room.Close()` или `peer.Close()`:
  1. Закрыть oggwriter (flush)
  2. Шифровать через `pkg/crypto` (тот же AES-256-GCM что для медиа
     чатов) — генерируем per-recording key, key wrap'аем мастер-ключом
  3. Upload в MinIO/S3 через `pkg/storage`
  4. INSERT в `call_recordings` со всеми метаданными
  5. Удалить tmp файл
- **Important:** делать это в горутине после Close, чтобы не блокировать
  cleanup. Если upload упал — лог + flag в записи `upload_failed`,
  ретрай через cron не надо (потеря приемлема для compliance baseline)
- Тесты: модульный с фейковым track + storage mock

### D3. Compliance-плашка перед стартом звонка (~1.5 ч)

- При первом старте/принятии звонка показать модал:
  - Заголовок: «Звонки записываются»
  - Текст: «В этой компании звонки сохраняются для compliance согласно
    политике ИБ. Запись доступна администраторам по запросу. Продолжая,
    вы подтверждаете информированность.»
  - Чекбокс «Не показывать снова»
  - Кнопка «Продолжить» (disabled пока чекбокс не отмечен ИЛИ юзер
    впервые видит — тут продумать UX)
- Per-user flag в localStorage `orbit-call-recording-acknowledged-v1`
- Локализация: ключи `CallRecordingNoticeTitle`, `CallRecordingNoticeBody`,
  `CallRecordingNoticeAck`, `CallRecordingNoticeContinue`
- **Юридический crosscheck:** перед merge — попросить юзера показать
  текст юристу. ФЗ-152 формулировки в РФ строгие.

### D4. Admin endpoint для скачивания (~1.5 ч)

- `GET /admin/calls/{call_id}/recordings` → list `call_recordings` for
  this call с signed URL'ами (`pkg/storage.SignedDownloadURL`, TTL 1 час)
- Permission gate: `SysViewAuditLog` или новый `SysListenCallRecordings`
  (рекомендую новый — отделить право слушать от права читать audit)
- Аудит: каждый запрос signed URL пишется в `audit_log` action
  `call_recording.access` с `target_type=call_recording, target_id=recording.id`
- В Admin UI добавить таб «Записи звонков» рядом с «Audit Log» — список
  call_recordings с фильтром по call_id/participant/date

### D5. GC ретеншена 90 дней (~1 ч)

- В `deploy/backup-cron/scripts/` (или новом сервисе) добавить cron-job
  раз в день:
  ```bash
  KEYS=$(psql -t -c "DELETE FROM call_recordings WHERE ended_at < now() - interval '90 days' RETURNING s3_key;")
  echo "$KEYS" | xargs -I{} aws s3 rm "s3://${R2_BACKUP_BUCKET}/calls/{}"
  ```
- Альтернатива: bucket lifecycle rule + DELETE only DB. Проще, но БД и
  S3 расходятся при сбое — лучше явный workflow.
- Метрика `orbit_call_recordings_deleted_total` для мониторинга

### D6. Storage budget alert (~30 мин)

- Метрика `orbit_call_recordings_total_bytes` (sum size_bytes из БД,
  scrape раз в 5 мин)
- Prometheus alert `CallRecordingsStorageHigh` при > 100 GB → твой
  триггер посмотреть retention или вырубить запись
- Добавить в `monitoring/prometheus/rules/orbit.yml`

### Push после D

- D1 миграция — отдельным PR
- D2+D3 — feat PR (запись + плашка)
- D4 — отдельным PR (admin)
- D5+D6 — отдельным PR (infra GC)

---

## ФАЗА E — Pilot launch (твоя работа)

| Шаг | Кто | Артефакт |
|---|---|---|
| Saturn-side: enable OIDC env | Юзер | provider config |
| Manual smoke в проде: SSO login → создать чат → видео-звонок → проверить запись доступна в admin | Юзер | screenshot/notes |
| Импорт 150 юзеров (через первый OIDC-логин) | Юзер | список email'ов в IdP |
| Watch dashboards 24-48 ч | Юзер + агент при инцидентах | Saturn dashboards |

---

## ФАЗА F — Post-pilot deferred (НЕ ДЕЛАТЬ ДО ПИЛОТА)

Список того, что **намеренно отложено**:

- **3a server-side ICE restart handler** (фаза C3 опциональная — может
  переехать сюда)
- **Per-chat priority override UI** (`PUT /chats/{id}/notification-priority`
  бэкенд есть, фронт нет)
- **Firefox e2e** для звонков (`tests/calls-e2e/playwright.config.ts`)
- **AI Meeting Notes** (Wave 2 killer feature, отдельный sprint, нужен
  Whisper local или NVIDIA Parakeet для русского)
- **OIDC F3 multi-provider DB** (см. решение #9 в принятых)
- **SAML, magic-link, social-login**
- **Видео-recording звонков** (только аудио в MVP)
- **Selective recording** (несовместимо с compliance-моделью)
- **Поиск по содержимому записей** (нужна транскрипция + индекс)
- **PITR restore drill локально** (есть pg_dump 4h RPO, full WAL/PITR
  отложен — см. memory `WAL/PITR backlog`)

---

## Критический контекст

### Тест-юзеры (все пароль `LoadTest!2026`)

- `test@orbit.local` (alice, `3b4a280b-df0b-43e2-8fc3-629d33edb8c0`) — **superadmin**
- `user2@orbit.local` (bob, `e83bfcf7-9563-43d2-adb3-80a3aa0a4025`)
- `loadtest_0..149@orbit.local`

⚠ В предыдущих брифах alice/bob ID были перепутаны — DB-snapshot выше
канонический.

### Ключевые ID

- **Default-for-new-users чат** (mig 069): `997c7fcb-2075-47df-97bb-5dd15dc07d55` (`Orbit First Run`)
- **SFU тест-группа** (e2e): `cccccccc-3333-4444-5555-666666666666` (test/user2/loadtest_0)

### Локальный стек

- `docker compose up -d` → 17 контейнеров healthy
- Web: http://localhost:3000 (production build, nginx)
- Auth direct: http://localhost:8081
- Messaging direct: http://localhost:8082
- Gateway: http://localhost:8080 (всё проксируется через `/api/v1/...`)

### Прод

- URL: **https://new-tg-gwcikm.saturn.ac/** (НЕ orbit-messenger.saturn.ac
  из старых доков)
- Saturn проект: https://saturn.ac/projects/u040wk444w0cosc8sgss0s4o
- Auto-deploy по `git push origin main`

### Где жить вещам

| Тема | Путь |
|---|---|
| Smart Notifications | `services/{messaging,gateway,ai}/internal/...`, `web/src/components/left/settings/SettingsSmartNotifications.tsx`, `web/src/serviceWorker/pushNotification.ts` |
| SFU | `services/calls/internal/{handler,service,webrtc}/`, `web/src/hooks/useSfuStreamManager.ts`, `web/src/lib/secret-sauce/sfu.ts` |
| OIDC | `services/auth/internal/{handler/oidc_handler.go,service/oidc.go}`, `docs/canon/adr/006-oidc-sso.md` |
| Saturn infra | `.saturn.yml`, `docker-compose.yml`, `monitoring/`, `docs/runbooks/saturn-*.md` |
| E2E | `tests/calls-e2e/*` (Playwright, отдельный package, **не в `web/`**) |

### Команды

```bash
# Запустить всё
docker compose up -d

# Migration
docker exec -i orbit-postgres-1 psql -U orbit -d orbit < migrations/NNN_*.sql

# Тесты сервиса
cd services/<name> && go test ./... -count=1

# Frontend type check
cd web && npx tsc --noEmit

# E2E call test (требует docker compose up)
cd tests/calls-e2e && npx playwright test sfu-3-call.spec.ts
```

### Деп-ограничения

- `services/auth` go.mod на **1.24** (НЕ 1.25 — convention запрещает)
- `coreos/go-oidc/v3 v3.11.0` пиннуто (latest 3.18 требует Go 1.25)
- `golang.org/x/oauth2 v0.30.0` пиннуто по той же причине
- Не делать `go get -u` на этих двух без re-check go directive

---

## Открытые feature gaps (для контекста, не для немедленной работы)

Найдены в сессии 2026-05-06, **зафиксированы в коде комментариями**:

1. **`services/calls/internal/handler/sfu_handler.go:134`** — switch
   только на `answer`/`candidate`. Клиент шлёт `restart_ice` (см.
   `web/src/lib/secret-sauce/sfu.ts`) — сервер дропает с warn.
   **Закрывается фазой C3.**

2. **`web/src/api/saturn/updates/wsHandler.ts:737-799`** —
   `handleCallMuteChanged` / `handleScreenShareChanged` мапит на
   `updatePhoneCallPeerState` (P2P shape). В group call это перезаписывает
   индикатор всех participant'ов одним. **Закрывается фазой C1** (но
   через local map в хуке, не через global state).

3. **SFU room resume при WS-разрыве** — клиент дропается из room без
   автоматического возврата. **Закрывается фазой C2** (UX-плашка, не
   auto-resume).

---

## Suggested first move (для следующей сессии — фокус на D2-D6)

1. `git log --oneline -15` → убедиться, что 12 локальных коммитов A→D1
   на месте (последний — `62d9de0 feat(db): migration 071`).
2. `git status -s` → должно быть пусто.
3. Прочитать `docs/canon/state.json` (last_migration=071) + этот файл +
   `docs/canon/divergences.md`.
4. Спросить юзера:
   - **«Saturn-side ресурсы (`backup-cron` + `nats-exporter`) уже создал?»**
     Если ДА → можно `git push` всех 12 коммитов разом. Если НЕТ → пушить
     до A4 (`f5407d5`) включительно НЕЛЬЗЯ; для не-A4 коммитов проще
     дождаться кликов и запушить всё одной волной.
   - **«Старт пилота когда?»** Если в течение 2 рабочих дней — фаза D
     обязательна (D2-D6), параллельно юзер делает Saturn-side и
     юридический crosscheck текста D3-плашки.
   - **«Готов IdP-конфиг (Google Workspace OAuth client + Directory API
     SA)?»** — без этого фазу B нельзя проверить в проде.
5. Дальше — фаза D2-D6 строго в порядке плана выше. D2 — самая объёмная
   и хрупкая (Pion + crypto + S3 + tmp-файлы), её **не** делегировать
   sonnet-агенту целиком: сначала сам пройди по `peer.go` и
   `pkg/crypto`/`pkg/storage`, чтобы понять, какие callback'и Pion
   реально умеет. D3/D4/D5/D6 после D2 — гораздо более механические,
   их можно отдать sonnet'у пакетом.

**Не пушить ничего до подтверждения юзера, что Saturn-side готов.**

## Заметки для D2-D6

- **D2 риски:** Pion `pc.OnTrack` для audio kind зовётся ОДИН раз на
  трек, но трек может остановиться/перезапуститься при `restart_ice` —
  oggwriter надо умело закрывать-переоткрывать. Проще: писать в
  отдельный файл per `OnTrack` invocation, а на Close агрегировать.
  Encryption: можно reuse `pkg/crypto.SealAESGCM` (уже использовалось
  для медиа чатов в Phase 4); per-recording random key, key wrap
  через мастер.
- **D3 юридика:** текст из плана — это плейсхолдер. **ОБЯЗАТЕЛЬНО**
  показать юристу до merge. ФЗ-152 (Россия) и GDPR (если есть EU
  юзеры) формулировки строгие. Per-user ack persisted в localStorage
  под ключом `orbit-call-recording-acknowledged-v1` — если меняется
  текст, версию надо бампать (`-v2`) чтобы все юзеры пере-подтвердили.
- **D4 IDOR:** не забыть проверку, что `call_id` в URL действительно
  принадлежит compliance-роли, а не юзеру (сейчас `SysViewAuditLog`
  гейт уже стоит — просто не забыть его пересмотреть для нового
  permission `SysListenCallRecordings` если решишь его создать).
- **D5/D6:** D5-крон жить может в `deploy/backup-cron/scripts/`
  (рядом с pg_dump уже там). Метрика D6 `orbit_call_recordings_total_bytes`
  лучше всего скрапиться из БД (sum size_bytes), не из S3 — S3 listing
  стоит денег и асинхронен.

## Фаза E

См. оригинальную секцию выше — план не поменялся. После пуша всех
коммитов и Saturn-side подготовки запускается фаза E (юзерская).
