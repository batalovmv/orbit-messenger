# Codex Tasks — Audit Fix Prompts

Каждый промпт — самостоятельная задача для Codex. Принимай все решения сам, не задавай вопросов.

---

## Task 1: Saturn Method Aliases (Frontend — index.ts)

**Файл:** `web/src/api/saturn/methods/index.ts`

**Контекст:** Saturn API layer — это прослойка между TG Web A action'ами и нашим бэкендом. Когда action вызывает `callApi('methodName', ...)`, система ищет экспортированную функцию с таким именем в `index.ts`. Если метод не найден, `callApi` в `init.ts` (строка 30-37) молча возвращает `undefined` — без ошибки, без fallback. Это означает что многие кнопки и экраны UI выглядят рабочими, но фактически делают noop.

**Проблема:** TG Web A action'ы вызывают методы по именам из GramJS API, а Saturn реализовал те же функции под другими именами. Нужно добавить алиасы-обёртки.

**Что сделать:**

Добавь в `index.ts` следующие экспортируемые функции-алиасы. Каждый алиас должен просто вызывать существующий Saturn метод с правильным маппингом параметров. Существующие методы импортируются из соответствующих файлов в `./chats`, `./messages`, `./management` и т.д.

```typescript
// 1. Chat title/about editing
// Action вызывает: callApi('updateChatTitle', chat, title)
// Saturn имеет: editChatTitle({ chatId, title }) в chats.ts:377
export async function updateChatTitle(chat: ApiChat, title: string) {
  return editChatTitle({ chatId: chat.id, title });
}

// Action вызывает: callApi('updateChatAbout', chat, about)
// Saturn имеет: editChatAbout({ chatId, about }) в chats.ts:382
export async function updateChatAbout(chat: ApiChat, about: string) {
  return editChatAbout({ chatId: chat.id, about });
}

// 2. Invite link flow
// Action вызывает: callApi('checkChatInvite', hash)
// Saturn имеет: fetchChatInviteInfo({ hash }) в chats.ts:464
// Также убери 'checkChatInvite' из SILENCED_METHODS в init.ts (строка 13)
export async function checkChatInvite(hash: string) {
  return fetchChatInviteInfo({ hash });
}

// Action вызывает: callApi('importChatInvite', hash)
// Saturn имеет: joinChat({ hash }) в chats.ts:469
export async function importChatInvite(hash: string) {
  return joinChat({ hash });
}

// Action вызывает: callApi('exportChatInvite', { chat, ... })
// Saturn имеет: exportChatInviteLink({ chatId, ... }) в chats.ts:444
export async function exportChatInvite({
  chat, title, expireDate, usageLimit, isRequestNeeded
}: {
  chat: ApiChat; title?: string; expireDate?: number;
  usageLimit?: number; isRequestNeeded?: boolean;
}) {
  return exportChatInviteLink({
    chatId: chat.id, title, expireDate, usageLimit, isRequestNeeded,
  });
}

// 3. Message link
// Action вызывает: callApi('exportMessageLink', { chat, messageId })
// Saturn имеет: fetchMessageLink({ chatId, messageId }) в messages.ts:1128
export function exportMessageLink({
  chat, messageId,
}: { chat: ApiChat; messageId: number }) {
  return fetchMessageLink({ chatId: chat.id, messageId });
}

// 4. Channel join/leave/delete
// Action вызывает: callApi('joinChannel', { channelId })
// Saturn имеет: joinChat → но joinChannel в TG принимает channelId, не hash
// Реализуй как POST /chats/:id/join
export async function joinChannel({ channelId }: { channelId: string }) {
  return request('POST', `/chats/${channelId}/join`);
}

// Action вызывает: callApi('leaveChannel', { channelId })
// Saturn имеет: leaveChat({ chatId }) в chats.ts:390
export async function leaveChannel({ channelId }: { channelId: string }) {
  return leaveChat({ chatId: channelId });
}

// Action вызывает: callApi('deleteChannel', { channelId })
// Saturn имеет: deleteChat({ chatId }) в chats.ts:386
export async function deleteChannel({ channelId }: { channelId: string }) {
  return deleteChat({ chatId: channelId });
}

// 5. Delete chat user (remove member by action)
// Action вызывает: callApi('deleteChatUser', { chat, user })
// Saturn имеет: нужен DELETE /chats/:id/members/:userId
export async function deleteChatUser({ chat, user }: { chat: ApiChat; user: ApiUser }) {
  return request('DELETE', `/chats/${chat.id}/members/${user.id}`);
}
```

**Также в init.ts:**
- Убери `'checkChatInvite'` из массива `SILENCED_METHODS` (строка 13) — теперь метод реализован и warning подавлять не нужно.
- Убери `'fetchTopReactions'` из `SILENCED_METHODS` если он там есть и метод реализован в Saturn.

**Проверь:** что все импорты (`editChatTitle`, `editChatAbout`, `fetchChatInviteInfo`, `joinChat`, `exportChatInviteLink`, `leaveChat`, `deleteChat`, `fetchMessageLink`, `request`) доступны в scope файла. `request` импортируется из `./client`. `ApiChat` и `ApiUser` из `../../types`.

**Не трогай:** существующие функции, не меняй их сигнатуры. Только добавляй новые экспорты.

**Ограничения:** Не задавай вопросов, принимай решения сам. Если нужен тип — используй существующие типы из проекта. Следи за TypeScript strict mode.

---

## Task 2: Profile Edit Field Mapping (Frontend)

**Файлы:**
- `web/src/global/actions/api/settings.ts` (строки 59-128)
- `web/src/api/saturn/methods/users.ts` (строка 174)

**Контекст:** Экран редактирования профиля вызывает `callApi('updateProfile', { firstName, lastName, about })`, но Saturn метод `updateProfile` принимает `{ displayName, bio }`. Поля не совпадают — `displayName` = undefined, `bio` = undefined, профиль не обновляется.

**Что сделать:**

В `web/src/global/actions/api/settings.ts` в action handler `'updateProfile'` (примерно строка 59):

Текущий код (строки 72-73):
```typescript
if (firstName || lastName || about) {
  const result = await callApi('updateProfile', { firstName, lastName, about });
```

Замени на:
```typescript
if (firstName || lastName || about) {
  // Saturn API expects displayName (combined) and bio (not about)
  const displayName = [firstName, lastName].filter(Boolean).join(' ') || firstName || '';
  const result = await callApi('updateProfile', { displayName, bio: about });
```

