# Phase 6 Continuation — промт для нового чата

Скопируй всё ниже в новый чат Claude Code:

---

## Контекст

Мы работаем над **Orbit Messenger** — корпоративный мессенджер, монорепо `D:\job\orbit`.

**Phase 6 (Voice & Video Calls) частично реализована.** Сделано signaling + backend + gateway + frontend API stubs. Осталась WebRTC media plane.

## Что уже сделано

- **Calls service** (порт 8084): 12 REST endpoints, handler/service/store, PostgreSQL (migration 034)
- **Gateway**: proxy `/calls/*`, WebRTC signaling relay (offer/answer/ICE через hub.SendToUser), NATS subscriptions для call lifecycle/participant/media events
- **Docker**: calls container + coturn (3478 tcp/udp + 49152-49200 udp)
- **Frontend**: `web/src/api/saturn/methods/calls.ts` (REST + WS signaling), bridge methods (requestCall, acceptCall, discardCall, DH stubs), WS event handlers в wsHandler.ts
- **Все тесты проходят**, все контейнеры running

## Что нужно доделать

### 1. Pion SFU для групповых звонков
- `services/calls/internal/webrtc/` — пустая директория, нужна Pion SFU integration
- Room management: создание/удаление комнат по callId
- Track forwarding: каждый участник отправляет 1 audio + 1 video track, SFU раздаёт остальным
- Codec negotiation: VP8/VP9 video, Opus audio

### 2. Frontend WebRTC PeerConnection
- Интеграция с `secret-sauce` library (web/src/lib/secret-sauce/) для WebRTC media
- P2P: browser ↔ browser через RTCPeerConnection
- SFU: browser ↔ Pion SFU для 3+ участников
- Ringtone + vibration при incoming call
- Camera/mic permission requests

### 3. Push для звонков
- High-priority push notification когда app закрыт
- Отдельный push payload type для звонков

### 4. Nice to Have
- Network quality indicator
- Call rating после завершения
- Screen sharing через getDisplayMedia

## Как начать

1. Прочитай CLAUDE.md, PHASES.md (секция Phase 6 — отмеченные [x] и неотмеченные [ ] задачи)
2. Изучи Pion WebRTC через Context7
3. Изучи `web/src/lib/secret-sauce/` — как TG Web A управляет WebRTC connections
4. Начни с P2P 1-на-1 voice call (минимальный путь: кнопка → ringtone → accept → audio)
5. Затем video, затем group через SFU

## Критерий "готово"

Кнопка телефона → ringtone → принять → голос P2P работает. Видео → камера работает. Группа → video grid → screen share. Call history в профиле.
