# Orbit Messenger — Prompt для исправления багов и доработок

## Контекст

Проведено ручное тестирование Orbit Messenger (все сервисы локально через docker-compose). Тестировались два пользователя: admin@orbit.test и user2@orbit.test. Все основные фичи Phase 1-5 работают. Найдены 3 бага и несколько доработок.

## Баги для исправления

### Bug 1: "Group Info" вместо "User Info" в профиле DM

**Симптом:** При клике на имя пользователя в шапке DM-чата, правая панель показывает заголовок "Group Info" вместо "User Info".

**Причина:** В `web/src/components/right/RightHeader.tsx` функция `getHeaderTitle()` (строка 343-365) падает в fallback `return oldLang('GroupInfoTitle')` потому что `userId` не определён. Переменная `userId` вычисляется на строке 733: `isProfile && chatId && isUserId(chatId)` — но для DM `chatId` это UUID чата, а не UUID пользователя, поэтому `isUserId(chatId)` возвращает `false`.

**Файлы:**
- `web/src/components/right/RightHeader.tsx` — строки 343-365 (getHeaderTitle), строка 733 (userId lookup)

**Решение:** Для DM-чатов нужно извлечь peer user ID из чата (через `selectChatUser` или `chat.userId` / `privateChatUserId`) вместо проверки `isUserId(chatId)`.

---

### Bug 2: История поиска не сохраняется

**Симптом:** После поиска запрос не появляется в истории. GET `/search/history` возвращает пустой массив.

**Причина:** Метод `saveSearchQuery()` в `web/src/api/saturn/methods/search.ts` (строка 766-768) реализован, но **нигде не вызывается**. Action handlers `searchMessagesGlobal` и `setGlobalSearchQuery` в `web/src/global/actions/api/globalSearch.ts` не вызывают `saveSearchQuery`.

**Файлы:**
- `web/src/api/saturn/methods/search.ts` — строки 766-768 (saveSearchQuery, реализован но не вызывается)
- `web/src/global/actions/api/globalSearch.ts` — строки 94-131 (searchMessagesGlobal), строка 36 (setGlobalSearchQuery)

**Решение:** Добавить вызов `saveSearchQuery(query)` в `searchMessagesGlobal` после успешного поиска (или при нажатии Enter). Не сохранять пустые запросы и дубликаты.

---

### Bug 3: Кнопка звонка не инициирует звонок

**Симптом:** В DM-чате кнопка телефона (Call) видна, но клик по ней не создаёт звонок. В логах calls-сервиса нет запросов.

**Причина:** Цепочка вызовов:
1. `HeaderActions.tsx:228` → `requestMasterAndRequestCall({ userId: chatId, chatId })`
2. `calls.ts (ui):353-397` → Устанавливает `phoneCall.state = 'requesting'`, вызывает `toggleGroupCallPanel()`
3. `PhoneCall.tsx:133` → useEffect при маунте компонента вызывает `connectToActivePhoneCall`
4. `calls.async.ts:263` → `callApi('requestCall', ...)` → POST `/calls`

**Проблема:** Шаг 3 зависит от рендера компонента `PhoneCall`. Если панель звонка не отрисовывается (компонент не маунтится), `connectToActivePhoneCall` никогда не вызывается и POST к calls-сервису не отправляется.

Дополнительно: `requestPhoneCall()` в `calls.ts:238-241` — это no-op заглушка.

**Файлы:**
- `web/src/components/middle/HeaderActions.tsx` — строка 228 (handleRequestCall)
- `web/src/global/actions/ui/calls.ts` — строки 353-397 (requestCall UI action)
- `web/src/global/actions/api/calls.async.ts` — строки 246-270 (connectToActivePhoneCall)
- `web/src/api/saturn/methods/calls.ts` — строки 277-328 (createCall), строки 238-241 (requestPhoneCall no-op)
- `web/src/components/calls/phone/PhoneCall.tsx` — строка 133 (useEffect trigger)

**Решение:** Два варианта:
- (A) Вызывать `connectToActivePhoneCall` напрямую из `requestCall` UI action, без зависимости от маунта PhoneCall компонента
- (B) Убедиться что PhoneCall компонент рендерится при `phoneCall.state === 'requesting'` (проверить условия рендера в родительском компоненте)

---

## Доработки (не баги, а improvements)

### 4. Shared Media панель — показать Common Chats

В правой панели профиля пользователя есть Shared Media (Media/Files/Links/Music), но нет секции "Groups in Common". Бэкенд-эндпоинт `GET /users/:id/common-chats` работает и возвращает корректные данные.

**Нужно:** Добавить секцию "Groups in Common" в правую панель профиля пользователя, которая вызывает `fetchCommonChats(userId)` и отображает список общих групп.

### 5. Активные сессии — показывает 12

В Settings → Active Sessions показывается "12" — это остаточные сессии от прошлых тестов и текущие API-вызовы через curl. Не баг, но стоит проверить что UI для управления сессиями (отзыв отдельных сессий) работает.

---

## Приоритеты

1. **Bug 3** (Calls) — Medium priority, Phase 6 blocker
2. **Bug 1** (Group Info text) — Low priority, косметика
3. **Bug 2** (Search history) — Low priority, доработка
4. **Improvement 4** (Common chats UI) — Low priority
