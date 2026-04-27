# Промпт для новой сессии: Phase 8A AI — отложенные UI компоненты

> Скопируй весь блок ниже (от `---` до конца) в новый чат Claude Code.

---

Мы в проекте **Orbit Messenger** (корпоративный мессенджер MST на 150 сотрудников, форк TG Web A на фронте). Репозиторий: `D:\job\orbit`. Читай `CLAUDE.md` в корне для общих правил, `web/CLAUDE.md` для Teact / Saturn / SCSS modules / Fasterdom конвенций.

## Задача сессии: доделать Phase 8A AI UI

Backend Phase 8A уже **полностью готов и задеплоен** (см. commit `0be6da3`). Saturn methods в `web/src/api/saturn/methods/ai.ts` готовы. UI MVP — только `AiSummaryModal` из chat header через кнопку `iconName="lamp"`.

В этой сессии делаем **3 отложенных UI компонента** Phase 8A, каждый как самостоятельный PR. Можно брать в любом порядке, они независимы.

## Что уже работает (контекст)

**Backend — `services/ai`:**
- `POST /ai/summarize` — SSE streaming (работает в `AiSummaryModal`)
- `POST /ai/translate` — SSE streaming (НЕ подключён к UI)
- `POST /ai/reply-suggest` — JSON, возвращает `{suggestions: string[]}` (НЕ подключён к UI)
- `POST /ai/transcribe` — JSON, принимает `{media_id}`, возвращает `{text, language}` (НЕ подключён к UI)
- `POST /ai/search` — **возвращает 501** (Phase 8A.2, не трогай)
- `GET /ai/usage` — JSON stats (опциональная UI-интеграция, nice-to-have)

**Degraded mode:** если `ANTHROPIC_API_KEY` или `OPENAI_API_KEY` в env = `placeholder`, endpoint'ы возвращают `503 service_unavailable` с JSON `{error: "service_unavailable", message: "AI provider not configured"}`. Твой UI должен это корректно обрабатывать — показывать банер "AI не настроен" вместо краша.

**Saturn methods (уже готовы, используй как есть):**
- `summarizeChat({ chatId, timeRange?, language? })` — `AsyncGenerator<string>`
- `translateMessages({ messageIds, chatId?, targetLanguage })` — `AsyncGenerator<string>`
- `suggestReply({ chatId })` — `Promise<string[]>`
- `transcribeVoice({ mediaId })` — `Promise<{text, language}>`
- `fetchAiUsage()` — `Promise<UsageStats>`

Файл: `web/src/api/saturn/methods/ai.ts`. Зарегистрированы в `web/src/api/saturn/methods/index.ts`.

**Reference компонент:** `web/src/components/middle/AiSummaryModal.tsx` (~200 строк). Там правильно реализовано:
- AsyncGenerator loop с abort-on-close
- Streaming text state
- 503 banner обработка
- useLastCallback / useFlag / memo паттерны Teact

Используй его как шаблон для `AiStreamModal` переиспользуемого компонента.

**Обязательно прочитай:**
- `web/CLAUDE.md` — Teact особенности, withGlobal/getActions, SCSS modules, no-null правило, no-conditional-spread
- `web/src/components/middle/AiSummaryModal.tsx` — reference pattern
- `docs/TZ-ORBIT-MESSENGER.md §11.8` — что именно должны делать эти фичи
- `CLAUDE.md` корневой — Saturn API naming, правила

## Задача 1 (M, 1 день): SuggestReply bar в composer

**Цель:** строка "💡 Предложения ответа" под textarea в composer. Клик → вызов `suggestReply({chatId})` → показать 3 кнопки с вариантами → клик по варианту подставляет его в input.

**Самая сложная часть scope:** Composer.tsx — 2742 строки, гигант. Трогай его **минимально**. Лучший подход — вынести всё в отдельный компонент `AiSuggestReplyBar.tsx` и встроить одной строкой где-нибудь рядом с ChatInput.

