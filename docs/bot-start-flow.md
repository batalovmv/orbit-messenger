# Bot `/start` Flow

Справка по тому как пользователь запускает бота через DM в Orbit. Закрывает чек-бокс "Phase 8B: /start автоотправка при открытии DM — код есть, нужна проверка" из [PHASES.md](../PHASES.md).

**Поведение UX:** как в Telegram — пустой DM с ботом показывает кнопку **START**, пользователь кликает, бот получает `/start` и отвечает. Никакой беззвучной автоотправки: явный клик обязателен, иначе пользователь будет отправлять команды ботам без своего согласия.

## Frontend путь

1. **Детекция пустого бот-DM:** [web/src/global/selectors/chats.ts:150](../web/src/global/selectors/chats.ts#L150) — `selectIsChatBotNotStarted(global, chatId)` возвращает `true`, если:
   - peer — это бот (`account_type='bot'` → `user.type='userTypeBot'`, см. [web/src/api/saturn/apiBuilders/users.ts:11](../web/src/api/saturn/apiBuilders/users.ts#L11))
   - в чате нет сообщений (или последнее сообщение — `isHistoryClearMessage`)

2. **Вычисление `canStartBot`:** [web/src/components/middle/MiddleColumn.tsx:804](../web/src/components/middle/MiddleColumn.tsx#L804) — `canStartBot = !canRestartBot && isBotNotStarted`

3. **Рендер кнопки START в 3 местах:**
   - [HeaderActions.tsx:361](../web/src/components/middle/HeaderActions.tsx#L361) — кнопка в хедере чата (десктоп)
   - [HeaderMenuContainer.tsx:624](../web/src/components/middle/HeaderMenuContainer.tsx#L624) — пункт в меню действий
   - [MiddleColumn.tsx:661](../web/src/components/middle/MiddleColumn.tsx#L661) — большая кнопка в футере (мобайл)
   - Все три используют локализационный ключ `BotStart`

4. **Клик → dispatch action:** [web/src/global/actions/api/bots.ts:517](../web/src/global/actions/api/bots.ts#L517) — `startBot({ botId, param })`
   ```typescript
   if (bot.type === 'userTypeBot') {
     const startText = param ? `/start ${param}` : '/start';
     actions.sendMessage({ text: startText, tabId });
     return;
   }
   ```
   Опциональный `param` — это deep-link параметр (`t.me/mybot?start=xyz`), для Orbit пока не используется.

5. **Отправка:** `sendMessage` идёт через обычный path Saturn → `POST /api/v1/chats/:id/messages` → messaging service.

## Backend путь

6. **Messaging persists + NATS publish:** messaging сохраняет сообщение, публикует событие на subject `orbit.chat.{chatID}.message.new` (см. [CLAUDE.md#nats-события](../CLAUDE.md)).

7. **Bots subscriber:** [services/bots/internal/service/nats_subscriber.go:54](../services/bots/internal/service/nats_subscriber.go#L54) — `BotNATSSubscriber.Start()` подписан на `orbit.chat.*.message.new`. При получении события:
   - Извлекает `chat_id` из subject
   - `installations.ListChatsWithWebhookBots(chatID)` — находит всех ботов, установленных в этот чат
   - Для каждого бота строит `botapi.Update` в Telegram-совместимом формате ([nats_subscriber.go:121](../services/bots/internal/service/nats_subscriber.go#L121))
   - Если у бота есть `webhook_url` → `webhookWorker.Enqueue(botID, webhookURL, secretHash, update)`
   - Если webhook не задан → `updateQueue.Push(botID, update)` (для polling-based ботов через `getUpdates`)

8. **Защита от self-echo:** [nats_subscriber.go:88](../services/bots/internal/service/nats_subscriber.go#L88) — `if info.UserID.String() == event.SenderID { continue }` — бот не получает собственное эхо, когда сам что-то отправляет.

9. **Webhook delivery:** `WebhookWorker` доставляет с retry (exponential backoff, 3 attempts), HMAC-SHA256 подписью и SSRF protection (см. [PHASES.md:1266](../PHASES.md)).

10. **Бот отвечает:** через Bot API `sendMessage` (Telegram-совместимый) → messaging → обратно в чат.

## Как протестировать живьём

1. Создать бота: `POST /bots` через Settings → Bot Management → получить token.
2. Задать webhook URL: `POST /bots/:id` с `webhook_url`. Или использовать polling-based подход через `GET /bot/:token/getUpdates`.
3. Установить бота в DM: `POST /bots/:id/install` (или автоматически при создании DM с ботом).
4. Открыть пустой DM с ботом в UI — должна появиться кнопка START.
5. Клик START → в БД в `messages` появляется сообщение с `content='/start'` → в логах bots service: `enqueue webhook delivery` / `push bot update` → бот получает `Update` с `Message.Text='/start'`.
6. Бот отвечает через `sendMessage` → в UI появляется ответ бота.

## Что НЕ делаем

- **Автоотправка без клика** — отвергнуто как анти-паттерн. TG так не делает, пользователь должен явно подтвердить намерение запустить бота. Термин "автоотправка" в PHASES.md описывает UX-поток "пользователь одним кликом стартует бота", а не беззвучную фоновую отправку.
- **Deep-link параметры (`start=xyz`)** — код поддерживает `param`, но в Orbit нет public-web бот-ссылок формата `t.me/bot?start=xyz`. Может понадобиться позже если будем делать приглашения через боты.
