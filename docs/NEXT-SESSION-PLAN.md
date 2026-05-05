# План работ — следующая сессия

> Создан 2026-05-05 как контекст для перезапуска работы. Кратко: что
> сделано в предыдущей сессии, что осталось, в каком порядке делать,
> какие решения уже приняты и не нужно переобсуждать.

---

## Что сделано в предыдущей сессии (2026-05-04)

Пилот-блокеры закрыты на ~80%. 16 коммитов на main, все запушено и
автодеплоится Saturn'ом. Полный список в `git log --oneline 9683828..HEAD`,
ключевое:

- Compliance-панель сделана responsive (desktop / tablet / mobile)
- Очистка репо от smoke-артефактов + .gitignore-правила
- INTERNAL_SECRET ротирован и выровнен по всем сервисам
- NATS заменён на новый сервис `nats-js` с включённым JetStream (24h
  replay, persistence, dedup) — без token-auth, потому что Saturn
  Connection не умеет встраивать токен в auto-генерируемый URL
- Kод научился читать `NATS_JS_URL` (Saturn) с fallback на
  `ORBIT_NATS_URL` (локальный dev)
- `TRUSTED_PROXIES` выставлен на gateway → rate-limit per real IP
- Helper `httputil.ClientIP()` отрезает X-Forwarded-For chain → auth
  больше не падает с SQLSTATE 22P02 на postgres `inet`-колонке
- postgres-exporter + Prom alerts (`WalArchiveStalled`,
  `WalArchiveFailures`, `CallPushFailureRate`)
- Push metric с label `type` (message/call/read_sync/admin_test)
- Локально: postgres-wrapper.sh с auto-archive + walg-cron, NATS
  Dockerfile, calls e2e Playwright (Chromium), k6 WS load 150 VU
- Live Translate prod-чек: backend готов, фронт wired (миграции 056/058)

WAL/PITR через wal-g явно отказался: Saturn-managed Postgres не даёт
shell, wal-g неприменим. Ostаются Saturn-managed daily backup +
проверенный pg_dump в R2 (через personal S3, см. ниже).

---

## TODO immediate (~1-2 часа на Saturn UI + ручной QA)

### 1. Подтвердить что логин на проде восстановлен
Вчерашний последний фикс (`49e921c` — strip X-Forwarded-For chain)
должен был починить "Server is unavailable" на login. Шаги:

1. Открыть https://orbit-messenger.saturn.ac/
2. Залогиниться superadmin'ом
3. Если "Server is unavailable" остался — open Saturn → Logs → auth →
   filter Errors → пришли скрин в чат
4. Если залогинился ок → переходим к пункту 2

### 2. Welcome backfill для пострадавших юзеров
Пока INTERNAL_SECRET был сломан (~26 апр – 4 мая), новые invited
юзеры регистрировались но не попадали в default-чаты. Нужно их
backfill'ить:

1. Меню (бургер) → **Administration**
2. Вкладка **Welcome**
3. Найти кнопку backfill → клик → confirm
4. Ответ типа `inserted: N` — N это сколько membership'ов добавилось

### 3. Stop старого `orbit-nats`
После того как nats-js работает >24h без проблем:

1. Architecture → orbit-nats (старый, не nats-js)
2. Stop / Disable / Delete (твоё решение — Stop безопаснее, можно
   откатиться при проблеме)

### 4. Cross-browser calls smoke (~30 мин)
Chromium у нас уже автоматизирован в `tests/calls-e2e/`. Manual нужен
для остальных по `docs/runbooks/cross-browser-call-test.md`:
- Firefox stable (must)
- Safari macOS (must)
- iOS Safari (must — самая высокая вероятность поломки)
- Edge (optional)
- Chrome Android (must)

Каждый ~10 мин. Записать что не работает.

### 5. Live Translate prod check (~10 мин)
По `docs/8d-qa-checklist.md` пункт 8D.4. UI strings RU,
auto-translate, manual translate, settings save.

---

## TODO эта неделя — оставшиеся пилот-блокеры

### 6. R2 backup на персональный S3 (~30 мин)
**Решение принято**: использовать твой личный S3 (не Cloudflare R2 и
не отдельный backup-cron сервис на Saturn). Saturn UI поддерживает
S3 с regional endpoint'ом (без custom endpoint поля).

Шаги:
1. На твоём S3-провайдере (AWS / Yandex Object Storage / etc) создай
   bucket `orbit-postgres-backups` (имя любое)
2. Создай IAM user / API key с правами Object Read/Write **только** на
   этот bucket
3. Запиши Access Key ID + Secret Access Key + Region + Bucket name
4. Saturn → orbit-postgres → Backups → Settings → S3 Storage Settings
5. Чекни "Store backups in S3"
6. Заполни поля:
   - S3 Bucket Name: `orbit-postgres-backups`
   - S3 Region: твой реальный регион (например `eu-west-1` или
     `ru-central1`)
   - S3 Access Key ID: твой
   - S3 Secret Access Key: твой
7. **Test Connection** → должно пройти
8. Save Settings
9. **Create Backup** вручную чтобы проверить что бэкап реально
   доезжает до S3
10. Зайди на S3 в bucket и убедись что объект появился

Если Test Connection упадёт — пришли error message, разберёмся.