Также ниже (примерно строка 88-94) есть optimistic state update:
```typescript
global = updateUser(global, currentUser.id, { firstName, lastName });
global = updateUserFullInfo(global, currentUser.id, { bio: about });
```
Это оставь как есть — это локальный state для UI, не для бэкенда.

**Не трогай:** `users.ts:updateProfile` — его сигнатура правильная. Проблема только в caller'е.

---

## Task 3: checkUsername / updateUsername (Frontend)

**Файлы:**
- `web/src/api/saturn/methods/users.ts`
- `web/src/api/saturn/methods/index.ts`

**Контекст:** `checkUsername` вызывается из `UsernameInput.tsx` на каждый keystroke, `updateUsername` — при сохранении. Оба метода отсутствуют в Saturn. Бэкенд `PUT /users/me` уже принимает все поля профиля включая username через display_name flow, но отдельного endpoint для username нет.

**Что сделать:**

1. В `users.ts` добавь:

```typescript
export async function checkUsername(username: string): Promise<{ result?: boolean; error?: string }> {
  try {
    // Saturn не имеет отдельного endpoint для проверки username.
    // Валидируем локально: 5-32 символа, a-z0-9_, не начинается с цифры
    const usernameRegex = /^[a-zA-Z][a-zA-Z0-9_]{4,31}$/;
    if (!usernameRegex.test(username)) {
      return { result: false, error: 'Username must be 5-32 characters, start with a letter, and contain only letters, numbers, and underscores' };
    }

    // Проверяем через поиск — если юзер с таким username существует и это не мы
    const response = await request<{ data: any[] }>('GET', `/users?q=${encodeURIComponent(username)}&limit=1`);
    const taken = response?.data?.some((u: any) =>
      u.username?.toLowerCase() === username.toLowerCase()
    );
    return { result: !taken };
  } catch {
    return { result: undefined, error: 'Failed to check username' };
  }
}

export async function updateUsername(username: string): Promise<boolean | undefined> {
  try {
    await request('PUT', '/users/me', { username });
    return true;
  } catch {
    return undefined;
  }
}
```

2. В `index.ts` добавь экспорты:
```typescript
export { checkUsername, updateUsername } from './users';
```

**Важно:** Бэкенд PUT /users/me может не иметь поля `username` — это ок, задача Task 14 (отдельная) добавит его. Сейчас важно что фронт перестанет крашиться при попытке destructure undefined.

---

## Task 4: DELETE /chats/:id/members/me (Backend)

**Файл:** `services/messaging/internal/handler/chat_handler.go`

**Контекст:** Фронтенд `leaveChat()` отправляет `DELETE /chats/:id/members/me`. На бэке зарегистрирован только `DELETE /chats/:id/members/:userId` (строка 39). При получении "me" как userId, `uuid.Parse("me")` падает с 400.

**Что сделать:**

В `chat_handler.go`, в функцию `RemoveMember` (строка 223), добавь обработку `"me"`:

```go
func (h *ChatHandler) RemoveMember(c *fiber.Ctx) error {
    uid, err := getUserID(c)
    if err != nil {
        return response.Error(c, err)
    }

    chatID, err := uuid.Parse(c.Params("id"))
    if err != nil {
        return response.Error(c, apperror.BadRequest("Invalid chat ID"))
    }

    // Support "me" as userId alias for self-leave
    userIDParam := c.Params("userId")
    var targetID uuid.UUID
    if userIDParam == "me" {
        targetID = uid
    } else {
        targetID, err = uuid.Parse(userIDParam)
        if err != nil {
            return response.Error(c, apperror.BadRequest("Invalid user ID"))
        }
    }

    if err := h.svc.RemoveMember(c.Context(), chatID, uid, targetID); err != nil {
        return response.Error(c, err)
    }

    return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}
```

**Не надо:** регистрировать отдельный route для `/members/me` — модификация handler'а достаточна и проще.

---

## Task 5: PUT /users/me Partial Update (Backend)

**Файл:** `services/messaging/internal/handler/user_handler.go`

**Контекст:** Handler `UpdateProfile` (строка 62) объявляет `DisplayName string` (не pointer) и валидирует его через `RequireString` (строка 80). Это значит что avatar-only или bio-only update невозможен — всегда нужен display_name. Фронтенд шлёт partial body.

**Что сделать:**

1. Сделай `DisplayName` pointer'ом в request struct:

```go
var req struct {
    DisplayName       *string `json:"display_name"`
    Bio               *string `json:"bio"`
    Phone             *string `json:"phone"`
    AvatarURL         *string `json:"avatar_url"`
    CustomStatus      *string `json:"custom_status"`
    CustomStatusEmoji *string `json:"custom_status_emoji"`
}
```

2. Валидируй `DisplayName` только если он передан:

```go
if req.DisplayName != nil {
    if vErr := validator.RequireString(*req.DisplayName, "display_name", 1, 64); vErr != nil {
        return response.Error(c, vErr)
    }
}
```

3. В вызове `h.svc.UpdateProfile` передай `req.DisplayName` как `*string` вместо `string`. Если service layer принимает `string` — измени сигнатуру `UpdateProfile` в service и store:
   - Если `DisplayName` = nil, не обновляй поле (оставь текущее значение в БД)
   - Если `DisplayName` != nil, обновляй

4. В service layer (`user_service.go`) и store layer (`user_store.go`):
   - Измени `UpdateProfile` чтобы принимал `*string` для `displayName`
   - В SQL используй `COALESCE($2, display_name)` или условную логику для nullable полей
   - Самый простой подход: собери dynamic UPDATE query только из non-nil полей

**Паттерн для dynamic partial update в store:**

```go
func (s *userStore) UpdateProfile(ctx context.Context, userID uuid.UUID,
    displayName *string, bio *string, phone *string, avatarURL *string,
    customStatus *string, customStatusEmoji *string) (*model.User, error) {

    setClauses := []string{"updated_at = NOW()"}
    args := []interface{}{userID}
    argIdx := 2

    if displayName != nil {
        setClauses = append(setClauses, fmt.Sprintf("display_name = $%d", argIdx))
        args = append(args, *displayName)
        argIdx++
    }
    if bio != nil {
        setClauses = append(setClauses, fmt.Sprintf("bio = $%d", argIdx))
        args = append(args, *bio)
        argIdx++
    }
    // ... аналогично для phone, avatarURL, customStatus, customStatusEmoji

    query := fmt.Sprintf("UPDATE users SET %s WHERE id = $1 RETURNING *", strings.Join(setClauses, ", "))
    // ...
}
```

**Не забудь:** обновить interface `UserStore` если он есть, и mock'и в тестах.

---

## Task 6: CanSendMedia Check in Scheduled Messages (Backend)

