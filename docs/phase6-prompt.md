# Phase 6: Voice & Video Calls — промт для нового чата

Скопируй всё ниже в новый чат Claude Code:

---

## Контекст

Мы работаем над **Orbit Messenger** — корпоративный мессенджер для компании MST (150+ сотрудников). Монорепо в `D:\job\orbit`.

**Завершено:** Phases 0-5 (core messaging, groups/channels, media, search/notifications/settings, rich messaging). Все долги Phase 2-5 закрыты в коммите `8960b58`.

**Текущая задача:** Phase 6 — Voice & Video Calls.

## Что нужно сделать

Phase 6 — голосовые и видеозвонки: 1-на-1 и групповые, с шарингом экрана.

### Архитектура
- **Новый сервис:** `services/calls/` (порт 8084) — Go/Fiber, как остальные сервисы
- **WebRTC SFU:** Pion (Go library) — для групповых звонков
- **TURN:** coturn на отдельном сервере (для NAT traversal)
- **Signaling:** через WebSocket (gateway), не через отдельный сервер
- **P2P:** 1-на-1 звонки идут напрямую (browser↔browser), SFU подключается при 3+ участниках

### Backend: 12 endpoints
```
POST   /calls              — инициировать звонок
PUT    /calls/:id/accept   — принять
PUT    /calls/:id/decline  — отклонить
PUT    /calls/:id/end      — завершить
GET    /calls/:id          — статус
GET    /calls/history       — история
POST   /calls/:id/participants       — добавить участника
DELETE /calls/:id/participants/:uid  — удалить
PUT    /calls/:id/mute              — mute/unmute
PUT    /calls/:id/screen-share/start — начать шаринг
PUT    /calls/:id/screen-share/stop  — остановить
GET    /calls/:id/ice-servers        — TURN/STUN credentials
```

### WebSocket: 11 событий
Server→Client: `call_incoming`, `call_accepted`, `call_declined`, `call_ended`, `call_participant_joined`, `call_participant_left`
Bidirectional: `webrtc_offer`, `webrtc_answer`, `webrtc_ice_candidate`, `call_muted`/`call_unmuted`, `screen_share_started`/`screen_share_stopped`

### Database: 2 таблицы
- `calls`: id, type (voice/video), mode (p2p/group), chat_id, initiator_id, status (ringing/active/ended/missed/declined), started_at, ended_at, duration_seconds
- `call_participants`: call_id + user_id PK, joined_at, left_at, is_muted, is_camera_off, is_screen_sharing

### Frontend: ~20 Saturn API методов
createCall, acceptCall, declineCall, hangUp, joinGroupCall, leaveGroupCall, toggleCallMute, toggleCallCamera, startScreenShare, stopScreenShare, fetchCallParticipants, fetchCallHistory, sendWebRtcOffer, sendWebRtcAnswer, sendIceCandidate, fetchIceServers

### Дополнительно
- Ringtone + vibration на входящий
- Push-уведомление на звонок когда app закрыт
- Кнопка звонка в хедере чата

## Как начать

1. Прочитай CLAUDE.md, PHASES.md (секция Phase 6), docs/TZ-PHASES-V2-DESIGN.md (секция Phase 6), docs/TZ-ORBIT-MESSENGER.md §11.6
2. Изучи Pion WebRTC Go library через Context7
3. Изучи как TG Web A обрабатывает звонки (web/src/components/calls/, web/src/api/gramjs/methods/calls.ts)
4. Выполни Шаг 0 из PHASES.md — проработка и план
5. Предложи план реализации на согласование

## Критерий "готово"

Кнопка телефона → ringtone → принять → голос P2P. Видео → камера. Группа "Начать звонок" → участники → video grid → screen share. Call history в профиле.
