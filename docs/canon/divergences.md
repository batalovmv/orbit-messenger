# Divergences from TZ / PHASES.md

Где документация говорит X, но реально Y. **НЕ реализуй буквально по ТЗ — сверяйся с этой таблицей.**

| Документ / план | Что написано | Реальность |
|-----------------|--------------|------------|
| Phase 7 | Signal Protocol E2E между клиентами | **Откачено.** Только AES-256-GCM at-rest (shared package `crypto`). Compliance-модель: администрация имеет полный доступ к переписке. |
| TZ-ORBIT-MESSENGER.md | Глобальные роли `superadmin` / `compliance` | **Реализовано частично.** 4 системные роли (`superadmin` / `compliance` / `admin` / `member`) с битмаской в [pkg/permissions/system.go](../../pkg/permissions/system.go); используется backend (admin export, audit log, feature flags, maintenance) и фронтом (AdminPanel, CompliancePanel) с feature-gate по роли. `superadmin` для bootstrap отдельно отключён в state.json `removed_features`, но 4-ролевая модель — живая. |
| TZ-ORBIT-MESSENGER.md | Channels (broadcast-каналы как в Telegram) | **Удалены.** Migration `035` дропнула таблицы и связанные сущности. |
| Phase 8D | 4 NATS streams (events / messages / presence / calls) | **1 stream `ORBIT`** с 24h retention, subject-routing внутри. |
| Phase 8D | 5 Redis keys (jwt_cache, jwt_blacklist, ratelimit, presence, sessions) | **3 prefixes:** `jwt_cache`, `jwt_blacklist`, `ratelimit`. Presence/sessions — иначе. |
| CLAUDE.md (старый) | "go 1.25 запрещено в новых сервисах" | **Исключение:** `services/gateway` на 1.25 из-за embedded `nats-server/v2` в тестах. Все остальные — 1.24. |
| TZ-ORBIT-MESSENGER.md | Architecture v2.0 ссылается на channels/superadmin | Считай эти разделы устаревшими; см. `architecture.md`. |

## Правило поддержки

При обнаружении нового расхождения — **обнови эту таблицу в том же PR**, который зафиксировал разрыв в коде.
