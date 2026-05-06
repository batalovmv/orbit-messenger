# Total Local QA — 2026-05-06

Финальный pre-push прогон всего, что накоплено в 18 локальных коммитах
(`8a81d35`..`879aea6`). Цель — поймать регрессии **до** того, как Saturn
auto-deploy подхватит и пилотные юзеры увидят. Каждый пункт self-contained:
команда, что считается PASS, что считается FAIL.

**Контекст для исполнителя:**
- Repo: `D:\job\orbit`. Branch: `master`. 18 unpushed commits, tree clean.
- Локальный стек поднимается через `docker compose up -d`.
- Прод URL (для справки, НЕ трогаем в этом QA): `https://new-tg-gwcikm.saturn.ac/`.
- Тест-юзеры (пароль `LoadTest!2026`):
  - `test@orbit.local` (alice, `3b4a280b-df0b-43e2-8fc3-629d33edb8c0`) — superadmin
  - `user2@orbit.local` (bob, `e83bfcf7-9563-43d2-adb3-80a3aa0a4025`)
  - `loadtest_0..149@orbit.local`
- Default-for-new-users чат (mig 069): `997c7fcb-2075-47df-97bb-5dd15dc07d55`
- SFU тест-группа (e2e): `cccccccc-3333-4444-5555-666666666666`
- Если на каком-то шаге FAIL — стоп, фиксируй точное сообщение/симптом, дальше
  не идти. Не пытаться "обходить".

---

## 0. Подготовка окружения

```bash
cd /d/job/orbit
git status -s                  # должно быть пусто
git log --oneline -20          # должны видеть commits 8a81d35..879aea6
docker compose ps              # 17 контейнеров healthy
```

**PASS:** tree clean, 18 commits на master, все контейнеры healthy.
**FAIL:** дёрнуть `docker compose up -d --build` если контейнеров нет; если
после этого не healthy — собрать `docker compose logs <service>` и зафиксировать.

---

## 1. Backend — Go tests (все сервисы)

**Цель:** ни один Go-сервис не сломан. Параллельно для скорости.

```bash
cd /d/job/orbit
for svc in auth messaging gateway calls media bots ai integrations; do
  echo "=== $svc ==="
  (cd services/$svc && go test ./... -count=1 2>&1 | tail -8)
done
```

**PASS:** каждый сервис заканчивается `ok` строкой; никаких `FAIL`.
**FAIL:** записать service + первый FAIL-блок целиком, не идти дальше до
понимания причины. Особое внимание:
- `services/auth` — там OIDC (mig 070) + sync worker. 4 теста OIDCSync_* должны
  быть зелёными.
- `services/calls` — TestPeer_RestartICE_ReturnsErrorOnFreshPC должен PASS.
- `services/messaging` — тесты NATS publisher (smart notifications priority).

---

## 2. Frontend — tsc + production build

```bash
cd /d/job/orbit/web
npx tsc --noEmit 2>&1 | tail -5
VAPID_PUBLIC_KEY=test npx webpack --mode production 2>&1 | tail -15
```

**PASS:**
- tsc — никакого вывода (success).
- webpack — `compiled with 2 warnings` (только entrypoint/asset size warnings,
  это норма для большого SPA).
- Артефакты:
  - `dist/serviceWorker.js` существует (~15 KB)
  - `dist/index.html` НЕ содержит `<script src="serviceWorker">`
  - `dist/site.webmanifest` содержит `"prefer_related_applications": false`

```bash
ls -la dist/serviceWorker.js
grep -c 'serviceWorker' dist/index.html  # ожидаемо 0
grep prefer_related_applications dist/site.webmanifest
```

**FAIL:** TS ошибка → читать stack, чинить. Если webpack падает с
`Module not found` — возможно, кеш сломался, попробовать
`rm -rf node_modules/.cache dist && npx webpack --mode production`.

---

## 3. Миграции — применяемость с нуля

**Цель:** проверить, что свежий postgres проходит ВСЕ миграции 001-070
без ошибок.

```bash
docker compose down -v postgres   # дропает volume
docker compose up -d postgres
sleep 10                           # дать инициализироваться
docker compose logs postgres 2>&1 | grep -iE "error|fatal" | head
```

**PASS:** В логах postgres нет `ERROR` / `FATAL`, только
`database system is ready to accept connections`.

