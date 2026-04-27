# Brief for OpenCode — Sprint 0: Saturn capabilities research

## Context

Orbit Messenger — корпоративный мессенджер на Go-микросервисах + TypeScript/Teact frontend, deployed to **Saturn.ac** (self-hosted PaaS, auto-deploy on `git push origin main`). Сейчас мы в Phase 8D (Production Hardening) и готовимся к пилоту MST (150+ сотрудников). Перед тем как строить observability и staging, нужно понять что Saturn реально умеет — это чёрный ящик с минимальной документацией, и мы не хотим потратить 2-3 дня на решение, которое развалится из-за платформенных ограничений.

GPL-3.0-or-later вопрос по форку Telegram Web A уже закрыт отдельно (web/LICENSE, web/package.json:6) — в scope этого research не входит.

## Задача

Research only. **Не писать никакого кода, не трогать репозиторий orbit/**, только собрать факты и оформить отчёт.

## 4 вопроса, на которые нужны однозначные ответы

### 1. Persistent volumes
- Поддерживает ли Saturn.ac постоянные диски для произвольных контейнеров (не только managed PostgreSQL/Redis)?
- Если да — как они монтируются (декларативно в манифесте, через dashboard UI, через CLI)?
- Лимиты по размеру, backup policy, шифрование at-rest?
- Можно ли примонтировать один volume к двум контейнерам (ReadWriteMany)?

**Почему важно**: собираемся ставить Prometheus + Grafana в прод. Без persistent volume метрики уходят при каждом redeploy — бессмысленно. Альтернатива — Grafana Cloud (платный, но с retention из коробки).

### 2. Staging environment
- Есть ли у Saturn встроенная концепция staging / preview environments (как Vercel preview deploys или Heroku pipelines)?
- Если нет — какая рекомендуемая практика? Отдельный проект в Saturn с тем же репо и другой веткой? Отдельный VPS?
- Стоимость staging'а: Saturn считает это одной подпиской или отдельной?

**Почему важно**: нужно место где гонять restore-тесты бэкапов, pre-prod smoke-тесты, миграции БД перед прод. Если Saturn staging нет — бюджетный план Б: €4/мес VPS с docker compose.

### 3. Healthcheck mechanism
- Как Saturn определяет что контейнер готов принимать трафик (HTTP probe, TCP probe, exec-based)?
- Конфигурируется ли timeout, interval, retries?
- Что происходит при падении healthcheck — rollback на предыдущую версию, просто алерт, ничего?
- Есть ли blue-green / rolling deploy из коробки?

**Почему важно**: в CLAUDE.md написано "blue-green deploy → health check → live", но мы не знаем как именно оно сконфигурировано и где менять параметры. В Phase 8D добавляем `/health/ready` vs `/health/live` — нужно знать что Saturn умеет их различать.

### 4. Observability primitives (bonus, если быстро)
- Есть ли у Saturn встроенный metrics endpoint (например Prometheus scrape URL для каждого контейнера)?
- Есть ли log aggregation из коробки или только `docker logs`-стиль?
- Доступны ли эти данные через API (чтобы federate в наш Prometheus) или только через UI?

## Источники куда смотреть

1. https://saturn.ac — основной сайт
2. https://docs.saturn.ac — если существует
3. https://saturn.ac/blog или changelog — часто фичи там анонсируются раньше чем попадают в docs
4. GitHub saturn-ac / saturn-platform org — если open-source части
5. Twitter/X @saturn_ac — иногда разработчики отвечают на вопросы там
6. Если есть CLI — `saturn --help`, `saturn docs`, `saturn volume --help`

Если на каждый вопрос нашёл ответ — отлично. Если какой-то вопрос не покрыт публичными docs — явно напиши "documented: нет, источников не найдено" и предложи как уточнить (форма поддержки, Discord, email).

## Deliverable

Создай файл `docs/saturn-capabilities.md` со структурой:

```markdown
# Saturn.ac Capabilities — research snapshot

Date: 2026-04-21
Sources: [list of URLs checked]

## 1. Persistent volumes
**Verdict**: supported | not supported | unclear
**How**: [конкретный механизм]
**Limits**: [размер, retention, backup]
**Sources**: [urls with quotes]

## 2. Staging environment
[same structure]

## 3. Healthcheck
[same structure]

## 4. Observability
[same structure]

## Recommendations

Куда ставим Prometheus+Grafana: [Saturn native / Grafana Cloud / отдельный VPS]
Куда ставим staging: [Saturn / VPS / skip]
Что блокируется если Saturn не умеет X: [список]

## Open questions

[что не удалось выяснить и как добить — support ticket, community Discord, etc.]
```

## Ограничения

- Не пиши код, не меняй существующие файлы кроме `docs/saturn-capabilities.md`
- Не фантазируй: если документации нет — так и напиши, не выдумывай фичи "по аналогии с Heroku"
- Цитируй источники с URL и выдержкой — иначе мы не сможем верифицировать
- Время на задачу: 1 рабочий день максимум. Если через 2-3 часа понял что публичных docs почти нет — останавливайся и пиши отчёт с тем что есть + список open questions
```