### 7. Performance smoke на Saturn (~2 часа)
Локально мы прогнали k6 на 150 WS-connections и получили p95 connect
8ms, 0 disconnects. Нужно повторить против Saturn чтобы убедиться
что прод-цифры близки.

Шаги:
1. Получить продовый JWT — на Saturn запусти `tests/load/mint-tokens.go`
   с продовыми DATABASE_URL и JWT_SECRET (или экспортируй из postgres
   pre-existing tokens)
2. Запусти k6 локально с `BASE_URL=wss://gateway.saturn.ac/api/v1/ws`
   (или whatever real URL)
3. Сравни p95: connect, auth-ack, disconnects
4. Если хуже локального — найти боттлнек

Артефакт: `audits/load-2026-05-XX-saturn.md`.

### 8. JetStream алерты в Saturn Prom (~1 час)
- Добавить scrape config на `nats-js:8222/jsz` в Saturn managed Prom
- Алерт `JetStreamConsumerLag` (consumer.delivered.consumer_seq отстаёт >10s)

---

## Wave 2 killer features (после пилота)

В порядке приоритета:

### Smart Notifications (~10 дней)
Приоритет 1 после пилот-фидбека. Push+AI инфра уже готова. Нужен
дедикейтед спринт. Не пытаться втиснуть между ops.

### AI Meeting Notes (~17 дней)
После Smart Notifications. Зависит от стабильности SFU group calls
(см. параллельный трек ниже).

### Workflow Automations (~15 дней)
После Meeting Notes.

---

## Параллельные крупные треки

### SSO / корпоративный вход (~12 часов)
**#2 из вчерашнего бэклога**. Сейчас invite-codes не масштабируется
на 150 человек. Нужно:

1. Решение по провайдеру (требует твоего выбора):
   - Yandex SSO (если корпоративная подписка Yandex 360)
   - Google OIDC (общий вариант)
   - Custom OAuth via Keycloak / Authentik (self-hosted — больше работы)
   - Microsoft Entra (если Microsoft 365 в компании)
2. После выбора — реализация standard OIDC flow в `services/auth`:
   - Endpoint `/auth/oidc/login` → redirect на провайдера
   - Callback `/auth/oidc/callback` → обмен code на токен → создаём
     orbit user если ещё нет → выдаём JWT
   - Конфиг переменные: `OIDC_ISSUER_URL`, `OIDC_CLIENT_ID`,
     `OIDC_CLIENT_SECRET`, `OIDC_REDIRECT_URL`
3. Frontend: кнопка "Sign in with X" на login screen рядом с invite-code
4. Existing users / migration: invite-code остаётся как fallback

**Когда делать**: до того как пилот вырастет за 50 юзеров.
Сначала пилот на invite-codes, потом SSO для масштабирования.

### Стабилизация SFU group calls 3+ участников (~3-5 дней)
**#5 из вчерашнего бэклога**. Сейчас 1-on-1 P2P работает (наш
автотест зелёный). Группы 3+ бывают глючные.

Шаги:
1. Расширить `tests/calls-e2e/p2p-call.spec.ts` до 3-browser scenario:
   alice → bob accepts → carol joins → все три видят друг друга →
   carol leaves → cleanup
2. Воспроизвести локально с fake-media (`--use-fake-device-for-media-stream`)
3. Если падает — debug `services/calls/internal/webrtc/sfu.go`
4. Fix + extend e2e

---

## Технический долг (low priority)

- Удалить две `delete` ноды на Saturn (по запросу пользователя — не
  трогать без явного разрешения)
- Saturn FR: добавить custom S3 endpoint в Backup Settings (для
  поддержки R2 / MinIO без AWS-style regional endpoints)
- Финализировать `tests/calls-e2e/` README — добавить step-by-step
  для Firefox/Safari после авто
- Прогнать `pitr-restore.md` drill один раз на staging чтобы проверить
  что local infrastructure (postgres-wrapper.sh + walg-cron) правильно
  восстанавливается из R2

---

## Принципы для следующей сессии

1. **Сначала пилот-блокеры, потом фичи.** Killer features ждут пилот-фидбека
2. **Не размазывать killer features.** Smart Notifications — отдельный
   спринт целиком, не пытаться сделать "пол-фичи" между ops
3. **Ops-задачи параллельно**, когда можно: R2 backup, JetStream alerts,
   perf smoke не требуют моего code-time
4. **Auto-test only для критичных браузеров.** Chromium auto, Firefox/
   Safari iOS — manual smoke. Edge / Chrome Android — skip для пилота
5. **Решения о провайдерах — твои.** Я предлагаю архитектуру, ты
   выбираешь Yandex / Google / etc
6. **Перед prod-операциями** — обязательная локальная репро (вчера
   спасла от 401 inet-bug, NATS auth bug, X-Forwarded-For chain bug)

---

## Ближайший конкретный шаг следующей сессии

1. Прочитать этот файл
2. Подтвердить что login на orbit-messenger работает (если ещё не
   проверено)
3. Welcome backfill (10 секунд клика)
4. R2 backup на личный S3 (30 минут — весь шаг 6 из этого плана)
5. Решить с пользователем приоритет: cross-browser smoke vs SSO vs
   killer features

После этого работа разбивается на дискретные задачи, каждая ~1-3
часа, можно идти по очереди.
