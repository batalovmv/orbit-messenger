# Saturn.ac Capabilities — research snapshot

Date: 2026-04-21
Sources checked:
- https://saturn.ac — **403 Forbidden** (сайт за авторизацией или закрыт)
- https://docs.saturn.ac — **503 Service Unavailable** (не существует или лежит)
- https://www.saturn.ac — **503 Service Unavailable**
- Exa web search `site:saturn.ac` — **0 результатов** (сайт не индексируется поисковиками)
- Exa web search `Saturn.ac PaaS` — **0 релевантных результатов** (все результаты про Saturn Cloud — другой продукт)
- GitHub orgs `saturn-ac`, `saturn-platform` — **не найдены**
- Кодовая база Orbit (docker-compose.yml, docs/mst-integrations.md, AGENTS.md) — **единственный реальный источник**

> **IMPORTANT**: Saturn Cloud (saturncloud.io) — это MLOps-платформа на Kubernetes, **не имеющая отношения** к Saturn.ac. Все результаты поиска вели на saturncloud.io и были отброшены как нерелевантные.

---

## 1. Persistent volumes

**Verdict**: unclear — documented: нет, источников не найдено

**What we know from codebase**:
- Saturn.ac предоставляет managed PostgreSQL и Redis (env vars `DATABASE_URL`, `REDIS_URL` в docker-compose.yml)
- Комментарий в `docker-compose.yml:328`: *"Saturn uses its own managed stack"* — подразумевает что Saturn имеет свои внутренние сервисы, но не уточняет persistent volumes для пользовательских контейнеров
- Нет `.saturn.yml` или аналогичного манифеста в репозитории — механизм деплоя через `git push origin main` без декларативной конфигурации volumes

**How**: неизвестно
**Limits**: неизвестно
**ReadWriteMany**: неизвестно

**Sources**: только `docker-compose.yml:328` из кодовой базы

---

## 2. Staging environment

**Verdict**: unclear — documented: нет, источников не найдено

**What we know from codebase**:
- Деплой триггерится через `git push origin main` (AGENTS.md:12)
- Saturn.ac имеет dashboard с управлением env vars (docker-compose.yml:283: *"dropped into Saturn.ac dashboard. No rebuild needed to swap them in"*)
- Saturn.ac поддерживает webhooks с событиями `deploy.started`, `deploy.succeeded`, `deploy.failed` (docs/mst-integrations.md:52-55)
- Нет упоминаний preview environments, staging branches, или pipeline концепций

**How**: неизвестно — возможно, отдельный проект в Saturn с другой веткой, но это спекуляция
**Cost**: неизвестно

**Sources**: `AGENTS.md:12`, `docs/mst-integrations.md:36-69`

---

## 3. Healthcheck

**Verdict**: unclear — documented: нет, источников не найдено

**What we know from codebase**:
- В AGENTS.md (ранее CLAUDE.md) упоминается *"blue-green deploy → health check → live"*, но без деталей реализации на стороне Saturn
- Все Go-сервисы Orbit имеют эндпоинты `/health/live` и `/health/ready` (стандартная практика проекта)
- Неизвестно, различает ли Saturn liveness vs readiness probes

**How**: неизвестно (HTTP probe? TCP? exec?)
**Timeout/interval/retries**: неизвестно
**Failure behavior**: неизвестно (rollback? alert? nothing?)
**Blue-green/rolling**: упоминается в документации проекта, но конфигурация на стороне Saturn неизвестна

**Sources**: `AGENTS.md` (упоминание blue-green), кодовая база сервисов (health endpoints)

---

## 4. Observability

**Verdict**: partially known

**What we know from codebase**:
- `docker-compose.yml:328-334`: *"Prometheus scraper — local dev only. Saturn uses its own managed stack. Pulls metrics from every service's /metrics endpoint."*
- Это подтверждает что Saturn **имеет свой Prometheus**, который скрейпит `/metrics` эндпоинты сервисов
- Сервисы Orbit гейтят `/metrics` за `INTERNAL_SECRET` через header `X-Internal-Token`
- Неизвестно: есть ли у Saturn Grafana dashboards, log aggregation, доступ к метрикам через API

**Metrics**: Saturn имеет managed Prometheus stack (подтверждено комментарием в docker-compose.yml)
**Log aggregation**: неизвестно
**API access**: неизвестно

**Sources**: `docker-compose.yml:328-334`

---

## Recommendations

### Prometheus + Grafana
**Рекомендация**: уточнить у Saturn.ac support что именно входит в их managed stack.
- Если Saturn уже скрейпит `/metrics` и предоставляет Grafana — использовать его (zero cost, zero ops)
- Если Saturn только скрейпит но не даёт UI/API доступ — **Grafana Cloud Free tier** (10k metrics, 50GB logs, 14d retention) как overlay
- Если Saturn managed stack минимален — **отдельный VPS €4-8/мес** с Prometheus + Grafana (persistent storage гарантирован)

### Staging
**Рекомендация**: бюджетный Plan Б — **VPS €4/мес с docker compose**.
- Saturn staging возможности неизвестны, ждать ответа от support
- VPS даёт полный контроль: restore-тесты бэкапов, pre-prod миграции, smoke-тесты
- Можно поднять за 2 часа на базе существующего `docker-compose.yml`

### Healthcheck
**Рекомендация**: продолжать реализацию `/health/ready` и `/health/live` в сервисах (Phase 8D) — это стандарт и будет работать на любой платформе. Уточнить у Saturn конфигурацию probes.

---

## Open questions

Все 4 вопроса остаются **открытыми** из-за отсутствия публичной документации Saturn.ac.

### Как добить

1. **Saturn.ac support ticket** (приоритет #1):
   - Есть ли persistent volumes для пользовательских контейнеров?
   - Есть ли staging/preview environments?
   - Как сконфигурированы healthcheck probes (HTTP/TCP, timeout, retries)?
   - Что входит в managed Prometheus stack? Есть ли Grafana? API доступ к метрикам?
   - Есть ли log aggregation?

2. **Saturn.ac dashboard** — залогиниться и проверить UI на наличие:
   - Volume management section
   - Environment/staging switcher
   - Healthcheck configuration
   - Monitoring/logs tabs

3. **Saturn.ac CLI** (если есть) — `saturn --help`, проверить доступные команды

4. **Прямой контакт с командой Saturn** — email, Discord, или форма обратной связи на dashboard

### Что блокируется без ответов

| Вопрос | Блокирует |
|--------|-----------|
| Persistent volumes | Решение по Prometheus: Saturn native vs Grafana Cloud vs VPS |
| Staging | Pre-prod тестирование миграций и бэкапов перед пилотом MST |
| Healthcheck config | Настройка blue-green deploy, rollback strategy |
| Observability API | Федерация метрик, кастомные dashboards, alerting |
