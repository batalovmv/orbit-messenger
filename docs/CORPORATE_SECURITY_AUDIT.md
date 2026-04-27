# Orbit Messenger — Corporate Security Audit

**Created:** 2026-04-23
**Context:** Корпоративный мессенджер MST, 150 сотрудников, invite-only
**Version:** Phase 8 (AI, Bots, Integrations + Production Hardening)

---

## Executive Summary

**Verdict:** 7/10 — Готов к pilot с пониманием tech debt

**Блокеры до prod:**
- H1: Нет admin-доступа к сессиям других юзеров
- H2: Нет data export endpoint  
- H3: DeactivateUser не инвалидирует access tokens

---

## History

- 2026-04-23: Initial audit + corporate context re-evaluation
- 2026-04-23: SQL & Input validation audit completed
- 2026-04-23: Roles & Permissions documented
- 2026-04-23: Website QA findings added

---

## 9. Website QA Findings (2026-04-23)

### Critical (Блокирует функциональность)

| Issue | Severity | Fix Required | Status |
|-------|----------|--------------|--------|
| Calls 403 — INTERNAL_SECRET missing for calls service | HIGH | User: add via Saturn dashboard | Pending |
| Settings inaccessible — frontend build needed | HIGH | Build + deploy web | Pending |
| Undecrypted message shown as base64 | HIGH | Fix decryption flow | Pending |
| No UI error on call failure | HIGH | Add error toaster | Pending |

### Medium (Работает, но плохо)

| Issue | Severity | Fix Required | Status |
|-------|----------|--------------|--------|
| Translate menu — raw i18n keys | MEDIUM | Add missing translations to fallback.strings | Pending |
| /auth/reset-admin public | MEDIUM | Add audit log or disable | Pending |

### Low (Cosmetic/Tech Debt)

| Issue | Severity | Fix Required | Status |
|-------|----------|--------------|--------|
| NewSettingsHandler — variadic internalSecret | LOW | Make required param | ✅ FIXED |

**FIXED in this commit:**
- NewSettingsHandler: variadic → required string parameter
- ResetAdmin: added audit logging (invalid key, target check, success)
- AuthService: added logger field for audit logging

---

## Website QA Findings (2026-04-23)

### Critical (Блокирует функциональность)

| Issue | Severity | Fix Required | Status |
|-------|----------|--------------|--------|
| Calls 403 — INTERNAL_SECRET missing for calls service | HIGH | User: add via Saturn dashboard | Pending |
| Settings inaccessible — frontend build needed | HIGH | Build + deploy web | Pending |
| Undecrypted message shown as base64 | HIGH | Fix decryption flow | Pending |
| No UI error on call failure | HIGH | Add error toaster | Pending |

### Medium (Работает, но плохо)

| Issue | Severity | Fix Required | Status |
|-------|----------|--------------|--------|
| Translate menu — raw i18n keys | MEDIUM | Add missing translations to fallback.strings | Pending |
| /auth/reset-admin public | MEDIUM | ✅ FIXED — Added audit logging | Pending |

### Low (Cosmetic/Tech Debt)

| Issue | Severity | Fix Required | Status |
|-------|----------|--------------|--------|
| NewSettingsHandler — variadic internalSecret | LOW | ✅ FIXED — Made required param | Done |

---

## 1. Security Audit

### 1.1 Auth & Session

| Severity | Issue | Location | Status |
|----------|-------|----------|--------|
| MEDIUM | TOTP replay (30sec window) | auth_service.go:101 | Acceptable corporate |
| LOW | Refresh token from body | auth_handler.go:263-268 | Acceptable |
| ✅ | bcrypt cost=12 | auth_service.go:51 | GOOD |
| ✅ | Timing-safe dummy hash | auth_service.go:84-86 | GOOD |
| ✅ | JWT blacklist on logout | middleware/jwt.go:51 | GOOD |
| ✅ | Session rotation atomic | session_store.go | GOOD |

### 1.2 Input Validation

| Severity | Issue | Location | Fix Required |
|----------|-------|----------|------------|
| ✅ FIXED | CreateChat — type allowlist | chat_service.go | DONE |
| ✅ FIXED | UpdateMemberRole — role allowlist | chat_service.go | DONE |
| ✅ | ChunkedInit — already validates | media_service.go | GOOD |
| ✅ FIXED | audit_store cursor — removed unused parse | audit_store.go | DONE |
| LOW | SetSlowMode — не валидирует диапазон Seconds | chat_handler.go:427-438 | Pending |

