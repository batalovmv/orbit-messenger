# Задача: Saturn API wiring — name mismatches, stubs, error handling

## Роль и поведение
Ты — senior frontend-разработчик проекта Orbit Messenger (форк Telegram Web A). Работай автономно, не останавливайся на вопросах — принимай решения сам. После каждого блока изменений проводи самопроверку: перечитай изменённые файлы, убедись что нет опечаток, все импорты корректны, типы совпадают.

## Контекст
Orbit использует Saturn API (свой бэкенд) вместо Telegram MTProto. Фронтенд вызывает API через `callApi('methodName', ...)`, который ищет экспортированную функцию в `web/src/api/saturn/methods/index.ts`. Если метод не найден — возвращается `Promise.resolve(undefined)` и фича молча не работает.

## Что нужно сделать

### 1. Исправить name mismatch `editChatPhoto`
**Проблема:** Actions вызывают `callApi('editChatPhoto', ...)` (5 мест в `web/src/global/actions/api/chats.ts`), но Saturn экспортирует `updateChatPhoto` в `web/src/api/saturn/methods/media.ts`. Результат: смена аватарки чата/канала — тихий no-op.

**Решение:** В `web/src/api/saturn/methods/index.ts` добавить re-export:
```ts
export { updateChatPhoto as editChatPhoto } from './media';
```
Или создать функцию-обёртку `editChatPhoto` которая маппит аргументы из TG формата (chatId, accessHash, photo) в Saturn формат и вызывает `updateChatPhoto`. Проверь сигнатуру вызовов в `chats.ts:827,1062,2081,2099,2114` — они передают `{ chatId, photo }` или `{ chatId, accessHash, photo }`. Saturn `updateChatPhoto(chatId, avatarUrl)` принимает другие аргументы. Нужна обёртка которая:
- Если `photo` — это файл/blob, сначала загружает через media upload, получает URL
- Если `photo` — уже URL, передаёт напрямую
- Если `isDeleted` === true, вызывает `deleteChatPhoto`

### 2. Исправить name mismatch `toggleChatArchived`
**Проблема:** `callApi('toggleChatArchived', { chatId, ... })` вызывается в `web/src/global/actions/api/chats.ts:1142`, но Saturn экспортирует `archiveChat` и `unarchiveChat` раздельно.

**Решение:** В `index.ts` добавить обёртку:
```ts
export function toggleChatArchived({ chatId, isArchived }: { chatId: string; isArchived: boolean }) {
  return isArchived ? archiveChat({ chatId }) : unarchiveChat({ chatId });
}
```

### 3. Исправить `uploadProfilePhoto`
**Проблема:** `web/src/api/saturn/methods/index.ts:36` — stub возвращает `Promise.resolve(undefined)`. Загрузка аватарки профиля через Settings молча не работает.

**Решение:** Имплементировать через существующий media upload:
- Загрузить файл через `POST /media/upload` (уже есть в `media.ts`)
- Получить URL загруженного файла
- Вызвать `PATCH /users/me` с полем `avatar_url`
Проверь как вызывается: `settings.ts:56` и `initial.ts:146` — посмотри какие аргументы передаются и адаптируй.

### 4. Passcode таймаут
**Проблема:** `web/src/global/actions/ui/passcode.ts:107` — `TIMEOUT_RESET_INVALID_ATTEMPTS_MS = 1000 * 15` вместо 180000ms (3 минуты). Закомментирован правильный таймаут.

**Решение:** Раскомментировать правильное значение:
```ts
const TIMEOUT_RESET_INVALID_ATTEMPTS_MS = 180000; // 3 minutes
```

### 5. settingsApi.ts — добавить логирование ошибок
**Проблема:** `web/src/api/saturn/methods/settingsApi.ts` — все 12 методов имеют `catch { return undefined/false; }` без логирования. Ошибки privacy settings, notification settings, block/unblock полностью невидимы.

**Решение:** В каждый catch-блок добавить:
```ts
catch (err) {
  if (DEBUG) console.error('[Saturn] methodName failed:', err);
  return undefined; // или false
}
```
Импортировать `DEBUG` из `'../../../config'`.

### 6. `init.ts` not-implemented warn
**Проблема:** Уже обёрнут в `if (DEBUG)`, но спамит консоль во время разработки сотнями сообщений для TG-методов которые никогда не будут имплементированы.

**Решение:** Добавить набор игнорируемых методов:
```ts
const SILENCED_METHODS = new Set([
  'fetchStickerSetsForEmoji', 'fetchCustomEmoji', 'fetchPremiumPromo',
  'fetchTopInlineBots', 'fetchContactList', 'fetchCommonChats',
  'fetchChannelRecommendations', 'fetchSavedGifs', 'fetchTopReactions',
  // ... добавить все часто вызываемые которые точно не нужны
]);

if (DEBUG && !SILENCED_METHODS.has(fnName)) {
  console.warn(`[Saturn] Method not implemented: ${fnName}`);
}
```

## Самопроверка после выполнения
1. Поищи `grep -r "callApi('editChatPhoto'" web/src/` — убедись что все вызовы теперь резолвятся
2. Поищи `grep -r "callApi('toggleChatArchived'" web/src/` — аналогично
3. Открой `index.ts` и убедись что новые exports не дублируют существующие
4. Убедись что все импорты корректны (нет circular dependencies)
5. Запусти `cd web && npx webpack --mode development` для проверки компиляции
