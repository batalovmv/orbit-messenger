# Задача: PDF рендер в чате + дедупликация API запросов

## Роль и поведение
Ты — senior frontend-разработчик проекта Orbit Messenger (форк Telegram Web A). Работай автономно, принимай решения сам. Проводи самопроверку после каждого блока.

## Часть 1: PDF рендер

### Контекст
Когда пользователь отправляет PDF файл, он отображается как обычный документ (иконка + имя файла). В Telegram PDF показывается с превью первой страницы. Нужно добавить аналогичный функционал.

### Что нужно сделать

**1. Thumbnail для PDF**
Бэкенд option: при upload PDF генерировать thumbnail первой страницы серверно. Но это сложно.
Frontend option (проще): использовать `pdf.js` (уже может быть в зависимостях) или canvas рендеринг.

**Проверь зависимости:**
```bash
grep -r "pdfjs\|pdf.js\|pdf-dist\|pdfjs-dist" web/package.json web/node_modules/.package-lock.json
```

Если `pdfjs-dist` нет — **не добавляй новую библиотеку**. Вместо этого:
- Используй `<canvas>` + встроенный PDF рендер браузера НЕТ, это не работает
- Или отобразить стилизованную иконку PDF с метаданными (кол-во страниц, размер)

Если `pdfjs-dist` уже есть:
```ts
import * as pdfjsLib from 'pdfjs-dist';

async function renderPdfThumbnail(url: string, canvas: HTMLCanvasElement) {
  const pdf = await pdfjsLib.getDocument(url).promise;
  const page = await pdf.getPage(1);
  const viewport = page.getViewport({ scale: 0.5 });
  canvas.width = viewport.width;
  canvas.height = viewport.height;
  const ctx = canvas.getContext('2d')!;
  await page.render({ canvasContext: ctx, viewport }).promise;
}
```

**2. Компонент `PdfPreview`**
- Найди `web/src/components/middle/message/Document.tsx` — здесь рендерится документ
- Для PDF файлов (mime_type === 'application/pdf') — рендерить превью вместо стандартной иконки
- Превью: canvas с первой страницей, скруглённые углы, overlay с "PDF" текстом
- При клике — открыть PDF в новой вкладке или в встроенном viewer

**3. Стилизация**
```scss
.pdfPreview {
  position: relative;
  border-radius: 0.75rem;
  overflow: hidden;
  background: var(--color-background-secondary);
  max-width: 20rem;

  canvas {
    width: 100%;
    height: auto;
    display: block;
  }

  .pdfBadge {
    position: absolute;
    top: 0.5rem;
    left: 0.5rem;
    background: rgba(0, 0, 0, 0.5);
    color: white;
    padding: 0.125rem 0.375rem;
    border-radius: 0.25rem;
    font-size: 0.6875rem;
    font-weight: var(--font-weight-semibold);
  }
}
```

## Часть 2: Дедупликация API запросов

### Контекст
При открытии чата некоторые API запросы вызываются 2-6 раз:
- `GET /chats/:id/pinned` — вызывается 2 раза
- `GET /chats/:id/messages?...` — вызывается 2 раза с идентичными параметрами
- `GET /users/me/settings/privacy` — вызывается 6 раз подряд

Это тратит bandwidth и нагружает сервер.

### Что нужно сделать

**1. Найти источники дублей**
- Поищи в `web/src/global/actions/api/` где вызываются `fetchPinnedMessages`, `loadMessages`, `fetchPrivacySettings`
- Вероятные причины:
  - Двойной вызов из `useEffect` + `withGlobal` при mount
  - Race condition при быстром переключении чатов
  - Отсутствие проверки "уже загружается"

**2. Решение: request deduplication**
Создай утилиту или используй существующую:
```ts
// В client.ts или новый файл
const pendingRequests = new Map<string, Promise<any>>();

export function deduplicatedRequest<T>(key: string, fn: () => Promise<T>): Promise<T> {
  const existing = pendingRequests.get(key);
  if (existing) return existing as Promise<T>;

  const promise = fn().finally(() => pendingRequests.delete(key));
  pendingRequests.set(key, promise);
  return promise;
}
```

**3. Применить к горячим эндпоинтам**
- `fetchPinnedMessages` — key: `pinned:${chatId}`
- `fetchMessages` — key: `messages:${chatId}:${cursor}`
- `fetchPrivacySettings` — key: `privacy`

**4. Privacy settings — 6 вызовов**
- Найди где вызывается `fetchPrivacySettings` — вероятно в нескольких Settings-компонентах
- Добавь проверку: если уже загружено в global state, не запрашивай повторно
- Или кэшируй на уровне action: `if (global.settings.privacy) return;`

## Самопроверка
1. Проверь нет ли `pdfjs-dist` в зависимостях перед добавлением
2. Проверь что PDF превью рендерится только для `application/pdf` mime type
3. Для дедупликации: добавь `console.log` в dedup утилиту и проверь что запросы не дублируются при открытии чата
4. Компиляция: `cd web && npx webpack --mode development`
5. `grep -r "fetchPinnedMessages\|fetchPrivacySettings" web/src/global/actions/` — найди все точки вызова