**Файлы:**
- **Новый:** `web/src/components/middle/composer/AiSuggestReplyBar.tsx`
  - OwnProps: `{ chatId: string, onSuggestionClick: (text: string) => void }`
  - State: `suggestions: string[] | undefined`, `isLoading: boolean`, `error?: string`
  - Render: маленькая строка с иконкой-lamp и текстом "Предложить ответ". При клике — загружает, показывает 3 кнопки-chips
  - Кнопка-суггестия onClick → `props.onSuggestionClick(text)`
  - Reuse `Button` из `ui/Button`, стили через SCSS module `AiSuggestReplyBar.module.scss`
- **Новый:** `web/src/components/middle/composer/AiSuggestReplyBar.module.scss`
  - Строка под input'ом, flex-row, gap, subtle фон
  - Активные chip'ы выглядят как ghost buttons
- **Измение:** `web/src/components/common/Composer.tsx` (минимально!)
  - Grep найди где ChatInput рендерится
  - Прямо над или под ним — добавь `<AiSuggestReplyBar chatId={chatId} onSuggestionClick={handleInsertSuggestion} />`
  - `handleInsertSuggestion` — вставляет текст в html editor (через `insertHtmlInSelection` или setSignal утилиту, погугли в соседних компонентах как они пишут в input)
  - **Не рефактори Composer.** Не трогай ничего кроме одного места вставки и одного handler'а.

**Edge cases:**
- Если peer — не human (бот, system) — не показывать AI suggestions вообще
- Если чат пустой — disabled state "Нет контекста для предложений"
- При 503 — показать inline "AI не настроен" вместо списка
- Пользователь начал печатать — скрыть suggestions (не перетирать его ввод)

**Acceptance:**
- Открыть DM, увидеть строку "💡 Предложить ответ"
- Клик → появились 3 варианта через 2-5 сек streaming
- Клик по варианту → текст в input
- Отправка работает как обычно
- В degraded mode (placeholder ключ) — banner "AI не настроен"

## Задача 2 (S, 0.5 день): AiTranscribeButton на voice messages

**Цель:** кнопка "📝 Транскрипция" под voice message bubble. Клик → вызов `transcribeVoice({mediaId})` → показать текст под кружком.

**Место:** voice message renderer. Найди через grep: `voice_note|voiceNote|VoiceMessage|RoundVideo`. Скорее всего это `web/src/components/middle/message/Voice.tsx` или похожий.

**Файлы:**
- **Новый:** `web/src/components/middle/message/AiTranscribeButton.tsx`
  - OwnProps: `{ mediaId: string }`
  - Collapsed state: кнопка "📝 Транскрипция"
  - Loading state: спиннер
  - Done state: показывает текст под кружком с возможностью "Свернуть"
  - Error state: "Не удалось транскрибировать" + retry
  - 503: "AI не настроен"
- **Измение:** найденный voice renderer — добавить `<AiTranscribeButton mediaId={...} />` под waveform или после duration

**Edge cases:**
- Слишком большой файл (>25MB) — Whisper вернёт error, покажи понятный текст
- Длинные транскрипции (>500 chars) — collapsable
- Кэшировать результат в memo/state чтобы не вызывать API повторно при ре-рендере
- Если юзер уже получал транскрипцию для этого mediaId в текущей сессии — сразу показывать без кнопки (локальный cache в ref / WeakMap)

**Acceptance:**
- Открыть чат с voice message
- Кликнуть "📝" → увидеть спиннер → увидеть текст
- Перерендер чата — текст сохраняется (не вызывается повторно)
- Degraded mode — 503 banner корректно

## Задача 3 (M, 0.5 день): Translate messages — select UX + modal

**Цель:** позволить выбрать N сообщений в chat history → открыть "Перевести выбранные" → стрим перевода в модалке.

**Это самый invasive** из трёх — select mode в chat history уже существует (для forward/delete), но нужно добавить новое действие в bulk menu.

**Файлы:**
- **Новый:** `web/src/components/middle/AiTranslateModal.tsx` — копия с `AiSummaryModal.tsx` pattern'а. Отличия:
  - Dropdown "Язык перевода" (ru/en/es/de/fr — стандартные BCP-47)
  - Streaming output через `translateMessages({messageIds, targetLanguage})`
  - OwnProps: `{ isOpen, messageIds, onClose }`
