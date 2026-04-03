# Задача: Исправить рендеринг фото/медиа + "Unexpected mutation" спам

## Роль и поведение
Ты — senior frontend-разработчик проекта Orbit Messenger (форк Telegram Web A). Работай автономно, принимай все решения сам. Проводи самопроверку после каждого блока. Читай CLAUDE.md проекта — там описаны правила fasterdom (requestMutation/requestMeasure).

## Баг 1: Фото с подписью рендерятся как документы

### Симптомы
В чатах jpg/jpeg файлы с подписью (caption) отображаются как документ — синяя иконка "jpg" + имя файла + размер, вместо inline-превью фото с подписью под ним. Фото БЕЗ подписи рендерятся нормально (inline-превью работает).

### Root cause (установлен аудитом)

**Файл:** `web/src/api/saturn/apiBuilders/messages.ts`

**Проблема 1 (главная):** `buildMediaContent()` (строки 193-258) содержит `switch (first.type)` где `case 'file': default:` ведёт к `content.document`. Если бэкенд Saturn возвращает `SaturnMediaAttachment.type` !== `'photo'` (например `'file'` или другое значение) для фото с caption — фото рендерится как документ.

Цепочка:
1. Saturn backend отдаёт `media_attachments[0].type = 'file'` для фото с caption
2. `buildMediaContent` → `switch ('file')` → `case 'file': default:` → `content.document = {...}`
3. `Message.tsx:1316` рендерит `<Document>` (иконка файла) вместо `<Photo>` (строка 1516)

**Проблема 2 (вторичная):** `registerMediaAttachmentAsset()` (строки 67-79) ВСЕГДА регистрирует ассет под ключом `['document']`:
```ts
registerAsset(att.media_id, {...}, ['document']);  // ← всегда 'document', никогда 'photo'
```
Из-за этого даже если `content.photo` будет установлен, `Photo` компонент не найдёт URL ассета (ищет по ключу `photo:${id}`).

### Исправление — два места

**Фикс 1:** В `buildMediaContent`, добавь fallback по mime_type в switch:
```ts
// Перед switch, добавить нормализацию типа:
const effectiveType = first.type === 'file' && first.mime_type?.startsWith('image/')
  ? 'photo'
  : first.type;

switch (effectiveType) {
  case 'photo':
    content.photo = { ... };
    break;
  // ... остальные case
  case 'file':
  default:
    content.document = { ... };
    break;
}
```

**Фикс 2:** В `registerMediaAttachmentAsset`, регистрируй под правильным ключом:
```ts
const assetKind = att.type === 'photo' || att.mime_type?.startsWith('image/')
  ? ['photo', 'document']  // регистрируем под обоими ключами
  : ['document'];
registerAsset(att.media_id, { ... }, assetKind);
```

**Фикс 3 (бэкенд, если нужно):** Проверь `services/messaging/` — убедись что при сохранении фото с caption в БД `media_type` остаётся `'photo'`, а не меняется на `'file'`:
```bash
grep -rn "media_type\|MediaType\|type.*file\|type.*photo" services/messaging/internal/
```

## Баг 2: Битые фото (X / красный квадрат)

### Симптомы
Некоторые изображения не загружаются — вместо фото показывается:
- Пустая область с серым крестиком (X) — кнопка "отмена загрузки"
- Красный квадрат малого размера
- Фото-контейнер без содержимого

### Диагностика

**Шаг 1:** Проверь как формируется URL фото:
```bash
grep -rn "getMediaUrl\|buildMediaUrl\|mediaUrl\|downloadMedia\|getPhotoUrl" web/src/api/saturn/
```
Saturn media API: `GET /api/v1/media/{mediaId}` → возвращает JSON с presigned URL на R2.
Фронтенд должен:
1. Запросить `/api/v1/media/{mediaId}` → получить `{ url: "https://r2.../photo.webp" }`
2. Использовать полученный URL как `src` для `<img>`

**Шаг 2:** Проверь не кэшируются ли старые/истёкшие presigned URL
- R2 presigned URL имеют TTL (обычно 1 час)
- Если URL кэшируется в state и используется позже — он может протухнуть

**Шаг 3:** Добавь error handling для загрузки медиа:
```ts
// В компоненте фото — добавить onError fallback
<img
  src={photoUrl}
  onError={(e) => {
    // Перезапросить URL с сервера
    refreshMediaUrl(mediaId);
  }}
/>
```

## Баг 3: "Unexpected mutation detected: style" — 197 ошибок за сессию

### Контекст
`web/src/lib/fasterdom/stricterdom.ts` — MutationObserver следит за DOM. Ошибка возникает когда код пишет `element.style.X = ...` вне фазы `mutate` (см. rendering cycle в CLAUDE.md).

### Диагностика

**Шаг 1:** Ошибка не содержит полезный stacktrace (минифицирован). Нужно найти источник:
```bash
grep -rn "\.style\." web/src/components/ --include="*.ts" --include="*.tsx" | grep -v "\.scss" | grep -v "className" | grep -v "style=\`" | head -40
```
Это покажет прямые присваивания `element.style.*` — каждое из них потенциальный источник.

**Шаг 2:** Правильные паттерны:
```ts
// НЕПРАВИЛЬНО — вызовет "Unexpected mutation"
element.style.width = '100px';

// ПРАВИЛЬНО — через requestMutation
requestMutation(() => {
  element.style.width = '100px';
});

// ПРАВИЛЬНО — если нужно сначала прочитать DOM
requestMeasure(() => {
  const width = element.offsetWidth;
  requestMutation(() => {
    element.style.transform = `translateX(${width}px)`;
  });
});
```

**Шаг 3:** Самые вероятные источники:
- Компоненты с анимациями (scroll, resize, drag)
- Media/video компоненты (размер подгоняется под контейнер)
- SymbolMenu (позиционирование)
- Toast/Notification (анимация появления)

Поищи:
```bash
grep -rn "requestMutation\|requestMeasure\|requestForcedReflow" web/src/components/middle/ | head -20
```
Сравни с прямыми `.style.` присваиваниями в тех же папках.

### Исправление
Каждое прямое `element.style.X = Y` вне `requestMutation` — обернуть в `requestMutation(() => { ... })`. Импортировать из `web/src/lib/fasterdom/fasterdom.ts`.

## Самопроверка
1. После фикса photo mapping: фото с подписью ДОЛЖНЫ рендериться как inline-фото + caption под ним
2. После фикса битых фото: проверь что media URL не кэшируются дольше чем TTL
3. После фикса mutations: открой dev tools → Console → ошибок "Unexpected mutation" не должно быть (или значительно меньше)
4. Компиляция: `cd web && npx webpack --mode development`
5. Проверь что фото без подписи всё ещё рендерятся корректно (не сломал)