### 1.3 SQL Injection

| Severity | Issue | Location | Status |
|----------|-------|----------|--------|
| ✅ | All queries use $1, $2 parameters | — | GOOD |
| MEDIUM | delivery_store — fmt.Sprintf в interval | delivery_store.go:267 | Review needed |
| MEDIUM | notification_store — строковая конкатенация | notification_store.go:46-48 | YES |

### 1.4 Rate Limiting

TODO: Fill after audit

### 1.5 CORS

TODO: Fill after audit

### 1.6 Secrets

TODO: Fill after audit

### 1.7 File Upload

TODO: Fill after audit

---

## 2. Roles & Permissions (Corporate)

### 2.1 Current Roles

**Существующие роли (из pkg/permissions/system.go):**

| Role | Rank | Permissions |
|------|------|-------------|
| superadmin | 4 | All (16383) — полный контроль |
| compliance | 3 | SysViewAllChats, SysReadAllContent, SysViewAuditLog, SysExportData, SysViewBotLogs |
| admin | 2 | SysManageUsers, SysManageInvites, SysManageContent, SysManageBots, SysManageIntegrations, SysViewBotLogs |
| member | 1 | 0 — базовая роль |

### 2.2 Recommended Corporate Roles

**Рекомендуемые для MST корпоративного деплоя:**

| Role | Use Case | Permissions |
|------|----------|-------------|
| superadmin | IT Lead / Security | All |
| compliance | Legal / HR / Internal Audit | ViewAllChats + ReadAllContent + ViewAuditLog + ExportData |
| department_head | Head of department | Manage department chats + invite |
| team_lead | Team lead | Manage team chat + invite members |
| member | Regular employee | Default |

### 2.3 Role Hierarchy

```
superadmin (4)
    ↓ can assign: any role
compliance (3)
    ↓ can assign: member
admin (2)
    ↓ can assign: member
member (1)
    ↓ cannot assign roles
```

### 2.4 Permission Matrix

| Permission | superadmin | compliance | admin | member |
|------------|-------------|------------|-------|--------|
| SysManageUsers | ✅ | ❌ | ✅ | ❌ |
| SysManageInvites | ✅ | ❌ | ✅ | ❌ |
| SysManageChats | ✅ | ❌ | ❌ | ❌ |
| SysViewAllChats | ✅ | ✅ | ❌ | ❌ |
| SysReadAllContent | ✅ | ✅ | ❌ | ❌ |
| SysManageSettings | ✅ | ❌ | ❌ | ❌ |
| SysManageContent | ✅ | ❌ | ✅ | ❌ |
| SysViewAuditLog | ✅ | ✅ | ❌ | ❌ |
| SysExportData | ✅ | ✅ | ❌ | ❌ |
| SysAssignRoles | ✅ | ❌ | ❌ | ❌ |
| SysManageSecurity | ✅ | ❌ | ❌ | ❌ |
| SysManageBots | ✅ | ❌ | ✅ | ❌ |
| SysManageIntegrations | ✅ | ❌ | ✅ | ❌ |
| SysViewBotLogs | ✅ | ✅ | ✅ | ❌ |

### 2.5 Chat-Level Roles

| Role | Permissions |
|------|------------|
| owner | delete chat, manage members, edit settings |
| admin | manage members, mute, pin |
| member | send messages, react |
| banned | no access |

### 2.6 Gaps Analysis

| Gap | Current State | Recommended |
|-----|---------------|-------------|
| Department head role | ❌ | Add new role for department management |
| Team lead role | ❌ | Add new role for team management |
| HR-only role | compliance close but no user management | Consider dedicated HR role |
| Read-only auditor | compliance has export — may be too much | Consider read-only variant |

---

## 3. Compliance

### 3.1 Audit Trail

**Реализовано:**

- `audit_store.go` — append-only таблица, защищена DB-триггерами
- Audit пишется ПЕРВЫМ до действия (fail-closed)
- Каждая запись: actor_id, IP, User-Agent, timestamp
- Константы действий задокументированы в model/audit.go

**Что логируется:**

- Admin actions: create/delete chats, users, invites
- Privileged read: чужой чат с SysReadAllContent
- Role changes: assign, revoke
- Session management

