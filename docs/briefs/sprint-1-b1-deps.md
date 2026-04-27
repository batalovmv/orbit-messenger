# Sprint 1 / Task B1 — Dependency vulnerability scan + bumps

## Context

Orbit Messenger — Go microservices + TypeScript/Teact frontend, monorepo. AGENTS.md содержит конвенции проекта (читай его в первую очередь). Phase 8D (Production Hardening), готовимся к пилоту MST. Уже были 2 раунда фиксов: `1f83be8 chore(deps): bump Go libs to patch known CVEs` и `b9d84ac fix(deps): npm audit — patch music-metadata + file-type DoS`. Нужен свежий скан.

## Задача

1. **Go scan**: для каждого из 8 сервисов (`services/ai`, `auth`, `bots`, `calls`, `gateway`, `integrations`, `media`, `messaging`) — запустить `govulncheck ./...` из соответствующего `services/<name>/` директории. Собрать все `GO-*` findings с severity.

2. **npm scan**: из `web/` — `npm audit --production --json` (prod-only, dev-chain игнорируем per текущей политики проекта). Собрать high/critical findings.

3. **Патчи**: для каждой уязвимости — решение:
   - Minor/patch bump (same major) → применить (`go get -u package@version`, commit в go.mod/go.sum)
   - Major bump → **не делать**, записать в open questions
   - Нет патча → записать в open questions с ссылкой на advisory

4. **Build check**: после всех bump'ов — `cd services/<name> && go build ./...` для каждого сервиса где менял deps. Если ломается — откатить бамп для этого сервиса, записать как "requires code changes".

## Ограничения

- **НЕ запускай подагентов с `run_in_background: true`** — оставайся в одной sync-сессии
- **НЕ трогай код вне go.mod/go.sum/package.json/package-lock.json** — только патчи
- **НЕ бамп major-версий** — это не hotfix, требует review
- **НЕ трогай `go` directive** (версия языка) — она уже зафиксирована на 1.24.0, не меняй
- **НЕ запускай `npm install` без флага `--package-lock-only`** если нужен dry-run

## Deliverable

1. Файл `docs/sprint-1-deps-report.md` со структурой:

```markdown
# Sprint 1 — Dependency audit report

Date: 2026-04-21

## Go services

### services/ai
- govulncheck: [clean | N findings]
- Findings (if any): [GO-2024-XXXX → pkg@old_ver → new_ver → patched/blocked]

[repeat for each service]

## Frontend (web/)
- npm audit: [clean | N high, M critical]
- Findings: [CVE/GHSA → pkg@old_ver → new_ver → patched/blocked]

## Patches applied
- services/foo/go.mod: pkg@1.2.3 → pkg@1.2.4 (GO-XXXX)
- web/package.json: bar@2.0.0 → bar@2.0.1 (GHSA-XXXX)

## Open questions / requires review
- [major bumps needed / blocked advisories / no-patch situations]

## Build verification
- services/ai: go build ./... → [ok | failed → rolled back]
[repeat]
```

2. Commits: один коммит на сервис + один на web. Формат: `fix(deps): patch GO-XXXX in <service>`.

## Время
1 рабочий день максимум. Если govulncheck/npm audit недоступны — напиши почему и остановись.