```bash
docker exec orbit-postgres-1 psql -U orbit -d orbit -c \
  "SELECT filename FROM schema_migrations ORDER BY filename DESC LIMIT 3;"
```

**PASS:** последняя — `070_users_oidc_identity.sql`. **Нет** 071
(мы откатили — это правильно).

```bash
# Поднять остальной стек обратно
docker compose up -d
sleep 20
docker compose ps | grep -v healthy | head     # все должны быть healthy
```

**FAIL:** Миграция падает → собрать sql + ошибку, проверить, что pgcrypto
расширение есть (мы используем `gen_random_uuid()`).

---

## 4. Docker compose — все сервисы поднялись

```bash
docker compose ps
docker compose logs --tail=30 gateway auth messaging calls 2>&1 | grep -iE "error|fatal|panic" | head -20
```

**PASS:**
- 17 контейнеров healthy (postgres, redis, nats, meilisearch, minio, coturn,
  clamav, gateway, auth, messaging, media, calls, ai, bots, integrations,
  web, backup-cron).
- В логах никаких `panic`, `fatal`, разве что cosmetic warnings.

**Особое внимание:**
- `auth` логи должны содержать `database connected successfully` и
  `auth service started port=8081`.
- `gateway` — `gateway service started`.
- НЕ должно быть `oidc: provider discovery failed` (мы не выставляем OIDC
  env в локальном compose; провайдер просто не активируется, но routes
  всё ещё /auth/oidc/config отдают enabled:false).

---

## 5. Health endpoints + /auth/oidc/config

```bash
curl -sS http://localhost:8080/health 2>&1                                  # gateway
curl -sS http://localhost:8081/health 2>&1                                  # auth direct
curl -sS http://localhost:8080/api/v1/auth/oidc/config | jq .               # через gateway
```

**PASS:**
- `/health` отдаёт `{"status":"ok",...}` на оба порта.
- `/auth/oidc/config` отдаёт `{"enabled":false,"providerKey":"","displayName":""}`
  (без OIDC env в compose это норма).

**FAIL:** 502/503 от gateway → сервис не поднялся. 404 на `/auth/oidc/config`
→ роут не зарегистрирован, пересобрать auth контейнер.

---

## 6. PWA — артефакты + browser smoke

### 6.1 Build artifacts (быстро через curl)

Запустить standalone http-server против `web/dist`:
```bash
cd /d/job/orbit/web
npx http-server dist -p 3199 -c-1 --cors &
sleep 2
curl -sI http://localhost:3199/serviceWorker.js | head -3
curl -s  http://localhost:3199/site.webmanifest | jq '.prefer_related_applications, .name, .display'
curl -s  http://localhost:3199/version.txt
```

**PASS:**
- `serviceWorker.js` → `HTTP/1.1 200 OK`
- manifest: `false`, `"Orbit Messenger"`, `"standalone"`
- version.txt → одна строка типа `12.0.20`

```bash
# Cleanup
taskkill //F //IM node.exe 2>&1 | head -3   # или вручную найти PID 3199
```

### 6.2 Browser smoke (через docker'овский web-контейнер)

⚠ **Важно:** docker compose web контейнер пересобирается из исходников.
Если изменения webpack.config.ts ещё не подхвачены — пересобрать:
```bash
docker compose up -d --build web
sleep 20
```

Открыть `http://localhost:3000/` в Chromium (Chrome/Edge/Brave).

**Smoke шаги:**
1. **Login**: `test@orbit.local` / `LoadTest!2026` → должен залогиниться без
   permission prompt'а на нотификации.
   **PASS:** попадание в чат-список без всплывающих диалогов.
2. **DevTools → Application → Manifest:** должен отрисоваться превью с
   иконкой и `Orbit Messenger`. Никаких warnings.
3. **DevTools → Application → Service Workers:**
   - Status: `activated and is running`
   - Source: `serviceWorker.js` (БЕЗ хеша!)
   - Только ОДНА регистрация.
4. **DevTools → Lighthouse → PWA category:** запустить аудит. Score >= 90.
   Не должно быть failed checks "Manifest doesn't have a maskable icon",
   "Web app manifest meets the installability requirements".
5. **Install prompt:** в адресной строке Chrome должна появиться иконка
   "Install" или прямо в меню «Установить Orbit Messenger».
