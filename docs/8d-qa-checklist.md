# 8D QA Checklist for Claude Sonnet

## Проверяет Claude Sonnet

---

## 8D.4: Live Translate / Мультиязычность

### Проверки:

1. **Перевод интерфейса (UI)**
   - Открыть Orbit на prod
   - Проверить что все строки UI на русском
   - Проверить переключение языка в настройках

2. **Live Translate в чате**
   - Открыть любой чат
   - Отправить сообщение на русском
   - Включить auto-translate (если есть кнопка)
   - Проверить что сообщения переводятся автоматически

3. **Manual translate**
   - Выделить сообщение → "Translate"
   - Проверить что перевод появляется

4. **Настройки языка**
   - Settings → Language
   - Проверить RU/EN переключение
   - Проверить "Show Translate Button" toggle
   - Проверить "Translate Entire Chats" toggle

5. **Backend API (если есть доступ)**
   - `GET /users/me/settings` — проверить language + can_translate
   - `PUT /users/me/settings` — проверить обновление

### Ожидаемо:
- Все UI строки на RU
- Auto-translate работает
- Manual translate работает
- Настройки сохраняются

### Проблемы фиксировать:
- Конкретная функция которая не работает
- Браузер/OS где воспроизводится
- Скриншот если возможно

---

## 8D.5: Calls QA (browser)

### Проверки (нужно 2+ браузера/устройства):

**1. P2P звонок:**
- User A открывает чат с User B
- Нажимает кнопку звонка
- User B принимает звонок
- Проверить: audio работает, video работает
- User A завершает звонок

**2. Групповой звонок (SFU):**
- Открыть групповой чат (3+ участника онлайн)
- Начать групповой звонок
- Проверить: video grid показывает всех участников
- Screen sharing: start → проверяет что показывает → stop

**3. Push notification (app closed):**
- Browser backgrounded или tab скрыт
- User A звонит User B
- Проверить: push notification появляется с кнопками "Принять" / "Отклонить"
- Клик "Принять" → открывает звонок

**4. Mute/Screen-share buttons:**
- Во время звонка нажать mute
- Проверить что собеседник видит "Muted"
- Включить video → проверить video stream

### Ожидаемо:
- Звонки работают стабильно
- Push уведомления приходят когда app закрыт
- Screen sharing работает

### Проблемы фиксировать:
- Браузер + версия где воспроизводится
- Конкретный step где падает
- Есть ли error в console

---

## 8D.6: Security Audit (code review) — DONE (2026-04-24)

### Результаты:

1. **OWASP Top 10 spot-check:** PASS
   - A01 Broken Access Control — нет IDOR паттернов (userID берётся из JWT, не из params)
   - A02 Cryptographic Failures — нет MD5/SHA1 кроме RFC7635 TURN (корректно)
   - A03 Injection — нет fmt.Sprintf в SQL запросах, все параметризованы ($1, $2)
   - A05 Security Misconfiguration — нет CORS wildcard, нет AllowAllOrigins
   - A07 Auth Failures — auth middleware на всех защищённых routes
   - A09 Logging — нет паролей/токенов в slog вызовах (только password_len)
   - A10 SSRF — webhook URL validation через ParseRequestURI + HTTPS check
   - SAST scan: 0 findings

2. **Input validation:** PASS (выполнено ранее — systematic pass всех handlers)

3. **Rate limiting:** PASS (Redis-backed на всех публичных endpoints)

4. **GPL-3.0 license headers:** DONE
   - 225 Go файлов: SPDX-License-Identifier: GPL-3.0-or-later добавлен
   - 48 Saturn TS файлов: SPDX-License-Identifier: GPL-3.0-or-later добавлен

### Статус: PASS

---

## Output Format

```
## 8D.4 Live Translate
status: PASS / PARTIAL / FAIL
details: [что проверил]
issues: [конкретные проблемы если есть]
fixes: [что исправить]

## 8D.5 Calls QA
status: PASS / PARTIAL / FAIL
details: [какие тесты прошли]
issues: [конкретные проблемы с браузером/шагом]
fixes: [что исправить]

## 8D.6 Security Audit
status: PASS / PARTIAL / FAIL
details: [какие проверки прошли]
issues: [найденные уязвимости]
fixes: [что исправить]
```