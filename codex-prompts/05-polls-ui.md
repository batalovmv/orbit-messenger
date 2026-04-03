# Задача: Опросы — UI подгонка + quiz solution

## Роль и поведение
Ты — senior frontend-разработчик проекта Orbit Messenger (форк Telegram Web A). Работай автономно, принимай решения сам. Ориентируйся на оригинальный код TG Web A — он у тебя в проекте (это форк). Читай оригинальные SCSS файлы для справки по стилям.

## Контекст
Опросы (polls) в Orbit уже работают end-to-end: создание, голосование, real-time обновления через WebSocket. Но UI отличается от Telegram:

### Текущие проблемы (что видно на сайте)
1. **Прогресс-бары** — тонкие зелёные линии вместо заполненных цветных полос
2. **Не проголосованные варианты** — отображается процент "0%" рядом с текстом, нет радио-кнопок/чекбоксов
3. **Кнопка "Vote"** — текстовая ссылка внизу, не кликабельна до выбора варианта
4. **Quiz mode** — поле explanation (объяснение правильного ответа) не отправляется на сервер

## Как должно выглядеть (референс: оригинальный TG Web A код)

### Структура опроса (до голосования)
```
┌─────────────────────────────────┐
│ Question Text                    │
│ Anonymous Poll / Quiz            │  ← зелёный текст
│                                  │
│ ○ Option 1                       │  ← радио-кнопка (single choice)
│ ○ Option 2                       │  ← или чекбокс (multiple choice)
│ ○ Option 3                       │
│                                  │
│           [Vote]                 │  ← кнопка, неактивна пока не выбран вариант
│                          12:00 ✓✓│
└─────────────────────────────────┘
```

### Структура опроса (после голосования)
```
┌─────────────────────────────────┐
│ Question Text                    │
│ Anonymous Poll                   │
│                                  │
│ ███████████████ 75%  Option 1   │  ← зелёная полоса (выбранный)
│ █████ 25%            Option 2   │  ← серая полоса
│ 0%                   Option 3   │  ← пустая полоса
│                                  │
│        View Results              │
│                          12:00 ✓✓│
└─────────────────────────────────┘
```

## Конкретные изменения

### 1. Прочитай оригинальные стили
Файлы для справки — они УЖЕ есть в проекте (TG Web A форк):
- `web/src/components/middle/message/Poll.scss` — основные стили
- `web/src/components/middle/message/PollOption.scss` или стили внутри `Poll.scss`

**Ключевые CSS свойства которые нужны:**

Прогресс-бар (заполненный):
```scss
.pollOption {
  .bar {
    height: 1.75rem;            // высота полосы — НЕ тонкая линия
    border-radius: 0.75rem;     // скруглённые концы
    background: var(--color-primary); // зелёный для выбранного
    transition: width 0.3s ease;
    min-width: 0.5rem;          // минимальная ширина даже для 0%
  }

  .bar.unselected {
    background: var(--color-interactive-element-hover); // серый для невыбранного
  }
}
```

Радио-кнопки (до голосования):
```scss
.pollOption {
  .radio {
    width: 1.25rem;
    height: 1.25rem;
    border: 2px solid var(--color-borders-input);
    border-radius: 50%;          // круг для single choice
    margin-right: 0.75rem;
    flex-shrink: 0;
  }

  .radio.checkbox {
    border-radius: 0.25rem;      // квадрат для multiple choice
  }

  .radio.selected {
    border-color: var(--color-primary);
    background: var(--color-primary);
    // Внутри — белая галочка (✓)
  }
}
```

Кнопка Vote:
```scss
.voteButton {
  color: var(--color-primary);
  font-weight: var(--font-weight-medium);
  text-align: center;
  padding: 0.5rem;
  cursor: pointer;
  opacity: 0.5;                   // неактивна

  &.active {
    opacity: 1;                   // активна после выбора
  }
}
```

### 2. PollOption.tsx — изменения

Прочитай `web/src/components/middle/message/PollOption.tsx` и убедись что:
- До голосования: показывает радио-кнопку (или чекбокс для multiple) + текст
- После голосования: показывает заполненный прогресс-бар + процент + текст
- Анимация: ширина бара плавно меняется при получении нового процента
- Выбранный вариант: зелёный бар с галочкой (✅)
- Правильный ответ (quiz): зелёный. Неправильный: красный.

### 3. Poll.tsx — кнопка Vote

Прочитай `web/src/components/middle/message/Poll.tsx`:
- До голосования: внизу кнопка "Vote" (неактивна пока не выбран вариант)
- Клик по кнопке: отправить голос через `sendPollVote`
- Multiple choice: можно выбрать несколько вариантов перед голосованием
- После голосования: кнопка меняется на "View Results" или исчезает

### 4. Quiz solution — отправка explanation

Проблема: При создании quiz-опроса поле `explanation` (объяснение правильного ответа) парсится в UI но не отправляется на сервер.

Найди в `web/src/api/saturn/methods/messages.ts` где формируется body для poll:
```bash
grep -n "type.*poll\|question.*options\|is_quiz\|correct_option\|explanation" web/src/api/saturn/methods/messages.ts
```

Добавь `explanation` в body запроса:
```ts
body.poll_explanation = explanation; // или solution, зависит от API
```

Проверь какое поле ожидает бэкенд:
```bash
grep -rn "explanation\|solution" services/messaging/internal/handler/ services/messaging/internal/model/
```

## Самопроверка
1. Прочитай ОРИГИНАЛЬНЫЙ `Poll.scss` из нашего проекта — сравни с тем что ты изменил
2. Проверь что ДО голосования видны радио-кнопки, а НЕ процентные полосы
3. Проверь что ПОСЛЕ голосования видны цветные прогресс-бары с процентами
4. Проверь что quiz mode работает: правильный ответ зелёный, неправильный красный
5. Проверь что explanation отправляется в POST body
6. Компиляция: `cd web && npx webpack --mode development`
