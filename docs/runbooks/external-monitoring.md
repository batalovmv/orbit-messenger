# External Monitoring Runbook — UptimeRobot

> **Цель**: настроить внешний мониторинг доступности Orbit Messenger через UptimeRobot (бесплатный tier поддерживает до 50 мониторов с интервалом 5 минут; Pro — до 1 минуты)

---

## Что мониторить

| Endpoint | Метод | Ожидаемый ответ | Интервал |
|----------|-------|-----------------|----------|
| `https://orbit.mst.com/health` | GET | HTTP 200, тело `{"status":"ok"}` | 30s (Pro) / 5min (Free) |
| `https://orbit.mst.com/api/auth/health` | GET | HTTP 200 | 30s |
| `https://orbit.mst.com/api/messaging/health` | GET | HTTP 200 | 30s |
| `https://orbit.mst.com/api/media/health` | GET | HTTP 200 | 30s |

---

## Настройка UptimeRobot

### 1. Создать аккаунт и получить API key

1. Зарегистрироваться на [uptimerobot.com](https://uptimerobot.com)
2. Settings → API Settings → Create Main API Key
3. Сохранить ключ в `.env` как `UPTIMEROBOT_API_KEY=...`

### 2. Добавить монитор через UI

1. Dashboard → **Add New Monitor**
2. Заполнить поля:

```
Monitor Type:     HTTP(s)
Friendly Name:    Orbit Gateway Health
URL:              https://orbit.mst.com/health
Monitoring Interval: 5 minutes (Free) / 30 seconds (Pro)
HTTP Method:      GET
```

3. **Advanced Settings**:
   - Keyword: `"ok"` (проверить наличие в теле ответа)
   - Timeout: 30 seconds

4. Нажать **Create Monitor**

### 3. Настроить Alert Contacts

1. My Settings → **Alert Contacts** → Add Alert Contact
2. Типы уведомлений:
   - **Email**: devops@mst.com (или личный)
   - **Telegram**: через BotFather → `/newbot` → токен + chat_id в UptimeRobot
   - **Slack**: Incoming Webhook URL

3. Добавить контакты к монитору: Edit Monitor → Alert Contacts → выбрать нужные

### 4. Повторить для каждого сервиса

Аналогично создать мониторы для `/api/auth/health`, `/api/messaging/health`, `/api/media/health`.

---

## Настройка через API (автоматизация)

```bash
# Создать монитор через API
curl -X POST "https://api.uptimerobot.com/v2/newMonitor" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "api_key=${UPTIMEROBOT_API_KEY}" \
  -d "friendly_name=Orbit Gateway Health" \
  -d "url=https://orbit.mst.com/health" \
  -d "type=1" \
  -d "interval=300"

# Получить список мониторов
curl -X POST "https://api.uptimerobot.com/v2/getMonitors" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "api_key=${UPTIMEROBOT_API_KEY}" \
  -d "format=json"
```

---

## Публичная Status Page

1. Dashboard → **Status Pages** → Add Status Page
2. Название: `Orbit Messenger Status`
3. Добавить все мониторы
4. Опубликовать по адресу вида `https://status.uptimerobot.com/...` или настроить кастомный домен `status.mst.com`

---

## Алерты и эскалация

| Событие | Кто получает | Канал |
|---------|-------------|-------|
| Сайт упал | devops@mst.com | Email + Telegram |
| Сайт не восстановился 15 мин | Весь техотдел | Telegram канал #alerts |
| 3+ сервиса вниз | CTO | SMS (если настроено) |

Настрой порог в UptimeRobot: **Alert when down for ≥ 2 consecutive checks** — чтобы избежать ложных срабатываний на временные сбои сети.

---

## Проверка после настройки

```bash
# Убедиться что health endpoint отвечает корректно
curl -s https://orbit.mst.com/health | jq .
# Ожидаемый ответ: {"status":"ok","service":"gateway"}
```
