# Secrets Rotation Guide

Руководство по ротации секретов Orbit Messenger. Выполняется вручную при подозрении на компрометацию или по расписанию (рекомендуется раз в 90 дней).

---

## Секреты и их расположение

| Переменная | Сервис | Влияние ротации |
|---|---|---|
| `JWT_SECRET` | gateway, auth | Инвалидирует все активные сессии |
| `INTERNAL_SECRET` | gateway + все сервисы | Требует одновременного рестарта всех сервисов |
| `POSTGRES_PASSWORD` | postgres, все сервисы | Требует обновления DSN во всех сервисах |
| `REDIS_PASSWORD` | redis, gateway, auth, messaging | Требует рестарта всех сервисов |
| `NATS_TOKEN` | nats, gateway, messaging, media, bots, integrations | Требует рестарта всех сервисов |
| `ORBIT_MESSAGE_ENCRYPTION_KEY` | messaging | ⚠️ Требует ре-шифрования всей таблицы messages |
| `BACKUP_ENCRYPTION_PASSPHRASE` | scripts/backup-postgres.sh | Старые бэкапы остаются зашифрованы старым ключом |
| `MEILI_MASTER_KEY` / `MEILISEARCH_KEY` | meilisearch, messaging | Требует рестарта meilisearch + messaging |
| `VAPID_PRIVATE_KEY` | gateway | Инвалидирует все push-подписки браузеров |
| `R2_SECRET_ACCESS_KEY` | media | Ротируется в Cloudflare dashboard |
| `TURN_PASSWORD` / `TURN_SHARED_SECRET` | calls, coturn | Требует рестарта coturn + calls |
| `BOT_TOKEN_SECRET` | bots | Инвалидирует все bot tokens (нужен `POST /bots/:id/token/rotate`) |
| `ANTHROPIC_API_KEY` | ai | Ротируется в Anthropic console |
| `OPENAI_API_KEY` | ai | Ротируется в OpenAI console |
| `ALERTMANAGER_WEBHOOK_SECRET` | integrations, alertmanager | Обновить в Orbit Settings → Integrations |

---

## Процедуры ротации

### JWT_SECRET

Инвалидирует **все** активные сессии — пользователи будут разлогинены.

```bash
# 1. Сгенерировать новый секрет
openssl rand -hex 32

# 2. Обновить в Saturn.ac: Settings → Environment → JWT_SECRET
# 3. Перезапустить: gateway, auth
# 4. Пользователи получат 401 и будут перенаправлены на логин
```

### INTERNAL_SECRET

Используется для межсервисной аутентификации. Все сервисы должны получить новое значение **одновременно**.

```bash
# 1. Сгенерировать
openssl rand -hex 32

# 2. Обновить в Saturn.ac для ВСЕХ сервисов одновременно
# 3. Перезапустить все 8 сервисов одновременно (rolling restart не подходит)
```

### POSTGRES_PASSWORD

```bash
# 1. Сгенерировать
openssl rand -hex 24

# 2. Обновить пароль в PostgreSQL
psql -U orbit -c "ALTER USER orbit PASSWORD 'new_password';"

# 3. Обновить POSTGRES_PASSWORD в Saturn.ac
# 4. Перезапустить все сервисы с DB-подключением
```

### ORBIT_MESSAGE_ENCRYPTION_KEY

⚠️ **Критично**: потеря ключа = потеря всех сообщений. Перед ротацией обязательно сделать бэкап.

```bash
# 1. Сделать бэкап БД
./scripts/backup-postgres.sh

# 2. Сгенерировать новый ключ
openssl rand -hex 32

# 3. Запустить ре-шифрование (TODO: скрипт не реализован — требует разработки)
# Логика: SELECT все messages WHERE is_deleted=false,
#         расшифровать старым ключом, зашифровать новым, UPDATE

# 4. Обновить ORBIT_MESSAGE_ENCRYPTION_KEY в Saturn.ac
# 5. Перезапустить messaging service
```

> **TODO**: написать скрипт `scripts/reencrypt-messages.go` для ре-шифрования.

### VAPID_PRIVATE_KEY

Ротация инвалидирует все push-подписки — пользователи перестанут получать push до следующего визита в приложение.