**Файл:** `services/messaging/internal/service/scheduled_service.go`

**Контекст:** Метод `Schedule()` (строка 91) проверяет только `CanSendMessages`, но не `CanSendMedia`. Обычная отправка в `message_service.go:609` проверяет `CanSendMedia`. Это позволяет юзеру без права на медиа отправить медиа через scheduled flow. Та же проблема в `deliver()` (строка 319).

**Что сделать:**

1. В `Schedule()` после проверки `CanSendMessages` (строка 91-93), добавь:

```go
if !permissions.CanPerform(member.Role, chat.Type, member.Permissions, chat.DefaultPermissions, permissions.CanSendMessages) {
    return nil, apperror.Forbidden("You don't have permission to send messages")
}

// Check media permission if media is attached
if len(input.MediaIDs) > 0 {
    if !permissions.CanPerform(member.Role, chat.Type, member.Permissions, chat.DefaultPermissions, permissions.CanSendMedia) {
        return nil, apperror.Forbidden("You don't have permission to send media")
    }
}
```

2. В `deliver()` после аналогичной проверки `CanSendMessages` (строка 319-321), добавь:

```go
if !permissions.CanPerform(member.Role, chat.Type, member.Permissions, chat.DefaultPermissions, permissions.CanSendMessages) {
    return nil, apperror.Forbidden("Sender no longer has permission to send messages")
}

// Check media permission at delivery time
if len(scheduledMsg.MediaIDs) > 0 {
    if !permissions.CanPerform(member.Role, chat.Type, member.Permissions, chat.DefaultPermissions, permissions.CanSendMedia) {
        return nil, apperror.Forbidden("Sender no longer has permission to send media")
    }
}
```

**Проверь:** что `permissions.CanSendMedia` существует в `pkg/permissions/`. Если нет — добавь константу по аналогии с `CanSendMessages`.

---

## Task 7: Bilateral Block Check in Scheduled Messages (Backend)

**Файл:** `services/messaging/internal/service/scheduled_service.go`

**Контекст:** И в `Schedule()` (строка 96-113), и в `deliver()` (строка 323-341) проверяется только одно направление блокировки: "получатель заблокировал отправителя". Обычная отправка в `message_service.go` проверяет оба направления. Это позволяет отправителю, который сам заблокировал получателя, отправить ему scheduled message.

**Что сделать:**

В обоих местах (Schedule и deliver) после цикла по members добавь вторую проверку:

Текущий код (упрощённо):
```go
for _, m := range members {
    if m.UserID == senderID {
        continue
    }
    blocked, err := s.blockedStore.IsBlocked(ctx, m.UserID, senderID) // receiver blocked sender
    if err != nil { ... }
    if blocked { return nil, apperror.Forbidden("...") }
}
```

Добавь после первой проверки:
```go
for _, m := range members {
    if m.UserID == senderID {
        continue
    }
    // Check: receiver blocked sender
    blocked, err := s.blockedStore.IsBlocked(ctx, m.UserID, senderID)
    if err != nil {
        return nil, fmt.Errorf("check blocked: %w", err)
    }
    if blocked {
        return nil, apperror.Forbidden("You cannot send messages to this user")
    }
    // Check: sender blocked receiver
    blockedBySender, err := s.blockedStore.IsBlocked(ctx, senderID, m.UserID)
    if err != nil {
        return nil, fmt.Errorf("check sender block: %w", err)
    }
    if blockedBySender {
        return nil, apperror.Forbidden("You have blocked this user")
    }
}
```

Сделай это в ОБОИХ методах: `Schedule()` и `deliver()`.

---

## Task 8: Poll Vote Transaction (Backend)

**Файлы:**
- `services/messaging/internal/service/poll_service.go`
- `services/messaging/internal/store/poll_store.go`

**Контекст:** В `Vote()` (poll_service.go строка 219) для single-choice poll делается `UnvoteAll()`, затем в цикле `Vote()` — отдельными SQL запросами без транзакции. При concurrent запросах от одного юзера возможны multiple votes в single-choice poll.

**Текущий код store:**
- `UnvoteAll()` (poll_store.go:252): `DELETE FROM poll_votes WHERE poll_id = $1 AND user_id = $2`
- `Vote()` (poll_store.go:222): `INSERT INTO poll_votes ... ON CONFLICT DO NOTHING`

**Что сделать:**

Вариант 1 (рекомендуемый) — атомарная операция в store:

1. Добавь в `poll_store.go` новый метод `VoteAtomic`:

```go
func (s *pollStore) VoteAtomic(ctx context.Context, pollID, userID uuid.UUID, optionIDs []uuid.UUID, isMultiple bool) error {
    tx, err := s.pool.Begin(ctx)
    if err != nil {
        return fmt.Errorf("begin tx: %w", err)
    }
    defer tx.Rollback(ctx)

    // For single-choice: clear existing votes first
    if !isMultiple {
        _, err = tx.Exec(ctx,
            `DELETE FROM poll_votes WHERE poll_id = $1 AND user_id = $2`,
            pollID, userID,
        )
        if err != nil {
            return fmt.Errorf("clear votes: %w", err)
        }
    }

    for _, optionID := range optionIDs {
        _, err = tx.Exec(ctx,
            `INSERT INTO poll_votes (poll_id, option_id, user_id)
             SELECT p.id, po.id, $3
             FROM polls p
             JOIN poll_options po ON po.id = $2 AND po.poll_id = p.id
             WHERE p.id = $1 AND p.is_closed = FALSE
             ON CONFLICT (poll_id, user_id, option_id) DO NOTHING`,
            pollID, optionID, userID,
        )
        if err != nil {
            return fmt.Errorf("vote: %w", err)
        }
    }

    return tx.Commit(ctx)
}
```

2. Добавь `VoteAtomic` в PollStore interface.

3. В `poll_service.go` замени блок строк 219-229:

```go
// Старый код:
// if !poll.IsMultiple {
//     if err := s.polls.UnvoteAll(ctx, poll.ID, userID); err != nil { ... }
// }
// for _, optionID := range uniqueOptionIDs {
//     if err := s.polls.Vote(ctx, poll.ID, optionID, userID); err != nil { ... }
// }

// Новый код:
if err := s.polls.VoteAtomic(ctx, poll.ID, userID, uniqueOptionIDs, poll.IsMultiple); err != nil {
    return nil, fmt.Errorf("atomic vote: %w", err)
}
```

4. Обнови mock в тестах (если есть `mock_stores_test.go` в handler/) — добавь `VoteAtomicFn`.

---

## Task 9: Reaction Transaction (Backend)

