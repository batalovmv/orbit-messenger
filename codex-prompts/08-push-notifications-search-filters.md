# Задача: Push-уведомления UI + Поисковые фильтры

## Роль и поведение
Ты — senior frontend-разработчик проекта Orbit Messenger (форк Telegram Web A). Работай автономно, принимай решения сам, не останавливайся на вопросах. Проводи самопроверку после каждого блока.

## Часть 1: Push-уведомления

### Контекст
Бэкенд Orbit уже поддерживает VAPID push-уведомления:
- `POST /api/v1/push/subscribe` — подписка (принимает `{endpoint, keys: {p256dh, auth}}`)
- `DELETE /api/v1/push/subscribe` — отписка
- Saturn фронтенд уже имеет метод `subscribePushNotifications` в `web/src/api/saturn/methods/settingsApi.ts`
- Service Worker зарегистрирован (TG Web A PWA)

### Что нужно сделать

**1. Запрос разрешения браузера**
- При первом входе или при открытии Settings → Notifications — показать промпт
- Найди `web/src/util/notifications.ts` или `web/src/util/pushNotification.ts` — там уже может быть логика
- Если нет — создай утилиту:
```ts
export async function requestNotificationPermission(): Promise<boolean> {
  if (!('Notification' in window)) return false;
  if (Notification.permission === 'granted') return true;
  if (Notification.permission === 'denied') return false;
  const result = await Notification.requestPermission();
  return result === 'granted';
}
```

**2. Подписка на push**
- После получения разрешения — создать PushSubscription через Service Worker:
```ts
const registration = await navigator.serviceWorker.ready;
const subscription = await registration.pushManager.subscribe({
  userVisibleOnly: true,
  applicationServerKey: VAPID_PUBLIC_KEY, // из config или env
});
```
- Отправить подписку на сервер через `subscribePushNotifications`
- VAPID public key должен приходить из бэкенда или быть в конфиге. Поищи `VAPID` в коде.

**3. Settings → Notifications UI**
- Найди `web/src/components/left/settings/SettingsNotifications.tsx`
- Добавь toggle "Enable push notifications" (если нет)
- При включении — запросить разрешение + подписаться
- При выключении — отписаться (`DELETE /push/subscribe`)

**4. Отображение уведомлений**
- Service Worker должен обрабатывать `push` event и показывать `self.registration.showNotification()`
- Найди service worker файл (`web/public/sw.js` или `serviceWorker.ts`)
- При клике на уведомление — открыть/сфокусировать вкладку Orbit и перейти в чат

### VAPID ключ
Поищи в коде:
```bash
grep -r "VAPID\|vapid\|applicationServerKey" web/src/
grep -r "VAPID" services/ .env.example
```
Ключ может быть в `.env.example` как `VAPID_PUBLIC_KEY`.

## Часть 2: Поисковые фильтры (by user, by date)

### Контекст
В Orbit при нажатии 🔍 в хедере чата открывается поиск. Справа от поля ввода есть две иконки:
- 👤 (фильтр по пользователю)
- 📅 (фильтр по дате)

Иконки отображаются, но клик по ним ничего не делает. Saturn search API уже поддерживает фильтры:
```
GET /api/v1/search?q=...&scope=messages&chat_id=...&type=text&from_user_id=UUID&date_from=2024-01-01&date_to=2024-12-31
```

### Что нужно сделать

**1. Найти компонент поиска в чате**
- `web/src/components/middle/` — найди `ChatSearch`, `MiddleSearch` или поисковую панель
- Или `web/src/components/right/` — может быть `RightSearch`

**2. Фильтр по пользователю (👤)**
- При клике — показать dropdown/модал со списком участников чата
- При выборе пользователя — добавить `from_user_id` к поисковому запросу
- Показать чип "From: Username" под полем поиска
- Крестик на чипе — убрать фильтр

**3. Фильтр по дате (📅)**
- При клике — показать date picker (calendar)
- TG Web A уже имеет `CalendarModal` — найди и переиспользуй
- При выборе даты — добавить `date_from` и `date_to` к запросу
- Показать чип "Date: Mar 15, 2024" под полем

**4. Saturn search API wiring**
- Проверь `web/src/api/saturn/methods/search.ts` — найди функцию поиска сообщений в чате
- Добавь параметры `fromUserId` и `dateRange` к вызову
- Убедись что query string правильно формируется

## Самопроверка
1. `grep -r "VAPID\|vapid" web/src/` — найти где хранится ключ
2. `grep -r "pushManager\|PushSubscription" web/src/` — найти существующую push логику
3. `grep -r "CalendarModal\|DatePicker" web/src/` — найти date picker
4. `grep -r "searchMessages\|searchMessagesLocal" web/src/api/saturn/` — проверить search API
5. Компиляция: `cd web && npx webpack --mode development`
6. Проверь что push subscription отправляется на сервер при включении
7. Проверь что поисковые фильтры добавляют параметры в API запрос
