# Задача: Починить emoji-пикер + подключить emoji ассеты

## Роль и поведение
Ты — senior frontend-разработчик проекта Orbit Messenger (форк Telegram Web A, фреймворк Teact). Работай автономно от начала до конца. Не прерывайся, принимай решения сам. После каждого блока изменений перечитай файлы и проверь корректность.

## Критический баг: Emoji-пикер не открывается

### Симптомы
При клике на кнопку 😊 (слева от поля ввода сообщения) вместо открытия SymbolMenu (панель emoji/stickers/GIF) в поле ввода вставляется битый img-тег. Кнопка отправки появляется (значит контент вставлен), но пикер не показывается.

### Диагностика — конкретные точки проблемы

Проведён аудит. Механика кнопки и SymbolMenu исправна. Проблема в **условии рендеринга кнопки**.

**Кнопка рендерится в `Composer.tsx:2236`:**
```tsx
{((!isComposerBlocked || canSendGifs || canSendStickers) && !isNeedPremium && !isAccountFrozen) && (
  <SymbolMenuButton ... />
)}
```

**Три вероятных root cause (проверяй по порядку):**

1. **`chat` === undefined при mount** — `getAllowedAttachmentOptions()` в `web/src/global/helpers/chats.ts:203` возвращает все false если `!chat`. Saturn API может не успеть загрузить чат в global state. Проверь:
   - `selectChat(global, chatId)` в `withGlobal` Composer — возвращает ли chat?
   - Если нет — Saturn `fetchChats` может не маппить chat объект в global правильно

2. **`global.appConfig.freezeUntilDate` !== undefined** — если Saturn backend или initial state содержит `freezeUntilDate`, `isAccountFrozen` === true и кнопка скрыта. Проверь:
   ```bash
   grep -rn "freezeUntilDate\|isAccountFrozen\|selectIsCurrentUserFrozen" web/src/
   ```
   Если Saturn не поддерживает заморозку — убедись что `freezeUntilDate` === undefined.

3. **`canSendPlainText` === false** — если `isComposerBlocked` true и при этом `canSendGifs` и `canSendStickers` тоже false. Проверь `getAllowedAttachmentOptions` — что возвращает для текущего чата.

**Кнопка (`SymbolMenuButton.tsx`):**
- Desktop: `ResponsiveHoverButton` с `onActivate={handleActivateSymbolMenu}` — клик вызывает `openSymbolMenu()` (useFlag setter). Механика корректна.
- Mobile: обычный `Button` с `onClick={isSymbolMenuOpen ? closeSymbolMenu : handleSymbolMenuOpen}`.

**SymbolMenu:**
- `isOpen` prop — единственный gate. CSS не блокирует (`transform: translate3d` для анимации).
- Desktop: `positionX: 'left', positionY: 'bottom'`, backdrop + mouse-leave для закрытия.
- Mobile: fixed overlay, слайд снизу.

### Что исправить
1. Найди почему `chat` undefined или `freezeUntilDate` задан — это главный блокер
2. Если проблема в timing (chat не загружен) — убедись что Composer mount происходит ПОСЛЕ загрузки chat data
3. Если `freezeUntilDate` — обнули его в Saturn initial state
4. После фикса: клик по 😊 ДОЛЖЕН открыть SymbolMenu
5. SymbolMenu должен закрываться по Escape, mouse-leave (desktop), или повторному клику

## Emoji ассеты (twemoji вместо unicode)

### Контекст
Сейчас emoji в чате рендерятся как unicode символы (зависят от ОС — на Windows выглядят иначе чем на Mac). В Telegram Web A emoji рендерятся через Apple-style спрайты (twemoji или собственные).

### Что нужно проверить
1. Поищи в коде:
```bash
grep -rn "twemoji\|Twemoji\|emoji.*sprite\|emoji.*asset\|apple.*emoji" web/src/
grep -rn "EmojiInteraction\|emojiData\|getEmojiUrl" web/src/
```

2. TG Web A имеет систему emoji рендеринга через `<img>` теги с URL на emoji спрайт. Найди:
   - `web/src/util/emoji*.ts` — утилиты для emoji
   - `web/src/components/common/CustomEmoji.tsx` — кастомные emoji
   - Поищи `IS_EMOJI_SUPPORTED` или аналог в config

3. Если система уже есть, но отключена — включи её. Если нет — подключи twemoji:
   - TG Web A использует Apple emoji спрайты с CDN
   - Поищи URL спрайтов в коде: `grep -r "emoji.*cdn\|emoji.*url\|apple.*emoji" web/src/`
   - Если спрайты загружаются с Telegram CDN — нужно либо проксировать, либо bundlить локально

### Визуальный результат
- Все emoji в сообщениях рендерятся через `<img>` с Apple-style изображениями
- Emoji в пикере тоже через спрайты
- Консистентный вид на всех платформах (Windows, Mac, Linux)

## Самопроверка
1. После исправления: клик по 😊 ДОЛЖЕН открыть панель emoji
2. Tab переключение: Emoji → Stickers → GIF должно работать
3. Выбор emoji из пикера ДОЛЖЕН вставить его в поле ввода (не битый img)
4. Emoji в отправленных сообщениях должны рендериться корректно
5. Поищи `"Unexpected mutation"` — убедись что SymbolMenu не вызывает новые mutation ошибки
6. Компиляция: `cd web && npx webpack --mode development`