**Файлы:**
- `services/messaging/internal/service/reaction_service.go`
- `services/messaging/internal/store/reaction_store.go`

**Контекст:** В `AddReaction` (reaction_service.go строка 78) делается `RemoveAllByUser()` + `Add()` без транзакции. Concurrent запросы могут оставить несколько emoji от одного юзера.

**Что сделать:**

1. Добавь в `reaction_store.go` атомарный метод:

```go
func (s *reactionStore) ReplaceUserReaction(ctx context.Context, messageID, userID uuid.UUID, emoji string) error {
    tx, err := s.pool.Begin(ctx)
    if err != nil {
        return fmt.Errorf("begin tx: %w", err)
    }
    defer tx.Rollback(ctx)

    _, err = tx.Exec(ctx,
        `DELETE FROM message_reactions WHERE message_id = $1 AND user_id = $2`,
        messageID, userID,
    )
    if err != nil {
        return fmt.Errorf("remove existing: %w", err)
    }

    _, err = tx.Exec(ctx,
        `INSERT INTO message_reactions (message_id, user_id, emoji)
         VALUES ($1, $2, $3)
         ON CONFLICT (message_id, user_id, emoji) DO NOTHING`,
        messageID, userID, emoji,
    )
    if err != nil {
        return fmt.Errorf("add reaction: %w", err)
    }

    return tx.Commit(ctx)
}
```

2. Добавь `ReplaceUserReaction` в `ReactionStore` interface.

3. В `reaction_service.go` замени строки 78-84:

```go
// Старый код:
// if err := s.reactions.RemoveAllByUser(ctx, messageID, userID); err != nil { ... }
// if err := s.reactions.Add(ctx, messageID, userID, emoji); err != nil { ... }

// Новый код:
if err := s.reactions.ReplaceUserReaction(ctx, messageID, userID, emoji); err != nil {
    return fmt.Errorf("replace reaction: %w", err)
}
```

4. Обнови mock в тестах — добавь `ReplaceUserReactionFn`.

---

## Task 10: DM chat_created NATS Publish (Backend)

**Файл:** `services/messaging/internal/service/chat_service.go`

**Контекст:** `CreateDirectChat()` (строка 134) не публикует NATS lifecycle event при создании DM. `CreateChat()` (группы/каналы, строка 204-210) делает `s.nats.Publish(...)`. Из-за этого получатель первого DM не видит чат в реалтайме.

**Что сделать:**

В конце `CreateDirectChat()`, перед `return chat, nil`, добавь NATS publish по аналогии с CreateChat:

```go
func (s *ChatService) CreateDirectChat(ctx context.Context, userID, otherUserID uuid.UUID) (*model.Chat, error) {
    // ... existing code ...

    chat, err := s.chats.CreateDirectChat(ctx, userID, otherUserID)
    if err != nil {
        return nil, fmt.Errorf("create DM: %w", err)
    }

    // Publish lifecycle event so both users get the new chat via WebSocket
    memberIDs := []string{userID.String(), otherUserID.String()}
    s.nats.Publish(
        fmt.Sprintf("orbit.chat.%s.lifecycle", chat.ID),
        "chat_created",
        chat,
        memberIDs,
        userID.String(),
    )

    return chat, nil
}
```

**Проверь:** что `s.nats` не nil. Если в конструкторе ChatService nats publisher optional — оберни в `if s.nats != nil { ... }`.

---

## Task 11: UnpinAll NATS Event (Backend)

**Файл:** `services/messaging/internal/service/message_service.go`

**Контекст:** `UnpinAll()` (строка 515) не публикует NATS/WS событие. После unpin all у других участников чата остаётся stale pinned state.

**Что сделать:**

В конце `UnpinAll()`, после `return s.messages.UnpinAll(ctx, chatID)`, добавь NATS publish:

```go
func (s *MessageService) UnpinAll(ctx context.Context, chatID, userID uuid.UUID) error {
    // ... existing permission checks ...

    if err := s.messages.UnpinAll(ctx, chatID); err != nil {
        return err
    }

    // Notify all chat members about unpin
    memberIDs, err := s.chats.GetMemberIDs(ctx, chatID)
    if err != nil {
        slog.Error("failed to get member IDs for unpin_all", "error", err, "chat_id", chatID)
        // Don't fail the unpin operation itself
    } else {
        s.nats.Publish(
            fmt.Sprintf("orbit.chat.%s.message.updated", chatID),
            "unpin_all",
            map[string]interface{}{
                "chat_id": chatID.String(),
            },
            memberIDs,
            userID.String(),
        )
    }

    return nil
}
```

**Проверь:** сигнатуру `GetMemberIDs` — она может возвращать `[]string` или `[]uuid.UUID`. Используй тот формат, который ожидает `s.nats.Publish()`.

---

## Task 12: Typing Multi-User Support (Frontend)

**Файлы:**
- `web/src/api/saturn/updates/wsHandler.ts` (строки 275-304)
- `web/src/global/actions/apiUpdaters/chats.ts` (строки 177-192)

**Контекст:** В state хранится один `typingStatus` на чат. Если печатают двое, второй затирает первого. `stop_typing` очищает весь status без проверки user_id.

**Что сделать:**

1. В `wsHandler.ts`, изменить `handleStopTyping`:

Текущий код (строка 293-304):
```typescript
case 'stop_typing': {
    const chatId = data.chat_id as string;
    const userId = data.user_id as string | undefined;
    if (userId === currentUserId) return;
    sendApiUpdate({
        '@type': 'updateChatTypingStatus',
        id: chatId,
        typingStatus: undefined,
    });
}
```

Измени на — передавай userId чтобы updater мог проверить кого очищать:
```typescript
case 'stop_typing': {
    const chatId = data.chat_id as string;
    const userId = data.user_id as string | undefined;
    if (userId === currentUserId) return;
    sendApiUpdate({
        '@type': 'updateChatTypingStatus',
        id: chatId,
        typingStatus: undefined,
        userId, // Pass userId so updater can clear only this user's typing
    });
}
```

2. В `apiUpdaters/chats.ts`, измени handler `updateChatTypingStatus`:

Текущий код очищает typing вслепую. Измени чтобы проверял userId:

