# Sprint 1 / Task B3 — Playwright smoke test: login → send → receive

## Context

Orbit Messenger frontend: форк Telegram Web A на Teact (не React). Существуют два playwright-окружения:
- `web/tests/playwright/` — основной, конфиг `web/playwright.config.ts`
- `web/tests/playwright-live/` — против реального бэка, конфиг `web/playwright.live.config.ts`

Читай `AGENTS.md` для общих конвенций проекта, `web/CLAUDE.md` для frontend-specific.

Нужен **один smoke-тест критического пути пользователя**: логин → отправить сообщение в чат → получить подтверждение доставки.

## Задача

1. **Исследуй существующую структуру**:
   - `web/tests/playwright/` — что уже есть, какой pattern setup/fixtures
   - `web/playwright.config.ts` — baseURL, projects, auth setup
   - Есть ли shared login helper / auth state / storage state

2. **Реши где добавлять** — в существующий `playwright` (моки/fake backend) или `playwright-live` (реальный прод). По умолчанию — **в `playwright`**, потому что smoke должен быть детерминирован и не зависеть от прода. Если в проекте `playwright` уже моканый и нет existing messaging-спеков — там и добавляй.

3. **Тест-сценарий** (один файл, один `test()` или 2-3 связанных):
   - **Step 1 — Login**: авторизуйся (либо через программный storage-state/cookies если такой pattern уже есть, либо через UI с тестовым юзером). Предпочтение — существующий helper/fixture.
   - **Step 2 — Open chat**: открой любой существующий чат (если моканый backend — там должен быть pre-seed чат)
   - **Step 3 — Send message**: введи уникальный текст (с timestamp'ом чтоб не конфликтовало при повторных запусках) → нажми Send
   - **Step 4 — Verify receive**: дождись что сообщение появилось в DOM с correct status (delivered/sent indicator). Используй `expect(...).toBeVisible()` с разумным timeout. Проверь что текст совпал.

4. **Не изобретай infra**: если нет auth helper — используй UI-flow с тестовым юзером из существующих fixtures. Если и этого нет — запиши в open questions и остановись, НЕ создавай новый mock backend.

## Ограничения

- **НЕ запускай подагентов с `run_in_background: true`**
- **НЕ трогай prod-код** и не меняй архитектуру web-приложения
- **НЕ добавляй новые devDependencies** — используй то что уже в `web/package.json`
- **НЕ делай flaky тесты** — предпочитай `expect(locator).toBeVisible()` с builtin waiting, избегай `waitForTimeout`
- Тест должен проходить локально: `cd web && npx playwright test tests/playwright/<твой-файл>.spec.ts`

## Deliverable

1. Один новый `.spec.ts` файл в `web/tests/playwright/`, название соответствует существующим (e.g. `smoke-send-receive.spec.ts`)
2. Файл проходит `npx playwright test tests/playwright/<file>` локально (зелёный)
3. Commit: `test(web): playwright smoke for login → send → receive`
4. Отчёт `docs/sprint-1-playwright-report.md`: что добавил, против какого окружения, как запускать, что моканое/реальное

## Время
1 день. Если существующая playwright-инфра не готова для этого сценария (нет auth, нет seed чатов) — **не строй с нуля**, запиши blocker в report и остановись.