```bash
# 1. Сгенерировать новую пару
cd services/gateway && go run ./cmd/generate-vapid

# 2. Обновить VAPID_PUBLIC_KEY и VAPID_PRIVATE_KEY в Saturn.ac
# 3. Перезапустить gateway
# 4. Пользователи автоматически переподпишутся при следующем открытии приложения
#    (Service Worker обнаружит изменение applicationServerKey)
```

### NATS_TOKEN

```bash
# 1. Сгенерировать
openssl rand -hex 32

# 2. Обновить NATS_TOKEN в Saturn.ac
# 3. Перезапустить: nats контейнер, затем gateway, messaging, media, bots, integrations
```

### BOT_TOKEN_SECRET

```bash
# 1. Сгенерировать
openssl rand -hex 32

# 2. Обновить BOT_TOKEN_SECRET в Saturn.ac
# 3. Перезапустить bots service
# 4. Ротировать токены всех ботов через API:
#    POST /bots/:id/token/rotate  (для каждого бота)
# 5. Уведомить владельцев ботов о новых токенах
```

### R2_SECRET_ACCESS_KEY

```bash
# 1. Cloudflare dashboard → R2 → API Tokens → Create new token
# 2. Обновить R2_SECRET_ACCESS_KEY и R2_ACCESS_KEY_ID в Saturn.ac
# 3. Перезапустить media service
# 4. Удалить старый токен в Cloudflare dashboard
```

### TURN_PASSWORD / TURN_SHARED_SECRET

```bash
# 1. Сгенерировать
openssl rand -hex 24  # для TURN_PASSWORD
openssl rand -hex 32  # для TURN_SHARED_SECRET

# 2. Обновить в Saturn.ac
# 3. Перезапустить: coturn, calls service
# 4. Активные звонки прервутся — предупредить пользователей
```

### Alertmanager webhook secret

```bash
# 1. Orbit Settings → Integrations → alertmanager connector → Rotate Secret
# 2. Скопировать новый secret
# 3. Обновить ALERTMANAGER_WEBHOOK_SECRET в Saturn.ac
# 4. Перезапустить alertmanager (или обновить alertmanager.yml)
```

---

## Расписание ротации

| Секрет | Рекомендуемый интервал | Приоритет |
|---|---|---|
| `JWT_SECRET` | 90 дней | Высокий |
| `INTERNAL_SECRET` | 180 дней | Средний |
| `POSTGRES_PASSWORD` | 90 дней | Высокий |
| `REDIS_PASSWORD` | 180 дней | Средний |
| `NATS_TOKEN` | 180 дней | Средний |
| `ORBIT_MESSAGE_ENCRYPTION_KEY` | При компрометации | Критичный |
| `VAPID_PRIVATE_KEY` | При компрометации | Средний |
| `R2_SECRET_ACCESS_KEY` | 90 дней | Высокий |
| `TURN_PASSWORD` | 180 дней | Низкий |
| `BOT_TOKEN_SECRET` | При компрометации | Средний |
| `ANTHROPIC_API_KEY` | При компрометации | Высокий |
| `OPENAI_API_KEY` | При компрометации | Высокий |
| `BACKUP_ENCRYPTION_PASSPHRASE` | 365 дней | Средний |

---

## Чеклист при подозрении на компрометацию

1. [ ] Определить какой секрет скомпрометирован
2. [ ] Сделать бэкап БД (`./scripts/backup-postgres.sh`)
3. [ ] Ротировать секрет по процедуре выше
4. [ ] Проверить логи на признаки несанкционированного доступа
5. [ ] Если `JWT_SECRET` — уведомить пользователей о принудительном разлогине
6. [ ] Если `ORBIT_MESSAGE_ENCRYPTION_KEY` — немедленно эскалировать (данные под угрозой)
7. [ ] Обновить запись в журнале инцидентов

---

## Генерация секретов

```bash
# 32 байта hex (для JWT_SECRET, INTERNAL_SECRET, NATS_TOKEN)
openssl rand -hex 32

# 24 байта hex (для паролей)
openssl rand -hex 24

# Base64 (для BACKUP_ENCRYPTION_PASSPHRASE)
openssl rand -base64 32

# VAPID ключи
cd services/gateway && go run ./cmd/generate-vapid
```