```typescript
case 'updateChatTypingStatus': {
    const { id, threadId = MAIN_THREAD_ID, typingStatus, userId } = update;

    // If clearing typing status, only clear if the stored status belongs to this user
    if (!typingStatus && userId) {
        const currentTypingStatus = selectThreadLocalStateParam(global, id, threadId, 'typingStatus');
        if (currentTypingStatus && currentTypingStatus.userId !== userId) {
            // Someone else is still typing — don't clear
            return undefined;
        }
    }

    global = replaceThreadLocalStateParam(global, id, threadId, 'typingStatus', typingStatus);
    setGlobal(global);

    setTimeout(() => {
        global = getGlobal();
        const currentTypingStatus = selectThreadLocalStateParam(global, id, threadId, 'typingStatus');
        if (typingStatus && currentTypingStatus && typingStatus.timestamp === currentTypingStatus.timestamp) {
            global = replaceThreadLocalStateParam(global, id, threadId, 'typingStatus', undefined);
            setGlobal(global);
        }
    }, TYPING_STATUS_CLEAR_DELAY);
    return undefined;
}
```

**Примечание:** Это минимальный фикс — хранение всё ещё одного юзера на чат. Полное решение (массив печатающих) — отдельная задача. Этот фикс решает конкретный баг: stop_typing от A не стирает typing от B.

---

## Task 13: auth_ok Handling + WS Error Frame (Frontend)

**Файл:** `web/src/api/saturn/methods/client.ts`

**Контекст:** Клиент ставит `connectionStateReady` на `ws.onopen` (строка 277), до подтверждения auth. `auth_ok` и `error` frames от сервера игнорируются. При revoked token UI показывает Ready и стартует sync.

**Что сделать:**

1. Убери `connectionStateReady` из `ws.onopen`. Отправляй его только после получения `auth_ok`.

2. Добавь обработку `auth_ok` и `error` в message handler.

Текущий `ws.onopen` (строка 270-284):
```typescript
ws.onopen = () => {
    console.log('[Saturn WS] Connected, sending auth frame');
    ws!.send(JSON.stringify({ type: 'auth', data: { token: accessToken } }));
    wsReconnectDelay = WS_RECONNECT_BASE_MS;
    startPing();
    onUpdate?.({ '@type': 'updateConnectionState', connectionState: 'connectionStateReady' });
    if (wsHasConnectedBefore) {
        onReconnect?.();
    }
    wsHasConnectedBefore = true;
};
```

Замени на:
```typescript
ws.onopen = () => {
    console.log('[Saturn WS] Connected, sending auth frame');
    ws!.send(JSON.stringify({ type: 'auth', data: { token: accessToken } }));
    wsReconnectDelay = WS_RECONNECT_BASE_MS;
    startPing();
    // Don't set connectionStateReady here — wait for auth_ok
    onUpdate?.({ '@type': 'updateConnectionState', connectionState: 'connectionStateConnecting' });
};
```

В `ws.onmessage` handler (строка 286), перед `handleWsMessage(msg)`:
```typescript
ws.onmessage = (event) => {
    try {
        const msg: SaturnWsMessage = JSON.parse(event.data as string);
        if (msg.type !== 'pong') console.log('[Saturn WS] Received:', msg.type);

        if (msg.type === 'auth_ok') {
            onUpdate?.({ '@type': 'updateConnectionState', connectionState: 'connectionStateReady' });
            if (wsHasConnectedBefore) {
                onReconnect?.();
            }
            wsHasConnectedBefore = true;
            return;
        }

        if (msg.type === 'error') {
            console.error('[Saturn WS] Server error:', msg.data);
            const errorMsg = (msg.data as any)?.message || 'Unknown error';
            // If auth failed, don't reconnect with same token
            if (errorMsg.includes('auth') || errorMsg.includes('token') || errorMsg.includes('unauthorized')) {
                onUpdate?.({ '@type': 'updateConnectionState', connectionState: 'connectionStateBroken' });
                ws?.close();
                return;
            }
        }

        handleWsMessage(msg);
    } catch {
        // Ignore malformed messages
    }
};
```

**Также:** убери `if (msg.type !== 'pong') console.log('[Saturn WS] Received:', msg.type, msg.data);` — в production не должен логировать msg.data (security finding). Оставь только `msg.type`.

---

## Task 14: media_ready Updater (Frontend)

**Файлы:**
- `web/src/api/saturn/updates/wsHandler.ts` (строки 339-355)
- `web/src/global/actions/apiUpdaters/messages.ts`

**Контекст:** `handleMediaReady` в wsHandler.ts (строка 339) эмитит `updateMessageMediaReady` через `sendApiUpdate` с `as any` cast. Но ни один updater этот тип не обрабатывает — событие уходит в пустоту.

**Что сделать:**

Вместо кастомного update type, используй существующий механизм. `media_ready` event содержит `media_id`, `width`, `height`, `duration_seconds`, `has_thumbnail`. Нужно найти сообщение по media_id и обновить его.

1. В `wsHandler.ts`, замени `handleMediaReady`:

```typescript
async function handleMediaReady(data: Record<string, unknown>) {
    const mediaId = data.media_id as string | undefined;
    if (!mediaId) return;

    // Fetch updated message info from server
    // media_ready means async processing finished — thumbnails/dimensions are now available
    // The simplest approach: trigger a message refetch if we can find which message uses this media
    const { fetchMessageByMediaId } = await import('../methods/messages');
    if (typeof fetchMessageByMediaId === 'function') {
        fetchMessageByMediaId(mediaId);
    }
}
```

2. Если `fetchMessageByMediaId` не существует (скорее всего нет), используй альтернативный подход — просто обнови media attachment в текущем state:

```typescript
function handleMediaReady(data: Record<string, unknown>) {
    const mediaId = data.media_id as string | undefined;
    if (!mediaId) return;

    // Dispatch a generic update that the media processing handlers can pick up
    sendApiUpdate({
        '@type': 'updateMessageMediaReady' as any,
        mediaId,
        width: typeof data.width === 'number' ? data.width : undefined,
        height: typeof data.height === 'number' ? data.height : undefined,
        duration: typeof data.duration_seconds === 'number' ? data.duration_seconds : undefined,
        hasThumbnail: Boolean(data.has_thumbnail),
    } as any);
}
```

3. В `apiUpdaters/messages.ts`, добавь case для `updateMessageMediaReady` в switch:

```typescript
case 'updateMessageMediaReady': {
    const { mediaId, width, height, duration, hasThumbnail } = update as any;
    if (!mediaId) break;

    // Find message containing this media_id across all chats
    const allMessages = global.messages.byChatId;
    for (const chatId of Object.keys(allMessages)) {
        const chatMessages = allMessages[chatId]?.byId;
        if (!chatMessages) continue;
        for (const msgId of Object.keys(chatMessages)) {
            const msg = chatMessages[Number(msgId)];
            if (!msg?.content) continue;

            // Check if any media attachment matches this mediaId
            const hasMedia = msg.content.photo?.id === mediaId
                || msg.content.video?.id === mediaId
                || msg.content.document?.id === mediaId
                || msg.content.voice?.id === mediaId;

            if (hasMedia) {
                // Update the message's media dimensions/duration
                const updatedContent = { ...msg.content };
                if (updatedContent.photo && updatedContent.photo.id === mediaId) {
                    updatedContent.photo = {
                        ...updatedContent.photo,
                        ...(width && height ? {
                            sizes: [{ width, height, type: 'y' as const }],
                        } : {}),
                    };
                }
                if (updatedContent.video && updatedContent.video.id === mediaId) {
                    updatedContent.video = {
                        ...updatedContent.video,
                        ...(width ? { width } : {}),
                        ...(height ? { height } : {}),
                        ...(duration ? { duration } : {}),
                    };
                }

                global = updateChatMessage(global, chatId, Number(msgId), {
                    content: updatedContent,
                });
                setGlobal(global);
                return;
            }
        }
    }
    break;
}
```

**Важно:** Проверь какие типы и функции доступны в `apiUpdaters/messages.ts` (например `updateChatMessage` или `addMessages`). Используй существующие хелперы проекта.

---

## Task 15: chat_deleted / chat_member_removed — Proper Leave (Frontend)

**Файлы:**
- `web/src/api/saturn/updates/wsHandler.ts` (строки 90-118)
- Возможно `web/src/global/actions/apiUpdaters/chats.ts`

**Контекст:** При `chat_deleted` и `chat_member_removed` (current user) фронт просто ставит `{ isRestricted: true }`. Чат остаётся в списках как ghost. Нужен нормальный leave flow.

**Что сделать:**

1. В `wsHandler.ts`, замени case `chat_deleted`:

```typescript
case 'chat_deleted': {
    const payload = msg.data;
    const chatId = payload.chat_id as string;
    sendApiUpdate({
        '@type': 'updateChat',
        id: chatId,
        chat: {
            isRestricted: true,
            isNotJoined: true,
        } as any,
    });
    // Also remove from chat list
    sendApiUpdate({
        '@type': 'updateChatLeave',
        id: chatId,
    } as any);
    break;
}
```

2. Замени case `chat_member_removed` для current user:

```typescript
case 'chat_member_removed': {
    const payload = msg.data;
    const chatId = payload.chat_id as string;
    if (payload.user_id === currentUserId) {
        // Current user was removed — treat as leave
        sendApiUpdate({
            '@type': 'updateChat',
            id: chatId,
            chat: {
                isRestricted: true,
                isNotJoined: true,
                isForbidden: true,
            } as any,
        });
        sendApiUpdate({
            '@type': 'updateChatLeave',
            id: chatId,
        } as any);
    } else {
        // Someone else was removed — refresh chat info
        const { fetchFullChat } = await import('../methods/chats');
        fetchFullChat({ id: chatId });
    }
    break;
}
```

3. Если `updateChatLeave` не существует как update type в проекте, найди как TG Web A обрабатывает leave. Поищи `updateChatLeave` или `leaveChat` в `apiUpdaters/`. Если такого типа нет, используй альтернативный подход — вычисти чат из `listIds`:

```typescript
// Альтернатива если updateChatLeave не существует:
sendApiUpdate({
    '@type': 'updateChatJoin',
    id: chatId,
    // Passing isNotJoined removes from active list
    chat: { isNotJoined: true, leftDate: Date.now() / 1000 },
} as any);
```

Поищи в проекте как другие места обрабатывают leave (grep по `isNotJoined`, `leftChat`, `removeChat`). Используй тот же подход.

---

## Task 16: Slow Mode Atomic Check (Backend)

**Файл:** `services/messaging/internal/service/message_service.go`

**Контекст:** Slow mode реализован как check-then-act: EXISTS проверка (строка 147-163), затем SET после записи сообщения (строка 201-207). Между ними — окно для race. Также SET fail-open (ошибка логируется, сообщение сохраняется).

**Что сделать:**

Замени двухшаговый подход на атомарный SET NX перед записью сообщения:

```go
// Slow mode: atomic check-and-set BEFORE creating the message
if chat.SlowModeSeconds > 0 && !permissions.IsAdminOrOwner(member.Role) {
    redisKey := fmt.Sprintf("slowmode:%s:%s", chatID, senderID)
    ttl := time.Duration(chat.SlowModeSeconds) * time.Second

    // SET NX = set only if not exists. Returns true if set (not in cooldown), false if exists (in cooldown)
    wasSet, err := s.redis.SetNX(ctx, redisKey, "1", ttl).Result()
    if err != nil {
        slog.Error("redis slow mode check failed", "error", err)
        return nil, apperror.Internal("Slow mode check temporarily unavailable")
    }
    if !wasSet {
        // Key already exists — user is in cooldown
        remaining, ttlErr := s.redis.TTL(ctx, redisKey).Result()
        waitSec := int(remaining.Seconds())
        if ttlErr != nil || waitSec <= 0 {
            waitSec = chat.SlowModeSeconds
        }
        return nil, apperror.TooManyRequests(fmt.Sprintf("Slow mode: wait %d seconds", waitSec))
    }
}
```

И **убери** old slow mode SET после записи (строки 201-207) — он больше не нужен, cooldown уже установлен атомарно перед записью.

**Edge case:** Если запись сообщения после SET NX упадёт — юзер будет в cooldown без отправленного сообщения. Это приемлемый trade-off: лучше ложный cooldown чем обход slow mode. Cooldown истечёт через SlowModeSeconds.

---

## Task 17: Join-Requests Field Name Mapping (Frontend)

**Файл:** Найди фронтовый адаптер/маппер для join requests. Вероятно в `web/src/api/saturn/methods/chats.ts` или `management.ts`.

**Контекст:** Backend возвращает `created_at` и `message` (модель `JoinRequest` в `models.go:439`). Фронт читает `date` и `about`.

**Что сделать:**

Найди место где join requests маппятся (grep по `date` и `about` рядом с `join_request` или `joinRequest`). Измени маппер чтобы читал правильные поля:

```typescript
// Было:
{
    date: item.date,
    about: item.about,
}

// Стало:
{
    date: item.created_at, // Backend field name
    about: item.message,   // Backend field name
}
```

Если маппер использует destructuring, исправь соответственно. Если типы TypeScript не совпадают — исправь interface.

---

## Task 18: Emoji Status Stubs (Frontend)

**Файлы:**
- `web/src/api/saturn/methods/symbols.ts`
- `web/src/api/saturn/methods/users.ts`
- `web/src/api/saturn/methods/index.ts`