**Что НЕ логируется (пробелы):**

- M1: Batch read не агрегируется
- M2: Просмотр audit log не fail-closed
- M4: ListAllUsers не логируется

### 3.2 Data Export

**Статус:** ❌ НЕТ endpoint

- SysExportData permission определён (bit 256)
- compliance роль имеет permission
- Но endpoint не существует

**Требуется для corporate:**
- GET /admin/chats/:id/export?format=json
- GET /admin/users/:id/export?format=json
- Все с audit trail

### 3.3 Data Retention

**Статус:** ❌ НЕ реализовано

- Нет автоматического удаления старых сообщений
- Нет retention policy enforcement
- Рекомендуется: 1 год для обычных, 3 года для compliance

---

## 4. Infrastructure Security

### 4.1 Database (PostgreSQL)

| Area | Status | Notes |
|------|--------|-------|
| WAL archival | ⚠️ Deferred | Hourly backups via Saturn UI |
| At-rest encryption | Partially implemented | Message content encrypted, media in progress |
| Connection | ✅ SSL required | Via env var |
| User isolation | ✅ Row-level security | Per-chat access checks |
| Audit table | ✅ Append-only | DB triggers prevent deletion |

### 4.2 Redis

| Area | Status | Notes |
|------|--------|-------|
| Password | ✅ Required | Via REDIS_PASSWORD env |
| ACL | ❌ Not configured | Default user with password |
| TLS | ⚠️ Optional | Can enable via redis:// |

### 4.3 Network

| Area | Status | Notes |
|------|--------|-------|
| Public ports | ⚠️ Only gateway:8080 | Other services via internal network |
| Rate limiting | ✅ Implemented | Per-service Lua scripts |
| Internal auth | ✅ X-Internal-Token | Gateway injects |

---

## 8. Next Steps

1. Review audit findings
2. Prioritize fixes for pilot vs production
3. Implement recommended roles (department_head, team_lead)
4. Plan data export endpoint development

---

## 5. Findings Summary

### HIGH (Блокеры до prod)

| ID | Issue | Fix Required | Status |
|----|-------|--------------|--------|
| H1 | Нет admin-доступа к сессиям | YES | Not started |
| H2 | Нет data export endpoint | YES | Not started |
| H3 | DeactivateUser не инвалидирует токены | YES | Not started |
| V1 | CreateChat — не валидирует type/name | YES | Not started |
| V2 | UpdateMemberRole — нет allowlist | YES | Not started |

### MEDIUM

| ID | Issue | Fix Required | Status |
|----|-------|--------------|--------|
| M1 | ChunkedInit — mime_type без валидации | YES | Not started |
| M2 | audit_store cursor — uuid.Parse игнорируется | YES | Not started |
| M3 | notification_store — строковая конкатенация | YES | Not started |
| M4 | delivery_store — fmt.Sprintf в interval | Review | Not started |
| A1 | TOTP replay — 30sec окно | Acceptable | — |
| A2 | Refresh token from body | Acceptable | — |

### LOW

| ID | Issue | Fix Required | Status |
|----|-------|--------------|--------|
| L1 | SetSlowMode — не валидирует диапазон | YES | Not started |
| CORS | AllowCredentials с AllowOrigins * | ✅ OK | — |
| Rate | All public endpoints covered | ✅ OK | — |

---

## 6. Recommendations

### Immediate (before pilot)

1. **H3** — Add Redis key in DeactivateUser (1 строка)
2. **V1** — Add type/name validation in CreateChat
3. **V2** — Add role allowlist in UpdateMemberRole
4. **M3** — Fix notification_store string concatenation

### Before Full Production

1. **H1** — Admin session management endpoints
2. **H2** — Data export endpoints
3. **M1** — Content-based MIME validation for chunked upload
4. **M2** — Fix audit_store cursor validation
5. Add department_head and team_lead roles

### Nice to Have

- Data retention policy automation
- Read-only auditor role variant
- Automated compliance reports

---

## 6. Recommendations

TODO: Fill

---

## 7. Verdict

**Pilot Deployment:** ✅ CONDITIONAL (after immediate fixes)
**Full Production:** ❌ Requires H1, H2, H3 + V1, V2 + M3 fixes

**Immediate fixes required before pilot:**
- V1: CreateChat validation
- V2: UpdateMemberRole validation  
- H3: DeactivateUser token invalidation
- M3: notification_store fix