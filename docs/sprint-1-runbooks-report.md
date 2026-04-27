# Sprint 1 — Runbooks report

## Что создано

1. **`docs/runbook-rollback.md`** — процедура отката продакшен-деплоя
   - Decision tree: rollback vs hotfix forward
   - Пошаговый `git revert` (force-push запрещён)
   - Раздел про откат миграций БД (3 варианта по уровню риска)
   - Timeline: ~7–8 мин от обнаружения до восстановления
   - Escalation-критерии

2. **`docs/runbook-post-deploy.md`** — чеклист верификации после деплоя
   - Health endpoints (gateway + 8 сервисов)
   - UI smoke checklist (~2–3 мин)
   - Метрики и красные флаги (error rate, latency, memory)
   - Конкретные критерии когда переходить к rollback
   - Sign-off процедура с Slack-шаблоном

## Остались TODO / placeholders

| Placeholder | Где | Причина |
|-------------|-----|---------|
| `<GATEWAY_URL>` | rollback, post-deploy | Прод URL не задокументирован в репо |
| Per-service health routes (`/api/v1/<svc>/health`) | post-deploy | Нет health endpoints в коде сервисов — нужно верифицировать точные пути после реализации |
| Grafana/monitoring dashboards link | post-deploy | Директория `monitoring/` не существует в проекте |

## Стиль

Ориентировался на `docs/runbook-restore.md`: copy-paste-ready команды, таблицы, минимум прозы, без академического тона.
