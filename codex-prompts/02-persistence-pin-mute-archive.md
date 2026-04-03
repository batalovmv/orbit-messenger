# Задача: Персистенция pin/mute/archive — бэкенд + фронтенд

## Роль и поведение
Ты — fullstack-разработчик проекта Orbit Messenger. Работай автономно от начала до конца. Пиши и бэкенд (Go/Fiber), и фронтенд (TypeScript/Teact). Не останавливайся на вопросах — принимай архитектурные решения сам. Следуй конвенциям из CLAUDE.md.

## Контекст
Три фичи работают только client-side (state пропадает при reload страницы):
- **Pin chat** — `toggleChatPinned` в `web/src/api/saturn/methods/chats.ts:545`
- **Mute chat** — `setChatMuted` в `web/src/api/saturn/methods/chats.ts:554`
- **Archive chat** — `archiveChat/unarchiveChat` в `web/src/api/saturn/methods/chats.ts:528-543`

Все три просто шлют локальный `sendApiUpdate` без вызова API. При reload — состояние теряется.

## Архитектура решения

### Backend (Go, сервис messaging)

В таблице `chat_members` уже есть колонки для каждого юзера в чате. Нужно добавить три колонки (если нет):

**Миграция** `migrations/018_member_preferences.sql`:
```sql
ALTER TABLE chat_members
  ADD COLUMN IF NOT EXISTS is_pinned BOOLEAN DEFAULT false,
  ADD COLUMN IF NOT EXISTS is_muted BOOLEAN DEFAULT false,
  ADD COLUMN IF NOT EXISTS is_archived BOOLEAN DEFAULT false;
```

**Endpoint:** `PATCH /api/v1/chats/:id/members/me` — обновление preferences текущего юзера в чате.

Request body (partial update, все поля optional):
```json
{
  "is_pinned": true,
  "is_muted": false,
  "is_archived": false
}
```

Response: 200 OK с обновлённым объектом member.

**Файлы бэкенда:**
- `services/messaging/internal/store/chat_store.go` — добавить метод `UpdateMemberPreferences(ctx, chatID, userID, prefs)`
- `services/messaging/internal/service/chat_service.go` — бизнес-логика
- `services/messaging/internal/handler/chat_handler.go` — HTTP handler для `PATCH /chats/:id/members/me`
- `services/messaging/cmd/main.go` — зарегистрировать роут

**Конвенции бэкенда:**
- getUserID из `X-User-ID` header (не JWT middleware)
- Ответ через `response.JSON(c, status, data)` — никогда `c.JSON()`
- Ошибки через `apperror.BadRequest/NotFound/etc`
- Параметризованные SQL `$1, $2` — никакого fmt.Sprintf

**Также:** в `GET /chats` (список чатов) — уже возвращается `chat_members` data. Убедись что `is_pinned`, `is_muted`, `is_archived` включены в response.

### Frontend

**1. Saturn API метод** в `web/src/api/saturn/methods/chats.ts`:
```ts
async function updateMemberPreferences(chatId: string, prefs: {
  is_pinned?: boolean;
  is_muted?: boolean;
  is_archived?: boolean;
}) {
  await client.request('PATCH', `/chats/${chatId}/members/me`, prefs);
}
```

**2. Обновить `toggleChatPinned`:**
```ts
export async function toggleChatPinned({ chatId, isPinned }: { chatId: string; isPinned: boolean }) {
  await updateMemberPreferences(chatId, { is_pinned: isPinned });
  sendApiUpdate({
    '@type': 'updateChat',
    id: chatId,
    chat: { isPinned } as any,
  });
}
```

**3. Аналогично обновить `setChatMuted` и `archiveChat/unarchiveChat`** — сначала API вызов, потом локальный update.

**4. При загрузке чатов** (`fetchChats` в `chats.ts`) — убедись что `is_pinned`, `is_muted`, `is_archived` из Saturn response маппятся в поля TG Web A chat объекта (`isPinned`, `isMuted`, `folderId`).

## Самопроверка
1. Запусти миграцию: `psql -f migrations/018_member_preferences.sql`
2. Проверь Go компиляцию: `cd services/messaging && go build ./...`
3. Проверь фронт компиляцию: `cd web && npx webpack --mode development`
4. Протестируй PATCH endpoint через curl:
```bash
curl -X PATCH http://localhost:8080/api/v1/chats/{chatId}/members/me \
  -H "Authorization: Bearer {token}" \
  -H "Content-Type: application/json" \
  -d '{"is_pinned": true}'
```
5. Убедись что при reload страницы pin/mute/archive сохраняются