- **Новый:** `web/src/components/middle/AiTranslateModal.module.scss` (может reuse стили AiSummaryModal если они есть)
- **Измение:** `web/src/components/middle/MessageSelectToolbar.tsx` (или как называется bulk action toolbar в select mode)
  - Grep `selectMode|selected` чтобы найти toolbar
  - Добавить кнопку "🌐 Translate" рядом с существующими Forward/Delete
  - onClick → setOpen(true) + передать `selectedMessageIds` в modal
- **Может понадобиться:** action в global state для открытия translate modal, если select toolbar не имеет прямого доступа

**Edge cases:**
- Min 1, max 50 выбранных сообщений (выше — вероятно переполнение context window у Claude)
- Выбраны non-text сообщения (фото/видео/стикеры) — отфильтровать на фронте, передавать в API только те что имеют `content`
- Streaming показывает перевод по мере поступления строк
- Кнопка "Copy all" после завершения

**Acceptance:**
- Войти в select mode в чате
- Выбрать 3-5 сообщений
- Кликнуть "Translate"
- Выбрать язык (default EN)
- Кликнуть "Generate" → стримит перевод
- Скопировать результат → закрыть

## Порядок работы

Рекомендую идти: **Задача 2 (AiTranscribeButton)** — самая простая, самостоятельная, хороший warm-up → **Задача 1 (SuggestReply bar)** — средняя, осторожная правка Composer → **Задача 3 (Translate modal)** — последняя, самая интегрированная.

Но ты можешь взять их в любом порядке, они действительно независимы. Если одна блокируется чем-то (например непонятно как вклиниться в Composer) — переключайся на другую.

## Стиль работы

- Russian неформально (CLAUDE.md §"Роль AI")
- **Каждая задача = отдельный коммит** (`feat(web): AI transcribe button on voice messages` и т.п.)
- **Teact конвенции:** `const Component = (props) =>`, НЕ `FC<OwnProps>`. Никаких `null` (линт запрещает). String templates для `style` prop. `useLastCallback` вместо `useCallback` (см. `web/CLAUDE.md`).
- **SCSS modules camelCase**, импортить как `styles`, использовать `buildClassName`
- **Нет новых библиотек** — используй только то что уже в `web/package.json`
- **Нет тестов для фронта** — `web/CLAUDE.md` явно говорит "Do not write tests"
- **Localization:** все пользовательские тексты через `lang()`, ключи добавляй в `web/src/assets/localization/fallback.strings`, запускай `npm run lang:ts` после
- **Верификация:** preview_start + preview_screenshot после каждой задачи чтобы показать что работает. Если dev server не стартует — читай preview_logs, фикси

## Что НЕ делать

- **Не трогать `services/ai/`** — backend полностью готов, все edge cases обработаны
- **Не переписывать Composer** — только минимальное вклинивание
- **Не добавлять AI features вне ТЗ §11.8** — никаких creative дополнений, только summarize/translate/suggest/transcribe
- **Не реализовывать semanticSearch UI** — backend возвращает 501, это deferred до Phase 8A.2 (embeddings + pgvector)
- **Не делать UI для `/ai/usage`** — опциональное nice-to-have, не критично для релиза
- **Не трогать `web/src/global/cache.ts`** — если твой новый state нужно сохранять между сессиями, сначала спроси

## Checklist на каждую задачу

- [ ] Компонент создан, рендерится без ошибок
- [ ] Подключён к backend через готовые Saturn methods из `methods/ai.ts`
- [ ] Degraded mode (503) корректно отображается как "AI не настроен"
- [ ] Error states: network error, empty result, timeout
- [ ] `tsc --noEmit` — 0 новых ошибок
- [ ] `pnpm build` зелёный
- [ ] Preview smoke test показан скриншотом
- [ ] `lang()` используется для всех текстов, ключи добавлены в fallback.strings
- [ ] Коммит с conventional message

Старт: выбери одну из трёх задач, прочитай reference компонент `AiSummaryModal.tsx`, изучи код вокруг точки интеграции, покажи план и приступай.