6. **Offline check:** DevTools → Network → Offline. F5 страницы. Должна
   загрузиться (из SW cache), показать "Connection lost" баннер.
7. **Update banner check:**
   - На сервере поменять `web/public/version.txt` → `12.0.99`
   - Пересобрать `docker compose up -d --build web`
   - В браузере подождать до 5 минут (или дёрнуть `actions.checkAppVersion()`
     через консоль). Должен появиться баннер «Новая версия» с кнопкой
     «Обновить».

---

## 7. Чаты — base happy path

В двух браузерах (или Chrome + incognito):
- Окно 1: `test@orbit.local` / `LoadTest!2026`
- Окно 2: `user2@orbit.local` / `LoadTest!2026`

Шаги:
1. Из окна 1 написать сообщение в DM с user2.
2. В окне 2 — сообщение должно прийти WS-real-time (без F5).
3. В окне 2 ответить.
4. В окне 1 — увидеть ответ.
5. Закрыть окно 2. В окне 1 удалить сообщение. Открыть окно 2 → сообщение
   должно исчезнуть (sync на reconnect).

**PASS:** все шаги проходят, real-time без перезагрузки.
**FAIL:** WS не подключается → проверить, что gateway на 8080 healthy и нет
ошибок в `docker compose logs gateway` про WS.

---

## 8. Smart Notifications — feature smoke

В окне `test@orbit.local`:

1. **Settings → Notifications → Smart Notifications**: включить Smart mode.
   **PASS:** dropdown переключается, настройки per-priority видны.
2. **Per-priority behavior:** убрать "Show banner" для Low. Ничего не должно
   падать.
3. **Right-click on a message → "Suggest priority":** появляется submenu с
   четырьмя вариантами (Urgent/Important/Normal/Low).
   **PASS:** клик по любому варианту → toast "Feedback sent" или подобное.
4. **Settings → Notifications → Smart Notifications → Recent classifications:**
   список (может быть пустой если приоритеты ещё не классифицировались).
   **PASS:** не падает с error при открытии.

---

## 9. Звонки — P2P

В двух окнах:
1. Из окна 1 (test) → DM с user2 → иконка телефона → начать **аудио** звонок.
2. В окне 2 (user2) — должен появиться incoming-call modal.
3. Принять звонок.
4. Проверить:
   - Звук с обеих сторон (mute/unmute).
   - Видео (включить камеру с обеих сторон).
   - Hangup из любого окна → оба окна возвращаются в чат.

**PASS:** звонок устанавливается, обе стороны слышат и видят, hangup чистый.
**FAIL:** "Connection failed" → проверить coturn:
```bash
docker compose logs coturn | tail -20
```
Также проверить, что TURN_USER/TURN_PASSWORD есть в `.env`.

---

## 10. Звонки — SFU group + индикаторы (C1) + reconnect (C2)

В трёх окнах:
- Окно 1: test
- Окно 2: user2
- Окно 3: loadtest_0

1. Все трое заходят в группу `cccccccc-3333-4444-5555-666666666666`.
2. Окно 1 → стартует **видео-звонок** в группу.
3. В окнах 2 и 3 — иконка зелёного телефона "Join call".
4. Все трое в звонке. Каждый видит других в тайлах.
5. **C1 mute indicator:** в окне 2 нажать mute. В окнах 1 и 3 на тайле user2
   должен появиться mic-off иконка. PASS если значок возле user2, а не возле
   всех или возле случайного юзера.
6. **C1 screen-share indicator:** в окне 3 нажать screen-share. В окнах 1 и
   2 на тайле loadtest_0 — иконка share-screen.
7. **C2 reconnect toast:** в DevTools окна 1 → Network → Offline на 10 секунд
   → Online. Должен появиться toast «Connection lost. Tap to reconnect»
   с кнопкой. Клик → возвращаемся в звонок без перезапуска.
8. Hangup из всех окон.

**PASS:** все индикаторы на правильных тайлах, reconnect работает по клику.
**FAIL:** индикатор "залипает" на одном тайле или появляется на всех — это
старый bug C1, должен быть починен; проверить `groupCallParticipantState.ts`
emit'ы.

---

## 11. Звонки — restart_ice (C3)

Проверка серверной части ICE restart. Это сложно отресовать без двух
физических устройств — поэтому делаем минимальный sanity:

1. Старт SFU группового звонка как в шаге 10.
2. В DevTools одного окна → Application → Service Workers (нет ничего)
   → консоль:
   ```js
   // Найти активную SFU сессию и форсировать ICE restart
   // (через клиент-side watchdog)
   ```
   Альтернатива: просто сделать Network → Offline на 30 секунд → Online.
   Клиентский ICE watchdog должен послать `restart_ice` на сервер.
3. В `docker compose logs calls`:
   ```bash
   docker compose logs calls 2>&1 | grep -i "restart_ice\|sfu:" | tail -10
   ```
   **PASS:** видим обработку restart_ice, нет `unknown signal event` после
   нашего merge.

**FAIL:** видим "unknown signal event: restart_ice" → C3 не задеплоился, пересобрать calls контейнер `docker compose up -d --build calls`.

---

## 12. OIDC SSO — local Dex E2E

⚠ Этот шаг требует Dex profile (часть B3 коммита).

```bash
docker compose --profile oidc-dev up -d dex
sleep 5
curl -sS http://localhost:5556/dex/.well-known/openid-configuration | jq .issuer
```

**PASS:** issuer == `http://dex:5556/dex` (через docker network) ИЛИ
`http://localhost:5556/dex` (по необходимости — зависит от env).

Затем перезапустить `auth` с OIDC env:

```bash
# Вариант: prepend env-вары в docker-compose.override.yml для auth, либо
# ручной запуск:
docker exec -e OIDC_PROVIDER_KEY=dex \
           -e OIDC_PROVIDER_DISPLAY_NAME='Dex (local)' \
           -e OIDC_ISSUER=http://dex:5556/dex \
           -e OIDC_CLIENT_ID=orbit-local \
           -e OIDC_CLIENT_SECRET=local-dev-secret \
           -e OIDC_REDIRECT_URL=http://localhost:8080/api/v1/auth/oidc/dex/callback \
           -e OIDC_FRONTEND_URL=http://localhost:3000/ \
           -e OIDC_ALLOWED_EMAIL_DOMAINS=orbit.local \
           orbit-auth-1 /app/auth
```

Проще: добавить эти env во временный override и перезапустить:
```yaml
# docker-compose.override.yml (gitignore'd)
services:
  auth:
    environment:
      OIDC_PROVIDER_KEY: dex
      OIDC_PROVIDER_DISPLAY_NAME: "Dex (local)"
      OIDC_ISSUER: http://dex:5556/dex
      OIDC_CLIENT_ID: orbit-local
      OIDC_CLIENT_SECRET: local-dev-secret
      OIDC_REDIRECT_URL: http://localhost:8080/api/v1/auth/oidc/dex/callback
      OIDC_FRONTEND_URL: http://localhost:3000/
      OIDC_ALLOWED_EMAIL_DOMAINS: orbit.local
```
```bash
docker compose up -d auth
sleep 5
docker compose logs auth | grep "oidc: provider ready"
```

**PASS:** в логах строка `oidc: provider ready key=dex issuer=http://dex:5556/dex`.

Затем:
```bash
curl -sS http://localhost:8080/api/v1/auth/oidc/config | jq .
```

**PASS:** `{"enabled":true,"providerKey":"dex","displayName":"Dex (local)"}`.

Затем в браузере на `http://localhost:3000/` (logged out):
1. На login screen должна появиться **полноширинная кнопка** "Sign in with Dex (local)"
   над email/password формой.
2. Клик → редирект на Dex login page.
3. Login: `alice@orbit.local` / `LoadTest!2026`.
4. Должен вернуть в `http://localhost:3000/` уже залогиненным.
5. URL очищен от `?access_token=` (history.replaceState).
6. В логах auth: `oidc: linked existing user user_id=3b4a280b-...` (т.к. alice уже
   существует в БД).

**PASS:** flow проходит без ошибок, alice залогинена.
**FAIL:** "OIDC nonce mismatch" → state expired (Redis перезапускался?).
"Unknown or expired OIDC state" → то же. Просто повторить flow.
"Account is already linked to a different SSO identity" → если alice уже была
slinkована раньше с другим subject; решается ручным `UPDATE users SET
oidc_subject=NULL WHERE email='test@orbit.local'`.

**Cleanup:** удалить override, перезапустить auth.

---

## 13. OIDC sync worker (B4) — unit-тесты

