# Sprint 1 / Task B2 — Regression tests for recent bugs

## Context

Orbit Messenger — Go microservices. Читай `AGENTS.md` для конвенций проекта **в первую очередь** — там описан паттерн тестирования (fn-field mocks, miniredis, NewNoopNATSPublisher, naming `TestFunction_Scenario`). Это канон, следуй ему.

На прошлой неделе было 3 прокола в проде. Для каждого — нужен regression test, который бы словил этот баг если бы существовал.

## 3 зоны + коммиты-фиксы

### Bug 1: Decrypt в chat store превью
- **Fix commit**: `b6624d7 fix(messaging): decrypt content in chat list preview and scheduled messages`
- **Что случилось**: `chat_store` читал `messages.content` колонку напрямую после `Scan` без вызова `DecryptContentField` — клиент получал ciphertext в превью последних сообщений чата и списке scheduled messages
- **Что тестировать**:
  1. Read fix commit: `git show b6624d7 --stat` и `git show b6624d7` — найти конкретные функции/файлы которые чинились
  2. В соответствующем `*_test.go` добавить тест: создаёшь message с известным plaintext → пишешь через `MessageStore` (он шифрует на запись) → читаешь через `ChatStore` метод который чинили (вероятно `ListChatsWithPreview` или аналог) → assert что `preview == plaintext`, а не ciphertext
  3. **Важно**: тест должен падать если убрать вызов `DecryptContentField` из store — это и есть regression guard
- **Локация**: `services/messaging/internal/store/*_test.go`

### Bug 2: WebSocket reconnect backoff
- **Fix commit**: `4e7a35d fix(web): reset WS reconnect backoff when REST confirms server is back`
- **Что случилось**: клиент наращивал exponential backoff бесконечно даже когда REST-probe подтверждал восстановление сервера — юзер ждал минуты до переподключения хотя gateway уже живой
- **Что тестировать**:
  1. Read fix commit — найти конкретный модуль WS reconnect в `web/src/`
  2. Unit test на функцию которая сбрасывает backoff при успешном REST probe
  3. Проверить: backoff был 30s → REST probe вернул 200 → backoff стал 1s (или initial)
- **Локация**: `web/src/**/*.test.ts` (или куда проект кладёт frontend unit-тесты — проверь существующую структуру)

### Bug 3: Gateway routing bots
- **Fix commit**: `8b8521b fix(gateway): route /chats/:id/bots to bots service, not messaging`
- **Что случилось**: gateway отправлял `/chats/:id/bots` на messaging (404), должен был на bots
- **Что тестировать**:
  1. Read fix commit — найти routing table/proxy config в `services/gateway/`
  2. Integration test что вызов `GET /api/v1/chats/<uuid>/bots` с валидным токеном → проксируется в bots service, не в messaging
  3. Mock нижележащие сервисы через `httptest.NewServer`, проверь куда ушёл запрос по URL path или по отдельному service-marker header'у
- **Локация**: `services/gateway/cmd/main_test.go` или `services/gateway/internal/*/routing_test.go` — смотри как у проекта уже устроены тесты gateway'а

## Ограничения

- **НЕ запускай подагентов с `run_in_background: true`** — sync в одной сессии
- **НЕ меняй продакшн-код** — только тесты. Если видишь проблему в проде-коде — запиши в open questions, не фикси
- **Следуй fn-field mock pattern** из AGENTS.md (НЕ mockgen, НЕ testify/mock). Пример в любом существующем `*_test.go` под `services/`
- **НЕ пиши интеграционные тесты с реальной PostgreSQL** если проект использует мок — проверь существующую практику в каждом сервисе
- Тесты должны пройти `go test ./...` в соответствующем сервисе. Если не прошли — итерируй до зелёного

## Deliverable

1. 3 новых теста в соответствующих файлах (один на каждый bug)
2. Запусти `go test ./...` в `services/messaging` и `services/gateway` → все зелёные
3. Для frontend: запусти `cd web && npm run test -- --run <путь>` для нового теста
4. Commit: отдельный коммит на каждый тест. Формат: `test(<area>): regression guard for <short desc>`
5. Отчёт `docs/sprint-1-tests-report.md` — что добавил, где, какие коммиты чинил, проходят ли локально

## Время
1 день. Если какой-то из 3 тестов не получается — не выдумывай "похожий тест ради галочки", запиши причину в отчёт и останови.
