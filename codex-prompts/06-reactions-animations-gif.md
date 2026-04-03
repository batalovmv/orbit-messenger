# Задача: Реакции — анимации + GIF trending в composer

## Роль и поведение
Ты — senior frontend-разработчик проекта Orbit Messenger (форк Telegram Web A). Работай автономно, принимай решения сам. Не останавливайся. Проводи самопроверку.

## Часть 1: Реакции — подключить анимации

### Контекст
Реакции в Orbit работают: панель эмодзи появляется по правому клику на сообщение, клик отправляет реакцию через Saturn API, WS события `reaction_added`/`reaction_removed` обрабатываются. Но анимации не играют.

### Причина
`fetchAvailableEffects` в `web/src/api/saturn/methods/index.ts` возвращает `Promise.resolve(undefined)` (stub). Из-за этого объекты `availableReaction` не содержат полей `aroundAnimation` и `centerIcon` — компонент `ReactionAnimatedEmoji` fallback'ит на статичный `ReactionStaticEmoji`.

### Цепочка вызовов
1. `fetchAvailableReactions` в `reactions.ts` — вызывает `buildAvailableReactions()` который создаёт список из 22 hardcoded emoji
2. Каждая реакция создаётся без полей `aroundAnimation`, `centerIcon`, `appearAnimation`
3. `ReactionAnimatedEmoji.tsx` проверяет `tgsUrl` (от `aroundAnimation`) — если нет, рендерит статичный emoji

### Решение

**Вариант A (рекомендуемый — без бэкенда):** Добавить Lottie animation URLs в hardcoded данные.

1. Прочитай `web/src/api/saturn/apiBuilders/reactions.ts` — найди `buildAvailableReactions()` и `DEFAULT_AVAILABLE_REACTION_EMOJIS`
2. TG Web A имеет стандартные Lottie-анимации для реакций. Поищи в коде:
```bash
grep -rn "tgsUrl\|lottie.*reaction\|animation.*reaction\|ReactionEffect" web/src/
grep -rn "aroundAnimation\|centerIcon\|appearAnimation" web/src/api/
```
3. Если TG Web A загружает анимации с Telegram CDN — найди URL паттерн и проверь доступность
4. Если анимации загружаются через API call (getAvailableReactions) — имплементируй Saturn endpoint или верни static data

**Вариант B (проще):** Убедись что существующие CSS-анимации работают.

В `ReactionButton.module.scss` уже есть три анимации (ПРОВЕРЬ что классы `.popIn`, `.bounce`, `.countBump` применяются):
```scss
@keyframes reaction-pop-in {
  0%   { transform: scale(0);    opacity: 0; }
  70%  { transform: scale(1.15); opacity: 1; }
  100% { transform: scale(1);    opacity: 1; }
}
@keyframes reaction-bounce {
  0%   { transform: translateY(0); }
  40%  { transform: translateY(-0.25rem); }
  100% { transform: translateY(0); }
}
@keyframes reaction-count-bump {
  0%   { transform: scale(1); }
  50%  { transform: scale(1.3); }
  100% { transform: scale(1); }
}
.popIn   { animation: reaction-pop-in 300ms ease-out both; }
.bounce  { animation: reaction-bounce 400ms ease-out; }
.countBump { animation: reaction-count-bump 250ms ease-out; }
```

Reaction pill размеры: `height: 1.875rem; padding: 0 0.375rem 0 0.25rem; border-radius: 1.75rem; gap: 0.125rem;`
Hover: `transform: scale(1.1); transition: 150ms ease-out;`
Chosen state: `--reaction-background: var(--color-reaction-chosen);`
Container `.Reactions`: `display: flex; flex-wrap: wrap; gap: 0.375rem; margin-top: 0.25rem;`

### Дополнительно: Saved reaction tags

`fetchSavedReactionTags` и `updateSavedReactionTag` в `index.ts` возвращают `undefined`. Если в UI есть элементы которые от них зависят — скрой их. Или имплементируй сохранение тегов в localStorage (как favorites stickers).

Проверь:
```bash
grep -rn "savedReactionTags\|SavedReactionTag" web/src/components/
```
Если нигде не используется в UI — просто оставь stub.

## Часть 2: GIF — trending browse в composer tab

### Контекст
`GifPicker.tsx` (в composer, таб GIF) показывает только сохранённые GIF пользователя. Нет возможности browse trending GIF или искать — для этого нужно открыть правую панель через `GifSearch.tsx`.

### Что нужно сделать

1. Прочитай `web/src/components/middle/composer/GifPicker.tsx` — текущая структура
2. Прочитай `web/src/components/right/GifSearch.tsx` — как работает поиск и trending

3. Добавь в GifPicker:
   - **Trending секцию** сверху (до saved) — показывать `fetchGifs` (trending) результаты
   - **Поисковую строку** вверху — при вводе вызывать `searchGifs`
   - **Переиспользуй** `GifButton.tsx` для рендеринга каждой GIF

4. Saturn API (уже работает):
   - `GET /gifs/trending?limit=24` → массив GIF
   - `GET /gifs/search?q=...&limit=24` → поиск
   - `GET /gifs/saved?limit=200` → saved GIFs

5. Структура GifPicker после изменений:
```
┌──────────────────────────┐
│ 🔍 Search GIFs...        │  ← поисковая строка
├──────────────────────────┤
│ Trending                 │  ← секция trending (если нет поиска)
│ [gif] [gif] [gif]        │
│ [gif] [gif] [gif]        │
├──────────────────────────┤
│ Saved                    │  ← секция saved
│ [gif] [gif] [gif]        │
└──────────────────────────┘
```

При вводе в поиск — показывать результаты `searchGifs` вместо trending+saved.

### Стилизация
- Grid layout: `display: grid; grid-template-columns: repeat(3, 1fr); gap: 2px;`
- GIF items: `border-radius: 0.25rem; overflow: hidden; aspect-ratio: 1;`
- Search input: стандартный стиль как в TG Web A (поищи `SearchInput` компонент)

## Самопроверка
1. После реакций: правый клик → выбрать emoji → реакция должна появиться ПОД сообщением с анимацией
2. После GIF: в composer таб GIF → должны показываться trending GIF + строка поиска
3. Отправка GIF из composer должна работать
4. Компиляция: `cd web && npx webpack --mode development`
5. Проверь что реакции не сломались (отправка, удаление, WS обновление)