Sync worker нельзя реально потестить без Google Workspace API. Полагаемся
на 4 unit-теста:

```bash
cd /d/job/orbit/services/auth
go test ./internal/service/ -run 'OIDCSync' -count=1 -v 2>&1 | tail -10
```

**PASS:** все 4 теста PASS:
- TestOIDCSync_DeactivatesUserMissingFromDirectory
- TestOIDCSync_NoOpWhenAllPresent
- TestOIDCSync_DirectoryErrorIsConservative
- TestOIDCSync_LoadConfig

---

## 14. Безопасность — quick regressions

```bash
# 1. /metrics требует X-Internal-Token
curl -sS http://localhost:8081/metrics | head -5
# PASS: {"error":"unauthorized"}, status 401

# 2. /api/v1/auth/me без токена
curl -sS http://localhost:8080/api/v1/auth/me
# PASS: 401 / "Missing authorization"

# 3. /api/v1/admin/users требует admin
# (предполагая, что есть токен member-юзера — пропустить если нет)

# 4. SQL injection sanity (NOT a real injection test, just doesn't crash)
curl -sS "http://localhost:8080/api/v1/auth/oidc/google'%20OR%201=1--/authorize"
# PASS: 404 (provider key parse не должен пускать дальше)
```

---

## 15. Push-нотификации — local pipeline

Если в `.env` есть VAPID_PUBLIC_KEY и VAPID_PRIVATE_KEY:

1. В окне `test@orbit.local`: Settings → Notifications → toggle "Web notifications".
2. Браузер запросит permission — **PASS** если запрос появился ТОЛЬКО после
   клика по чекбоксу (не на открытии Settings — это P1 fix).
3. Allow permission.
4. В окне 2 (user2) → послать сообщение в DM с alice.
5. Окно 1 — push-уведомление должно прийти (даже если окно в фоне).

**PASS:** push приходит. **FAIL:** проверить
```bash
docker compose logs gateway | grep -i "push\|vapid" | tail
```
Если VAPID env пустой — это not-a-blocker для local QA, помечаем как skipped.

---

## 16. Финальный регрессионный seal

```bash
# Tree должен быть чистый
git status -s            # пусто
git log --oneline -20    # 20 commits, последний 879aea6

# Никаких temp файлов / leftover overrides
ls docker-compose.override.yml 2>/dev/null && echo "REMOVE THIS BEFORE PUSH"
```

**PASS:** tree clean, нет временных override'ов, 18 unpushed commits на месте.

---

## Сводная таблица результатов

Заполнить во время прогона:

| # | Категория | PASS / FAIL | Заметки |
|---|---|---|---|
| 1 | Backend Go tests | | |
| 2 | Frontend tsc + build | | |
| 3 | Migrations from scratch | | |
| 4 | Docker compose health | | |
| 5 | Health endpoints | | |
| 6.1 | PWA artifacts (curl) | | |
| 6.2 | PWA browser (Lighthouse) | | |
| 7 | Chats happy path | | |
| 8 | Smart Notifications | | |
| 9 | P2P call | | |
| 10 | SFU group + indicators + reconnect | | |
| 11 | restart_ice handler | | |
| 12 | OIDC E2E with Dex | | |
| 13 | OIDC sync worker tests | | |
| 14 | Security regressions | | |
| 15 | Push notifications | | |
| 16 | Tree clean | | |

**Решение по push'у:**
- 0 FAIL → push можно. → `git push origin master`
- 1+ FAIL в категории "регрессия" (1, 2, 4, 5, 7, 9, 10) → НЕ пушить, фиксить.
- 1+ FAIL в "новых фичах" (8, 12, 13, 15) — обсудить, можно ли пилотить
  без этой фичи (может, B/Smart/Push выключим env'ом и поедем).
- FAIL в 6.x (PWA) — критично для пилота, пушить только после фикса.

---

## Что НЕ проверяем в этом прогоне

- iOS Safari install / push (нужен живой iPhone)
- Android Chrome install (нужен живой Android)
- Firefox e2e (deferred per phase F)
- Tauri desktop (отдельный pipeline)
- Прод S3 / FirstVDS (Saturn-side, не локально)
- Whisper / AI transcription (Wave 2, deferred)
- WAL/PITR restore drill (см. memory `WAL/PITR backlog`)
