# Sprint 1 / Task B4 — Runbooks: rollback + post-deploy

## Context

Orbit Messenger на Saturn.ac PaaS. Auto-deploy по `git push origin main` (см. AGENTS.md). Существующий runbook: `docs/runbook-restore.md` (PostgreSQL restore из R2 backup) — **используй его как style reference** для тона, уровня детализации, структуры.

Цель: когда прод упадёт в 3 ночи, дежурный открывает runbook и действует по нему без догадок. Runbook должен быть practical, copy-paste-ready команды, без water.

## 2 новых runbook'а

### Runbook 1: `docs/runbook-rollback.md`

**Сценарий**: только что задеплоили коммит X на прод через `git push origin main`, Saturn успешно прокатил blue-green, но пользователи жалуются (ошибки в Sentry / UI сломан / метрики ушли в пол). Нужно быстро откатить.

**Что должно быть в runbook'е**:

1. **Decision tree** — когда rollback, а когда hotfix forward:
   - Rollback: UI сломан, регрессия > 5 минут, нет быстрого фикса
   - Hotfix forward: изолированный баг, есть фикс < 10 минут

2. **Пошаговая процедура rollback** (команды):
   - Как идентифицировать последний good commit (git log, последний зелёный CI)
   - `git revert <bad-sha>` vs `git reset --hard <good-sha> && git push --force-with-lease` — какой когда (подсказка: **revert безопаснее, force-push на main запрещён в AGENTS.md**)
   - Как дождаться что Saturn подхватил новый деплой (проверка dashboard / healthcheck)
   - Smoke-чеки после rollback (ссылка на runbook-post-deploy.md)

3. **Data migrations rollback** — что делать если плохой коммит включал миграцию БД:
   - Напомнить что миграции forward-only в нашем проекте (`migrations/NNN_*.sql`)
   - Варианты: оставить миграцию примененной но откатить код / написать NNN+1 обратную миграцию / restore из backup (ссылка на runbook-restore.md)
   - Риски и чеклист "прежде чем откатывать миграцию"

4. **Саммари**: таблица времени — revert: 2-3 min → Saturn redeploy: 2-3 min → smoke: 2 min → total ~7-8 min

5. **Escalation**: когда звать вторую голову (broken migration, data loss risk, auth-service не поднимается)

### Runbook 2: `docs/runbook-post-deploy.md`

**Сценарий**: только что прошёл `git push origin main`, Saturn auto-deploy завершился. Дежурный должен проверить что всё правда живое.

**Что должно быть в runbook'е**:

1. **Health endpoints** — какие URL дёрнуть:
   - Gateway: `GET https://gateway.orbit.mst/health/live`, `/health/ready`
   - Каждый из 8 сервисов — если доступны напрямую
   - Что значат разные ответы, когда паниковать

2. **Smoke-чеклист UI** (2-3 минуты):
   - Открыть веб-приложение, войти тестовым юзером
   - Отправить сообщение в тестовый чат → проверить что долетело
   - Проверить что WebSocket подключается (Network tab → WS → `/api/v1/ws`)
   - Проверить что `/api/v1/ai/*` не возвращает 503

3. **Metrics / observability**:
   - Какие дашборды смотреть (см. `monitoring/` для local-dev примеров)
   - Красные флаги в метриках: error rate spike, latency p95 > baseline × 2, memory approaching limit
   - Где смотреть логи (Saturn dashboard + локально `docker compose logs`)

4. **Rollback trigger** — когда переходить в runbook-rollback.md:
   - Критерии (конкретные: error rate > X% за 5 минут, health fails дольше Y min)
   - Linking к runbook-rollback.md

5. **Sign-off** — что нужно проверить/отметить чтобы закрыть деплой:
   - Smoke passed, metrics stable 10 min
   - Slack-анонс в #deploys с sha + summary

## Ограничения

- **НЕ запускай подагентов с `run_in_background: true`**
- **НЕ выдумывай конкретные URL/Grafana dashboards**, если их нет в проекте. Если прод URL не задокументирован — оставь placeholder `<GATEWAY_URL>` и пометь как TODO
- **НЕ копируй содержимое runbook-restore.md** — просто ссылайся на него
- **НЕ пиши academic prose** — bullet lists, copy-paste команды, таблицы
- Максимум 200 строк на runbook, если выходит больше — значит слишком много воды

## Deliverable

1. `docs/runbook-rollback.md`
2. `docs/runbook-post-deploy.md`
3. Стиль и глубина — как `docs/runbook-restore.md` (прочти его первым, подстройся)
4. Commit: `docs(runbook): add rollback and post-deploy runbooks for Phase 8D`
5. Короткий `docs/sprint-1-runbooks-report.md`: краткое саммари что написал, какие placeholders остались как TODO

## Время
1 день. Если чего-то не хватает из инфры проекта (нет конкретных URL, dashboard'ов, monitoring tooling) — используй placeholder, пометь TODO, продолжай.