**Контекст:** `fetchDefaultStatusEmojis`, `fetchRecentEmojiStatuses`, `fetchCollectibleEmojiStatuses` вызываются UI но возвращают undefined. `updateEmojiStatus` отсутствует. Без них emoji status picker крашится при destructure.

**Что сделать:**

1. В `users.ts` добавь:

```typescript
export async function updateEmojiStatus(emojiStatus: any): Promise<boolean> {
    if (!emojiStatus) {
        // Clear status
        await request('PUT', '/users/me', { custom_status: null, custom_status_emoji: null });
    } else {
        await request('PUT', '/users/me', {
            custom_status: emojiStatus.title || '',
            custom_status_emoji: emojiStatus.documentId || emojiStatus.emoji || '',
        });
    }
    return true;
}
```

2. В `symbols.ts` добавь:

```typescript
export async function fetchDefaultStatusEmojis(): Promise<any> {
    // Return a basic set of emoji statuses for the picker
    return {
        set: { id: 'default-statuses', title: 'Default Statuses' },
        stickers: [],
    };
}

export async function fetchRecentEmojiStatuses(): Promise<any> {
    return {
        hash: '0',
        emojiStatuses: [],
    };
}

export async function fetchCollectibleEmojiStatuses(): Promise<any> {
    return {
        hash: '0',
        emojiStatuses: [],
    };
}
```

3. В `index.ts` добавь экспорты:
```typescript
export { updateEmojiStatus } from './users';
export { fetchDefaultStatusEmojis, fetchRecentEmojiStatuses, fetchCollectibleEmojiStatuses } from './symbols';
```

---

## Task 19: Poll Voters Cursor Pagination (Backend)

**Файлы:**
- `services/messaging/internal/handler/poll_handler.go`
- `services/messaging/internal/store/poll_store.go`

**Контекст:** `GetVoters` handler (poll_handler.go:97) игнорирует cursor query parameter. Store `GetVoters` (poll_store.go:288) не поддерживает cursor. Frontend шлёт cursor и ожидает реальную пагинацию.

**Что сделать:**

1. В `poll_handler.go`, добавь чтение cursor:

```go
func (h *PollHandler) GetVoters(c *fiber.Ctx) error {
    userID, err := getUserID(c)
    if err != nil {
        return response.Error(c, apperror.Unauthorized("Missing user context"))
    }
    msgID, err := uuid.Parse(c.Params("id"))
    if err != nil {
        return response.Error(c, apperror.BadRequest("Invalid message ID"))
    }
    optionID, err := uuid.Parse(c.Query("option_id"))
    if err != nil {
        return response.Error(c, apperror.BadRequest("Invalid option ID"))
    }

    limit := c.QueryInt("limit", 50)
    cursor := c.Query("cursor") // Add cursor support

    voters, nextCursor, hasMore, err := h.svc.GetPollVoters(c.Context(), msgID, userID, optionID, limit, cursor)
    if err != nil {
        return response.Error(c, err)
    }

    return response.Paginated(c, voters, nextCursor, hasMore)
}
```

2. В `poll_store.go` `GetVoters`, добавь cursor-based pagination:

```go
func (s *pollStore) GetVoters(ctx context.Context, pollID, optionID uuid.UUID, limit int, cursor string) ([]model.PollVote, string, bool, error) {
    if limit <= 0 || limit > 100 {
        limit = 50
    }

    args := []interface{}{pollID, optionID, limit + 1}
    query := `SELECT poll_id, option_id, user_id, voted_at
              FROM poll_votes
              WHERE poll_id = $1 AND option_id = $2`

    if cursor != "" {
        cursorTime, err := time.Parse(time.RFC3339Nano, cursor)
        if err == nil {
            query += ` AND voted_at < $4`
            args = append(args, cursorTime)
        }
    }

    query += ` ORDER BY voted_at DESC LIMIT $3`

    rows, err := s.pool.Query(ctx, query, args...)
    // ... scan rows ...

    hasMore := len(votes) > limit
    if hasMore {
        votes = votes[:limit]
    }

    var nextCursor string
    if hasMore && len(votes) > 0 {
        nextCursor = votes[len(votes)-1].VotedAt.Format(time.RFC3339Nano)
    }

    return votes, nextCursor, hasMore, nil
}
```

3. Обнови interface `PollStore` и service layer `GetPollVoters` чтобы они тоже прокидывали cursor/hasMore.

4. Обнови mock в тестах.

---

## Task 20: deleteHistory Saturn Method (Frontend)

**Файлы:**
- `web/src/api/saturn/methods/messages.ts`
- `web/src/api/saturn/methods/index.ts`

**Контекст:** `deleteHistory` вызывается из `messages.ts:1092` в actions. Saturn метода нет. UI локально чистит чат, но серверная история остаётся. Нужен API вызов.

**Что сделать:**

1. В `messages.ts` добавь:

```typescript
export async function deleteHistory({
    chat, shouldDeleteForAll,
}: {
    chat: ApiChat;
    shouldDeleteForAll?: boolean;
}) {
    try {
        // Use existing deleteChat for full history clear
        // Saturn backend doesn't have a separate "clear history" endpoint
        // The closest approach: delete all messages or use chat delete
        await request('DELETE', `/chats/${chat.id}/messages`, {
            clear_history: true,
            for_everyone: shouldDeleteForAll || false,
        });
    } catch (err) {
        // If clear_history endpoint doesn't exist, fall back to no-op
        // This is a known limitation — backend endpoint needed
        console.warn('[Saturn] deleteHistory not fully implemented on backend');
    }
}
```

2. В `index.ts` экспортируй:
```typescript
export { deleteHistory } from './messages';
```

**Примечание:** Бэкенд может не иметь endpoint для clear history. В этом случае метод корректно fallback'ится. Отдельная backend задача добавит endpoint.

---

## Task 21: 2FA Settings Wiring (Frontend)

**Файлы:**
- `web/src/api/saturn/methods/index.ts`
- Возможно новый файл `web/src/api/saturn/methods/twoFaSettings.ts`

**Контекст:** TG Web A 2FA screen вызывает `getPasswordInfo`, `checkPassword`, `updatePassword`, `clearPassword`. Saturn имеет TOTP 2FA через `/auth/2fa/*` endpoints. Нужно подключить.

**Что сделать:**

Создай `web/src/api/saturn/methods/twoFaSettings.ts`:

```typescript
import { request } from './client';

// Maps TG Web A 2FA API to Saturn TOTP endpoints

export async function getPasswordInfo() {
    try {
        const result = await request<{ enabled: boolean }>('GET', '/auth/2fa/status');
        return {
            hasPassword: result?.enabled || false,
            // Saturn TOTP doesn't use password hints/email like TG
            hint: undefined,
            hasRecoveryEmail: false,
            pendingResetDate: undefined,
        };
    } catch {
        return { hasPassword: false };
    }
}

export async function checkPassword(password: string) {
    try {
        const result = await request('POST', '/auth/2fa/verify', { code: password });
        return { success: true };
    } catch {
        return { success: false, error: 'Invalid code' };
    }
}

export async function updatePassword({
    currentPassword, password, hint, email, onUpdate,
}: {
    currentPassword?: string;
    password: string;
    hint?: string;
    email?: string;
    onUpdate?: Function;
}) {
    try {
        await request('POST', '/auth/2fa/enable', {
            code: currentPassword,
            secret: password, // TOTP secret
        });
        return true;
    } catch {
        return false;
    }
}

export async function clearPassword(currentPassword: string) {
    try {
        await request('POST', '/auth/2fa/disable', { code: currentPassword });
        return true;
    } catch {
        return false;
    }
}
```

В `index.ts` добавь экспорты:
```typescript
export { getPasswordInfo, checkPassword, updatePassword, clearPassword } from './twoFaSettings';
```

**Важно:** Проверь реальные Saturn endpoints для 2FA — они могут отличаться от `/auth/2fa/*`. Поищи в `services/auth/` route registration. Адаптируй URL'ы под реальные endpoints.

---

## Task 22: WS Production Logging Cleanup (Frontend)

**Файл:** `web/src/api/saturn/methods/client.ts`

**Контекст:** (Security finding) Все WS messages логируются в console вместе с `msg.data` (строка 288). Это утечка содержимого сообщений, presence и прочих чувствительных данных в browser console.

**Что сделать:**

В `ws.onmessage` (строка 286-295):

Текущий код:
```typescript
if (msg.type !== 'pong') console.log('[Saturn WS] Received:', msg.type, msg.data);
```

Замени на:
```typescript
if (DEBUG && msg.type !== 'pong') {
    // Only log event type in debug mode, never log payload data
    console.log('[Saturn WS] Received:', msg.type);
}
```

Убедись что `DEBUG` импортирован/определён. Вероятно из `../../config` или `process.env`. Поищи как DEBUG используется в других местах проекта.

**Также** в файле `web/src/util/notifications.tsx` (строка ~332) или `web/src/lib/notifications/pushNotification.ts` — убери логирование полного push subscription:

```typescript
// Было:
console.log('[Push] Subscription:', JSON.stringify(subscription));
// Стало:
if (DEBUG) console.log('[Push] Subscription endpoint registered');
```

---

## Task 23: messages_read UUID→seq Fallback (Frontend)

**Файл:** `web/src/api/saturn/updates/wsHandler.ts`

**Контекст:** `handleMessagesRead` (строка 239) дропает read state если UUID→seq mapping отсутствует. Это происходит после reconnect, page reload, или если сообщение не загружено.

**Что сделать:**

В `handleMessagesRead`, добавь fallback — если seq не найден, используй fetchFullChat для обновления read state:

Текущий код (примерно строки 239-273):
```typescript
function handleMessagesRead(data: ...) {
    const lastReadUuid = data.last_read_message_id;
    const seq = getMessageSeqNum(lastReadUuid);
    if (!seq) return; // <-- silent drop
    // ... update read state
}
```

Замени на:
```typescript
async function handleMessagesRead(data: Record<string, unknown>) {
    const chatId = data.chat_id as string;
    const lastReadUuid = data.last_read_message_id as string;
    if (!chatId || !lastReadUuid) return;

    const seq = getMessageSeqNum(lastReadUuid);
    if (seq) {
        // Normal path: update read state directly
        sendApiUpdate({
            '@type': 'updateChat',
            id: chatId,
            chat: {
                lastReadOutboxMessageId: seq,
                unreadCount: 0,
            },
        });
    } else {
        // Fallback: UUID not in local map — refresh chat to get current read state
        const { fetchFullChat } = await import('../methods/chats');
        fetchFullChat({ id: chatId });
    }
}
```

**Проверь:** точный формат `updateChat` payload в проекте. Возможно поля называются иначе (`lastReadOutboxMessageId` vs `readOutboxMaxId`). Используй то что используется в остальном коде.

---

## Task 24: Shared Media Links/Audio Types (Frontend)

**Файл:** Найди маппер shared media типов. Вероятно `web/src/api/saturn/methods/messages.ts` или `web/src/api/saturn/apiBuilders/`.

**Контекст:** Saturn мапит shared media типы только `media`, `documents`, `voice`, `gif`. Вкладки `links` и `audio` в правой панели shared media пустые.

**Что сделать:**

Найди где маппятся типы shared media (grep по `SharedMediaType` или `fetchSharedMedia`). Добавь маппинг для `links` и `audio`:

```typescript
// Маппинг Saturn media types → TG Web A SharedMediaType
const MEDIA_TYPE_MAP: Record<string, string> = {
    photo: 'media',
    video: 'media',
    file: 'documents',
    voice: 'voice',
    gif: 'gif',
    // Добавить:
    audio: 'audio',
    link: 'links',
};
```

Если бэкенд не возвращает type `audio` или `link` для GET `/chats/:id/media` — это ок, просто добавь маппинг чтобы если данные придут, они корректно отображались.

---

## Task 25: Wallpapers Stub (Frontend)

**Файлы:**
- `web/src/api/saturn/methods/index.ts`
- `web/src/api/saturn/methods/settings.ts`

**Контекст:** `fetchWallpapers` возвращает undefined, `uploadWallpaper` отсутствует. Экран Background пустой или зависает.

**Что сделать:**

В `settings.ts` или `index.ts` добавь:

```typescript
export async function fetchWallpapers() {
    // Return empty wallpaper list — Saturn doesn't have wallpaper catalog yet
    // This prevents UI from hanging on Loading state
    return {
        wallpapers: [],
    };
}

export async function uploadWallpaper(file: File) {
    // Stub: wallpaper upload not supported yet
    // Return a local object URL so the UI at least shows the selected image
    const url = URL.createObjectURL(file);
    return {
        slug: `local-${Date.now()}`,
        document: {
            url,
            mimeType: file.type,
        },
    };
}
```

В `index.ts` добавь экспорты если добавил в `settings.ts`.

**Это стаб** — полная реализация (R2 upload + catalog) будет отдельной задачей. Сейчас важно чтобы UI не ломался.
