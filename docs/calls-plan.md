# Phase 6: Voice & Video Calls — Completion Plan

> Документ-чеклист для доработки звонков. Разбит на 5 этапов, каждый можно выполнять в отдельном чате.
> **Важно:** план составлен после полного аудита backend (services/calls/) и frontend (web/src/lib/secret-sauce/, calls.ts) 2026-04-09. Все file:line ссылки актуальны на коммит `aa6ef2f`.

---

## Оглавление

- [Контекст](#контекст)
- [Требования ТЗ](#требования-тз)
- [Текущее состояние](#текущее-состояние)
- [Каталог багов](#каталог-багов) — file:line список
- [Этап 1: Стабилизация P2P](#этап-1-стабилизация-p2p-1-on-1)
- [Этап 2: Media state sync](#этап-2-media-state-sync)
- [Этап 3: Pion SFU (группы)](#этап-3-pion-sfu-группы)
- [Этап 4: Push для закрытого app](#этап-4-push-для-закрытого-app)
- [Этап 5: Polish](#этап-5-polish)
- [Тестирование в Chrome](#тестирование-в-chrome)
- [Стартер-промты для новых чатов](#стартер-промты-для-новых-чатов)

---

## Контекст

**Orbit Messenger** — корпоративный мессенджер (monorepo `D:\job\orbit`), self-hosted, 150+ сотрудников.
- Backend: 8 Go микросервисов (Fiber v2, pgx, NATS, Redis).
- Frontend: форк Telegram Web A (Teact, кастомный Saturn API layer вместо MTProto/GramJS).
- Calls service: порт 8084, уже поднят в docker-compose, миграция 034 применена.

**Тестовые юзеры:**
- `admin@orbit.test` / `SuperAdmin123!` (сброшен через `/api/v1/auth/reset-admin` с ключом `localadminreset123`)
- `alice@orbit.test` / `TestPass123!`
- `bob@orbit.test` / `TestPass123!`

**Запуск:**
```bash
docker compose up --build          # вся инфра + сервисы + фронт на :3000
cd web && npm run dev                # HMR dev-сервер на :3000 (если docker фронт не нужен)
```

---

## Требования ТЗ

Из `docs/TZ-ORBIT-MESSENGER.md` §11.6 и `docs/TZ-PHASES-V2-DESIGN.md` Phase 6:

| Функция | Приоритет | Детали |
|---------|-----------|--------|
| 1-on-1 voice (P2P) | **Must** | Кнопка 📞 в хедере → P2P WebRTC |
| 1-on-1 video (P2P) | **Must** | Кнопка 📹 → видеозвонок |
| Group voice | **Must** | До 50 участников через Pion SFU |
| Group video | **Must** | Video grid + active speaker |
| Screen sharing | **Must** | Кнопка в call UI, через `getDisplayMedia` |
| Ringtone + vibration | **Must** | Звук + `navigator.vibrate()` на входящий |
| Push for calls | **Must** | High-priority VAPID push когда app закрыт |
| Call history | Should | ✅ Уже готово |
| Network quality indicator | Should | Индикатор связи в call UI |
| Rate call | Nice | Модалка после завершения |

**Архитектура (из ТЗ):**
- **P2P** — 1-на-1 calls через RTCPeerConnection, browser↔browser.
- **TURN (coturn)** — relay при корпоративном NAT. Уже в docker-compose.
- **SFU (Pion)** — group calls до 50 человек. НЕ реализован.

---

## Текущее состояние

### ✅ Что работает

**Backend (`services/calls/`):**
- 12 REST endpoints (`call_handler.go:37-50`): POST /calls, GET /calls/history, :id/accept/decline/end, :id/participants, :id/mute, :id/screen-share/*, :id/ice-servers.
- Gateway proxy: `services/gateway/internal/handler/proxy.go:244-269`.
- WS signaling relay: `services/gateway/internal/ws/handler.go:428-471` (`handleSignalingRelay`) — валидирует UUID, injects `sender_id`, rate limit 30/100ms, `hub.SendToUser` напрямую к target.
- NATS subjects: `orbit.call.{id}.lifecycle`, `orbit.call.{id}.participants`, `orbit.call.{id}.media`.
- DB: migration 034 (`calls` + `call_participants`), migrator применит при старте (после миграции 036 механизм migrator'а).
- Inter-service auth: gateway подписывает `X-Internal-Token`.

**Frontend (`web/src/`):**
- Saturn REST methods в `api/saturn/methods/calls.ts`: createCall, acceptCallApi, declineCallApi, endCallApi, toggleCallMute, screen share, fetchICEServers, iceServersToConnections.
- Bridge methods для TG Web A совместимости: requestCall, acceptCall, discardCall, DH stubs (getDhConfig, encodePhoneCallData и т.д.).
- WS handlers в `api/saturn/updates/wsHandler.ts`: `handleCallIncoming`, `handleCallAccepted`, `handleCallDeclined`, `handleCallEnded`, `handleCallMuteChanged`, `handleWebRTCSignaling`.
- WebRTC engine: `lib/secret-sauce/p2p.ts` — RTCPeerConnection setup, SDP parse/build, offer/answer, ICE candidates, mute/camera toggle.
- UI компоненты: `components/calls/PhoneCall.tsx`, `GroupCall.tsx`, `GroupCallParticipantList.tsx`, `ActiveCallHeader.tsx`, `MessagePhoneCall.tsx` — все отрендерены и подключены.
- Call history в профиле работает.

### ❌ Что сломано / отсутствует

| # | Проблема | Impact |
|---|----------|--------|
| 1 | `TURN_URL=turn:localhost:3478` — недостижим из Chrome | Блокер P2P через NAT |
| 2 | `wsHandler.ts:520-522` — dynamic `require()` | Race condition в hot path |
| 3 | `p2p.ts:395-396` — orphaned ICE candidates | Silent hang |
| 4 | `p2p.ts:506-507` — `createOffer()` без try-catch | Silent hang |
| 5 | `calls.ts:286` — `chatId` fallback на `user.id` | Broken DM calls |
| 6 | `call_service.go:67-72,101-106` — silent participant insert errors | Empty participants list |
| 7 | `call_service.go:234-262` — нет авторизации в AddParticipant | IDOR |
| 8 | Нет call timeout | Ringing висит вечно |
| 9 | Connection state не отображается в UI | Нет feedback юзеру |
| 10 | Vibration не вызывается | ТЗ Must не выполнен |
| 11 | Pion SFU не реализован (`services/calls/internal/webrtc/` пуст) | Нет групповых звонков |
| 12 | Frontend group call methods — stubs (calls.ts:401-412) | Нет групповых звонков |
| 13 | `call_participant_joined/left` WS events игнорируются (wsHandler.ts:168-171) | Group UI не обновляется |
| 14 | Нет push payload для звонков | ТЗ Must не выполнен |
| 15 | Network quality indicator отсутствует | ТЗ Should |
| 16 | `setCallRating()` stub, нет endpoint | ТЗ Nice |

---

## Каталог багов

Полный список с file:line — чтобы новый чат мог править точечно без re-audit.

### Backend bugs

**B1. TURN_URL недостижим** — `docker-compose.yml:208`
```yaml
TURN_URL: turn:localhost:3478    # ❌ localhost из контейнера не резолвится у клиента
```
**Fix:** добавить `TURN_PUBLIC_URL` в .env и docker-compose, использовать для ICE response. Дефолт dev: `turn:host.docker.internal:3478`.

**B2. Silent participant insert errors** — `services/calls/internal/service/call_service.go:67-72`
```go
if err := s.participants.Add(ctx, callID, initiatorID, ...); err != nil {
    slog.Error("failed to add initiator", "error", err)
    // ❌ не return err — call создан без участников
}
```
Аналогично lines 101-106, 253, 271, 289, 313, 331.
**Fix:** propagate up, rollback call creation если participant add failed.

**B3. AddParticipant нет авторизации** — `services/calls/internal/service/call_service.go:234-262`
Любой authenticated user с call_id может добавить любого юзера.
**Fix:** проверять `chat_members` — caller должен быть членом chat; callerRole должен быть admin/owner для groups, либо chat участник для DM.

**B4. Нет call timeout** — нет фоновой логики.
**Fix:** горутина в `calls/cmd/main.go` раз в 30с:
```sql
UPDATE calls SET status='missed', ended_at=NOW()
WHERE status='ringing' AND created_at < NOW() - interval '60 seconds'
RETURNING id, chat_id, initiator_id
```
Для каждой возвращённой строки publish NATS `call_ended` reason=`missed`.

**B5. TURN_URL dropped silently** — `call_service.go:356`
```go
if turnURL != "" && turnUser != "" && turnPassword != "" {  // если хоть один пустой — нет TURN
    servers = append(servers, ...)
}
```
**Fix:** логировать warning если turnURL есть но credentials нет.

**B6. NATS events содержат полный call object** — `call_service.go:116-120`.
**Fix:** минимизировать payload — только `call_id` + `user_id` + минимум. Клиент fetch детали через REST если надо.

### Frontend bugs

**F1. Dynamic require в WS hot path** — `web/src/api/saturn/updates/wsHandler.ts:520-522`
```typescript
const { setActiveCallId, setActiveCallPeerId } = require('../methods/calls');
```
**Fix:** статический import сверху файла.

**F2. chatId fallback на userId** — `web/src/api/saturn/methods/calls.ts:286`
```typescript
const chatId = providedChatId || user.id;  // ❌ userId ≠ chatId в Saturn
```
**Fix:** требовать chatId, если undefined — showNotification + return undefined.

**F3. Orphaned ICE candidates** — `web/src/lib/secret-sauce/p2p.ts:395-396`
Candidates queued если InitialSetup не пришёл; если не пришёл никогда — orphan.
**Fix:** setTimeout 10s, если gotInitialSetup === false → clear queue + sendApiUpdate discard.

**F4. createOffer без try-catch** — `web/src/lib/secret-sauce/p2p.ts:506-507`
```typescript
const offer = await conn.createOffer(params);
await conn.setLocalDescription(offer);
sendInitialSetup(parseSdp(conn.localDescription!, true) as P2pParsedSdp);
```
**Fix:** try-catch, на error → emit updatePhoneCall state=discarded reason=disconnect.

**F5. Connection state не показывается** — PhoneCall.tsx.
`updatePhoneCallConnectionState` приходит от secret-sauce, но UI не читает.
**Fix:** читать `phoneCall.connectionState` и показывать статус ('Connecting...', 'Reconnecting...').

**F6. Нет vibration на incoming** — `wsHandler.ts:handleCallIncoming`.
**Fix:** `if ('vibrate' in navigator) navigator.vibrate([300, 200, 300, 200, 300]);`

**F7. Media stream cleanup** — `p2p.ts:168-169` — нет finally на failed getUserMedia.
**Fix:** `try { ... } catch { stream?.getTracks().forEach(t => t.stop()); throw; }`

**F8. Data channel JSON parse без try-catch** — `p2p.ts:302`.
**Fix:** try-catch вокруг JSON.parse, log + ignore malformed.

**F9. RatePhoneCallModal submission — no-op** — `calls.ts:setCallRating` возвращает undefined.
**Fix:** реализация этапа 5.

**F10. Group call WS events игнорируются** — `wsHandler.ts:168-171`.
**Fix:** этап 3 — реальные handlers для `call_participant_joined/left/screen_share_*`.

---

## Этап 1: Стабилизация P2P 1-on-1

**Цель:** voice + video P2P между двумя Chrome работает надёжно (happy path + основные ошибки).
**Scope:** ТЗ Must — 1-on-1 voice/video, ringtone+vibration, ICE reliability.
**Файлы:** ~8, правки ~150 строк.
**Длительность:** один средний чат (после этого плана).

### Backend задачи

- [ ] **E1.B1** Добавить env var `TURN_PUBLIC_URL`:
  - `.env.example` — `TURN_PUBLIC_URL=turn:host.docker.internal:3478` (dev)
  - `docker-compose.yml` calls service — `TURN_PUBLIC_URL: ${TURN_PUBLIC_URL:-turn:host.docker.internal:3478}`
  - `services/calls/cmd/main.go` — читать через `config.EnvOr("TURN_PUBLIC_URL", "")`, передавать в handler как `turnURL` вместо внутреннего.
- [ ] **E1.B2** `call_service.go:67-72, 101-106` — propagate participant insert errors, rollback call (DELETE FROM calls WHERE id=$1) если Add failed.
- [ ] **E1.B3** `call_service.go:AddParticipant` — SELECT FROM chat_members WHERE chat_id=$1 AND user_id=$2; если нет — apperror.Forbidden.
- [ ] **E1.B4** `calls/cmd/main.go` — фоновая горутина:
  ```go
  go func() {
      ticker := time.NewTicker(30 * time.Second)
      defer ticker.Stop()
      for range ticker.C {
          if err := callStore.ExpireRinging(ctx, 60*time.Second); err != nil { ... }
      }
  }()
  ```
  Новый метод в `call_store.go:ExpireRinging` — UPDATE...RETURNING, для каждой строки publish `call_ended` reason=`missed` в NATS.
- [ ] **E1.B5** `call_service.go:356` — warning log если turnURL set без credentials.
- [ ] **E1.B6** Минимизировать NATS event payloads (опционально, если остаётся время).

### Frontend задачи

- [ ] **E1.F1** `web/src/api/saturn/updates/wsHandler.ts` — статический import `setActiveCallId, setActiveCallPeerId` сверху файла, удалить dynamic require() на строке 520-522.
- [ ] **E1.F2** `web/src/api/saturn/methods/calls.ts:requestCall` — убрать fallback на user.id; если `providedChatId` undefined → `showNotification({ message: lang('CallFailedToStart') })` + return undefined. Добавить строку в `fallback.strings`.
- [ ] **E1.F3** `web/src/lib/secret-sauce/p2p.ts` — setTimeout(10000) после первого queued ICE candidate; если в этот момент `gotInitialSetup === false` → clear pendingCandidates + emit error update.
- [ ] **E1.F4** `web/src/lib/secret-sauce/p2p.ts:createOffer` — try-catch, на error: call `onEmitUpdate({ '@type': 'updatePhoneCall', call: { ...call, state: 'discarded', reason: 'disconnect' }})`.
- [ ] **E1.F5** `web/src/components/calls/PhoneCall.tsx` — читать `phoneCall.connectionState`, показывать subtitle ('Connecting…', 'Reconnecting…', 'Failed') рядом с аватаркой.
- [ ] **E1.F6** `web/src/api/saturn/updates/wsHandler.ts:handleCallIncoming` — `if ('vibrate' in navigator) { try { navigator.vibrate([300, 200, 300]); } catch {} }`.
- [ ] **E1.F7** `web/src/lib/secret-sauce/p2p.ts:168` — finally cleanup stream on error.
- [ ] **E1.F8** `web/src/lib/secret-sauce/p2p.ts:302` — try-catch вокруг JSON.parse для data channel messages.

### Тестирование (Chrome, два окна)

1. Окно 1 — `localhost:3000`, login alice@orbit.test / `TestPass123!`.
2. Окно 2 (incognito) — `localhost:3000`, login bob@orbit.test / `TestPass123!`.
3. Открыть DM alice↔bob.
4. **Voice call:** alice → 📞 → bob видит incoming → vibration → accept → оба слышат друг друга (проверить через mic meter или "проверка раз-два").
5. **Video call:** alice → 📹 → accept → оба видят друг друга.
6. **Hangup:** alice → hangup → call завершается у обоих, state=ended в DB, appear в history.
7. **Decline:** alice → call → bob → decline → alice видит "Declined".
8. **Missed:** alice → call → bob не отвечает 60s → call helyett status=missed, alice видит "No answer".
9. **Reconnect:** alice → call → bob → active → отключить wifi на 5с bob'у → reconnect → call продолжается (ICE restart).
10. **Connection state:** во время establish видно "Connecting..." subtitle в UI.

### Commit message
```
fix(calls): stabilize P2P — TURN public URL, error propagation, ICE timeouts, connection state UI

- Expose TURN via TURN_PUBLIC_URL env for client reachability
- Propagate participant insert errors, rollback call on failure
- Authorize AddParticipant via chat_members check
- Auto-expire ringing calls after 60s (background worker)
- Static import setActiveCallId in WS handler (no more dynamic require)
- Require chatId in requestCall, notify on failure
- Timeout orphaned ICE candidates (10s), discard call on createOffer failure
- Show connection state in PhoneCall UI
- Vibrate on incoming call
- Cleanup media stream on getUserMedia failure, guard JSON.parse in data channel

Closes Phase 6 stage 1 (P2P stabilization).
```

### PHASES.md updates
```markdown
### Phase 6: Voice & Video Calls

#### Stage 1: P2P stabilization ✅
- [x] TURN_PUBLIC_URL для client reachability
- [x] Participant error propagation + rollback
- [x] AddParticipant chat membership check
- [x] Auto-expire ringing calls (60s)
- [x] Fix WS dynamic require
- [x] chatId validation in requestCall
- [x] ICE candidate timeout + createOffer error handling
- [x] Connection state UI
- [x] Vibration on incoming
```

---

## Этап 2: Media state sync

**Цель:** screen share, mute, camera toggle работают надёжно и синхронизируются между peers.
**Scope:** ТЗ Must — screen sharing.
**Файлы:** ~4, правки ~80 строк.

### Задачи

- [x] **E2.1** `web/src/lib/secret-sauce/p2p.ts` — `getDisplayMedia` + `sender.replaceTrack(screenTrack)` verified; added auto camera track restore on presentation stop (tracks `videoEnabledBeforePresentation`).
- [x] **E2.2** Mute path now: local audio track off/on (toggleStreamP2p) + data-channel MediaState + REST `PUT /calls/:id/mute` (PhoneCall.tsx `handleToggleAudio`). Peer receives `call_muted` WS event.
- [x] **E2.3** `switchCameraInputP2p` cycles through `enumerateDevices()` videoinputs; falls back to facingMode toggle for mobile.
- [x] **E2.4** `PhoneCall.tsx` buttons (mute/camera/screen-share) read from `phoneCall.isMuted` / `.videoState` / `.screencastState`. `updateStreams()` in p2p.ts now publishes LOCAL state (not peer).
- [x] **E2.5** `wsHandler.handleCallMuteChanged` → `updatePhoneCallPeerState` (echo-guarded). `PhoneCall.tsx` renders peer "Muted" badge overlay.
- [x] **E2.6** `wsHandler.handleScreenShareChanged` (started/stopped) → `updatePhoneCallPeerState` with `peerIsScreenSharing`. `PhoneCall.tsx` renders "Screen sharing" badge.

### Тестирование

1. Voice call активен между alice и bob.
2. alice mute → bob видит иконку мута над аватаркой.
3. alice unmute → иконка пропала.
4. alice screen share → bob видит desktop alice вместо камеры.
5. alice stop screen share → bob видит камеру alice.
6. bob camera off → alice видит аватарку вместо видео.
7. alice flip camera (если есть вторая) → видео обновилось.

### Commit message
```
feat(calls): media state sync — screen share, mute, camera toggle

- Screen share via getDisplayMedia + replaceTrack
- Unified mediaState source of truth in PhoneCall UI
- Peer mute indicator
- Screen share badge on peer
```

---

## Этап 3: Pion SFU (группы)

**Цель:** групповые голосовые/видео звонки до 50 участников через Pion SFU.
**Scope:** ТЗ Must — group voice + group video.
**Файлы:** ~15 новых, ~200 изменённых.
**Длительность:** отдельный чат, самый крупный этап.

### Архитектура

```
Клиент A ─┐
          │
Клиент B ─┼──→ WebSocket /calls/:id/sfu-ws ──→ Pion SFU (in calls service)
          │                                          │
Клиент C ─┘                                          └── Room{peers, localTracks}
                                                          - receives tracks from each peer
                                                          - forwards to all other peers
```

- 1-на-1 остаётся P2P (этап 1) — `mode='p2p'`.
- 3+ участников → `mode='group'` → клиенты коннектятся к `/api/v1/calls/:id/sfu-ws` (gateway проксирует на calls:8084).
- Каждый клиент = 1 RTCPeerConnection к SFU (не mesh).
- SFU forwards tracks через `TrackLocalStaticRTP`.

### Backend задачи

- [x] **E3.B1** `services/calls/go.mod` — добавить `github.com/pion/webrtc/v4`.
- [x] **E3.B2** `services/calls/internal/webrtc/sfu.go`:
  ```go
  type SFU struct {
      rooms map[uuid.UUID]*Room
      mu    sync.RWMutex
      api   *webrtc.API   // с правильными codec parameters
  }
  func NewSFU() *SFU
  func (s *SFU) GetOrCreateRoom(callID uuid.UUID) *Room
  func (s *SFU) CloseRoom(callID uuid.UUID)
  ```
- [x] **E3.B3** `services/calls/internal/webrtc/room.go`:
  ```go
  type Room struct {
      ID          uuid.UUID
      peers       map[uuid.UUID]*Peer
      localTracks map[string]*webrtc.TrackLocalStaticRTP  // ssrc → track
      mu          sync.RWMutex
  }
  func (r *Room) AddPeer(userID uuid.UUID, peer *Peer)
  func (r *Room) RemovePeer(userID uuid.UUID)
  func (r *Room) AddLocalTrack(trackID string, track *webrtc.TrackLocalStaticRTP)
  func (r *Room) RemoveLocalTrack(trackID string)
  func (r *Room) signalAllPeers()  // renegotiate all peers after track add/remove
  ```
- [x] **E3.B4** `services/calls/internal/webrtc/peer.go`:
  ```go
  type Peer struct {
      UserID uuid.UUID
      PC     *webrtc.PeerConnection
      WS     *websocket.Conn  // для signaling
      room   *Room
  }
  func NewPeer(userID uuid.UUID, ws *websocket.Conn, room *Room) (*Peer, error)
  // PC.OnTrack → пересылка в Room.localTracks (копировать RTP packets)
  // PC.OnICECandidate → send via WS
  // PC.OnConnectionStateChange → cleanup on disconnect
  func (p *Peer) HandleOffer(sdp string) error
  func (p *Peer) AddICECandidate(candidate string) error
  ```
- [x] **E3.B5** Новый HTTP handler в calls service: `GET /calls/:id/sfu-ws` — websocket upgrade (fiber websocket), валидация JWT через gateway internal auth, создание Peer + join Room.
- [x] **E3.B6** Gateway proxy `/api/v1/calls/:id/sfu-ws` → calls:8084 (websocket proxy). Сохранять user context через X-User-ID header в handshake.
- [x] **E3.B7** `call_service.go` — при CreateCall с `mode='group'` (или когда количество участников > 2) в response возвращать `sfu_ws_url: /api/v1/calls/:id/sfu-ws`.
- [x] **E3.B8** `calls/cmd/main.go` — инициализация SFU, передача в handler, periodic cleanup пустых rooms (раз в 5 минут).
- [x] **E3.B9** Codec configuration: VP8 (90000), Opus (48000). SDP munging если нужно.
- [x] **E3.B10** Disconnect handling: OnConnectionStateChange → Disconnected/Failed → Room.RemovePeer → publish NATS `call_participant_left`.

### Frontend задачи

- [x] **E3.F1** `web/src/api/saturn/methods/calls.ts` — имплементировать реальные методы:
  - `createGroupCall({ chatId, type })` → POST /calls с mode='group'
  - `joinGroupCall({ callId })` → open WS к sfu_ws_url, создать RTCPeerConnection, add local tracks, handle offer/answer/ICE
  - `leaveGroupCall({ callId })` → close WS + PC
  - `fetchGroupCallParticipants({ callId })` → GET /calls/:id (участники в response)
- [x] **E3.F2** Новый модуль `web/src/lib/secret-sauce/sfu.ts` (или adapt существующий `secretsauce.ts`):
  - `joinSfuCall(wsUrl, localStream, onRemoteTrack, onPeerLeave)` — single RTCPeerConnection, ontrack → добавляет remote stream в grid.
  - Signaling через WS: offer/answer/ICE packed как JSON {type, payload}.
- [x] **E3.F3** `web/src/api/saturn/updates/wsHandler.ts:168-171` — заменить no-op на реальные:
  - `call_participant_joined` → update groupCall.participants, dispatch updateGroupCallParticipants
  - `call_participant_left` → remove from participants
  - `screen_share_started/stopped` → update participant.isScreenSharing
- [ ] **E3.F4** `web/src/components/calls/GroupCall.tsx` — проверить что video grid обновляется через `ontrack` callbacks from SFU connector.
- [ ] **E3.F5** Ringtone + accept/decline для group call — не звонят всем сразу, а показывается уведомление "Alice started a call in chat" с кнопкой Join.

### Тестирование

1. 3 окна Chrome (alice, bob, charlie) в групповом чате.
2. alice → "Start call" → bob и charlie видят банер "Alice started a call" → join.
3. Video grid со всеми тремя, оба слышат и видят друг друга.
4. charlie screen share → все видят.
5. charlie leave → grid обновляется (2 tile), его stream пропадает.
6. bob join back → grid снова 3 tile.
7. alice leave (инициатор) → call продолжается с bob+charlie.
8. Last participant leave → call auto-ended, room cleanup.

### Commit message
```
feat(calls): Pion SFU for group calls

- SFU implementation in services/calls/internal/webrtc/
- Room + Peer + track forwarding via TrackLocalStaticRTP
- WebSocket signaling at /api/v1/calls/:id/sfu-ws (gateway proxy)
- Frontend SFU connector in lib/secret-sauce/sfu.ts
- Real group call Saturn API methods
- WS handlers for call_participant_joined/left, screen_share_*
- VP8 + Opus codec support

Closes Phase 6 stage 3 (group calls).
```

### Контекст для нового чата
- **Pion docs:** использовать Context7 — `resolve-library-id "pion webrtc"` → `query-docs` про SFU/TrackLocalStaticRTP/AddTrack/sender forwarding.
- **Reference implementations:** Pion `examples/broadcast`, `examples/sfu` на github.com/pion/webrtc.

### Status (initial implementation, 2026-04-09, commit 609e374)
Stage 3 backend + frontend wiring доставлено. Все backend задачи (E3.B1–B10) и frontend wiring (E3.F1–F3) закрыты:
- Pion v4 SFU внутри calls service (`internal/webrtc/`), MediaEngine с Opus + VP8
- Bidirectional WS proxy через gateway (`handler/sfu_proxy.go`), auth-frame паттерн с переиспользованием `ws.ValidateToken`
- Saturn SFU client `lib/secret-sauce/sfu.ts` + реальные `joinGroupCall` / `leaveGroupCall` в `methods/calls.ts`
- WS update handlers для `call_participant_joined` / `call_participant_left`
- 4 smoke-теста для room/peer lifecycle (passing)
- `cd services/calls && go build ./...` чисто, `go test ./...` зелёный
- `cd services/gateway && go build ./...` чисто, `go test ./...` зелёный
- `cd web && npx tsc --noEmit` — 12 baseline ошибок без новых

**Что отложено в Stage 3.5:**
- Глубокая интеграция existing TG Web A `GroupCall.tsx` UI с Saturn SFU streams (E3.F4) — backend готов, текущее WS-уведомление дёргает `updateGroupCallParticipants`, но video grid маппинг остался от Colibri-формата. Нужен либо адаптер reducer'а к новому schema, либо новый минимальный grid компонент.
- E3.F5 ringtone/banner для group call — отложено вместе с UI integration.
- Live-тест 3 окнами Chrome incognito по сценариям 11–13 — backend готов, требует ручного прогона.
- Auto-routing P2P vs SFU на frontend (selectedChat.member_count > 2 → mode='group') — поле выставляется явно создателем, нужна обвязка в `requestCall`.

---

## Этап 4: Push для закрытого app ✅ (закрыт 2026-04-09, 77a7b73)

**Цель:** высокоприоритетный push когда юзер оффлайн, с кнопками accept/decline в notification.
**Scope:** ТЗ Must — push for calls.
**Файлы:** 6 (gateway: dispatcher.go + tests, nats_subscriber.go + tests; web: pushNotification.ts, setupServiceWorker.ts).

> Реализовано: `SendCallToUsers` (UrgencyHigh + TTL=30s) в push.Dispatcher,
> `enqueueCallPushDispatch` в nats_subscriber.go (skip online users via `hub.IsOnline`,
> исключает initiator и sender), service worker call_incoming branch с tag/renotify/
> requireInteraction/actions, main-thread postMessage listener + URL-param fallback
> для openWindow case. Backend tests: 3 dispatcher + 2 subscriber. tsc 12 baseline без новых.

### Задачи

- [ ] **E4.1** `services/calls/internal/service/call_service.go:CreateCall` — для каждого callee проверить online status:
  ```go
  online, _ := redis.Exists(ctx, "online:"+userID.String()).Result()
  if online == 0 {
      publisher.Publish("orbit.user."+userID+".push", "incoming_call", map[string]any{
          "call_id": callID,
          "caller_id": initiatorID,
          "caller_name": callerName,
          "is_video": callType == "video",
          "chat_id": chatID,
      }, ...)
  }
  ```
- [ ] **E4.2** `services/gateway/internal/nats/push_subscriber.go` (или где push handling) — добавить case для `incoming_call` event type:
  ```go
  case "incoming_call":
      payload := webpush.Payload{
          Title: data.CallerName,
          Body:  "Incoming " + ifElse(data.IsVideo, "video", "voice") + " call",
          Tag:   "call-" + data.CallID,
          RequireInteraction: true,
          Actions: []Action{{Action: "accept", Title: "Accept"}, {Action: "decline", Title: "Decline"}},
          Data: data,
      }
      gateway.SendPushToUser(userID, payload, webpush.Urgency("high"))
  ```
- [ ] **E4.3** `web/public/service-worker.js` (или где SW в проекте) — обработчик push event для type=incoming_call:
  ```javascript
  self.addEventListener('push', event => {
      const data = event.data.json();
      if (data.type === 'incoming_call') {
          event.waitUntil(
              self.registration.showNotification(data.caller_name, {
                  body: data.is_video ? 'Incoming video call' : 'Incoming voice call',
                  tag: 'call-' + data.call_id,
                  requireInteraction: true,
                  actions: [
                      { action: 'accept', title: 'Accept' },
                      { action: 'decline', title: 'Decline' },
                  ],
                  data,
              })
          );
      }
  });
  self.addEventListener('notificationclick', event => {
      event.notification.close();
      const data = event.notification.data;
      if (event.action === 'accept') {
          event.waitUntil(clients.openWindow('/?accept_call=' + data.call_id));
      } else if (event.action === 'decline') {
          // POST /api/v1/calls/:id/decline
          event.waitUntil(fetch(`/api/v1/calls/${data.call_id}/decline`, { method: 'PUT', credentials: 'include' }));
      } else {
          event.waitUntil(clients.openWindow('/?open_call=' + data.call_id));
      }
  });
  ```
- [ ] **E4.4** Frontend — при старте читать `?accept_call=` / `?open_call=` из URL → auto-accept или show UI.
- [ ] **E4.5** Проверить что VAPID уже настроен (VAPID_PUBLIC_KEY/VAPID_PRIVATE_KEY в .env — да, есть).

### Тестирование

1. alice залогинена в Chrome окне 1, subscribe to push (есть в settings).
2. Закрыть окно 1 (или background tab).
3. bob звонит alice из окна 2.
4. alice получает system notification "Bob · Incoming voice call" с кнопками Accept/Decline.
5. Click Accept → окно открывается → call auto-accept.
6. Click Decline → notification dismissed → bob видит "Declined".

### Commit message
```
feat(calls): high-priority push for incoming calls when app is closed

- Publish incoming_call push event when callee is offline
- Service Worker handles push with accept/decline actions
- Auto-accept via ?accept_call= URL param
```

---

## Этап 5: Polish

**Цель:** network quality indicator, call rating.
**Scope:** ТЗ Should + Nice.
**Файлы:** ~6, ~150 строк.

### Задачи

#### Network quality indicator (ТЗ Should)

- [ ] **E5.1** `web/src/lib/secret-sauce/p2p.ts` — periodic `peerConnection.getStats()` каждые 2 секунды:
  ```typescript
  setInterval(async () => {
      const stats = await conn.getStats();
      let rtt = 0, packetLoss = 0, bitrate = 0;
      stats.forEach(s => {
          if (s.type === 'remote-inbound-rtp') {
              rtt = (s as any).roundTripTime || 0;
              packetLoss = (s as any).packetsLost / ((s as any).packetsReceived || 1);
          }
      });
      onEmitUpdate({
          '@type': 'updatePhoneCallConnectionQuality',
          quality: rtt < 0.1 && packetLoss < 0.02 ? 4 : rtt < 0.3 && packetLoss < 0.05 ? 2 : 1,
      });
  }, 2000);
  ```
- [ ] **E5.2** `web/src/api/types/updates.ts` — добавить `ApiUpdatePhoneCallConnectionQuality`.
- [ ] **E5.3** `web/src/global/actions/apiUpdaters/calls.ts` — handler обновляет `phoneCall.connectionQuality`.
- [ ] **E5.4** `PhoneCall.tsx` — показывать иконку связи 📶 (4 bars) в углу (как в нативных call apps).

#### Call rating (ТЗ Nice)

- [ ] **E5.5** Новая миграция `migrations/037_call_rating.sql`:
  ```sql
  ALTER TABLE calls ADD COLUMN rating INT CHECK (rating IS NULL OR (rating >= 1 AND rating <= 5));
  ALTER TABLE calls ADD COLUMN rating_comment TEXT;
  ALTER TABLE calls ADD COLUMN rated_by UUID REFERENCES users(id);
  ALTER TABLE calls ADD COLUMN rated_at TIMESTAMPTZ;
  ```
- [ ] **E5.6** `call_handler.go` — новый endpoint `POST /calls/:id/rating`, body `{rating: 1-5, comment: string}`. Проверить что caller был участником call.
- [ ] **E5.7** `call_service.go:RateCall(callID, userID, rating, comment)` — UPDATE calls SET rating=$1, rating_comment=$2, rated_by=$3, rated_at=NOW() WHERE id=$4 AND rated_by IS NULL (один raters).
- [ ] **E5.8** `web/src/api/saturn/methods/calls.ts:setCallRating` — реальная реализация:
  ```typescript
  export async function setCallRating({ callId, rating, comment }: {...}) {
      return request('POST', `/calls/${callId}/rating`, { rating, comment });
  }
  ```
- [ ] **E5.9** `RatePhoneCallModal.tsx` — submit через `callApi('setCallRating', {...})`, после успеха — close modal.
- [ ] **E5.10** Триггер показа модалки — после discardCall если duration > 10s.

### Commit message
```
feat(calls): network quality indicator + call rating

- peerConnection.getStats() → 4-bar quality indicator in PhoneCall UI
- POST /calls/:id/rating endpoint + migration 037
- RatePhoneCallModal submission wired to Saturn API

Closes Phase 6 — all Must/Should/Nice features complete.
```

---

## Тестирование в Chrome

**Setup:**
1. `docker compose up` — вся инфра и сервисы.
2. `cd web && npm run dev` — фронт HMR (порт 3000), если не используется docker web контейнер.
3. Chrome окно 1 (regular) — alice.
4. Chrome окно 2 (incognito) — bob.
5. Для группы — третье окно другой user profile или на Guest.

**Permissions:** при первом вызове Chrome спросит mic/camera — Allow. Если заблокировано — `chrome://settings/content/microphone` и `camera`, разрешить `localhost`.

**Debugging:**
- `chrome://webrtc-internals/` — все RTCPeerConnection + stats real-time.
- Chrome DevTools → Network → WS — смотреть offer/answer/ICE messages.
- Backend logs: `docker compose logs -f calls gateway`.

**Smoke suite (прогон после каждого этапа):**

| # | Сценарий | Этап |
|---|----------|------|
| 1 | P2P voice alice→bob, both hear | 1 |
| 2 | P2P video alice→bob, both see | 1 |
| 3 | Hangup cleans state | 1 |
| 4 | Decline shows correctly | 1 |
| 5 | Missed after 60s timeout | 1 |
| 6 | Vibration on incoming | 1 |
| 7 | Connection state visible | 1 |
| 8 | Mute syncs between peers | 2 |
| 9 | Screen share visible to peer | 2 |
| 10 | Camera off shows avatar | 2 |
| 11 | 3-user group call works | 3 |
| 12 | Group participant join/leave updates grid | 3 |
| 13 | Group screen share | 3 |
| 14 | Push notification when app closed | 4 |
| 15 | Accept from notification | 4 |
| 16 | Quality indicator bars | 5 |
| 17 | Rating modal submits | 5 |

---

## Стартер-промты для новых чатов

Каждый этап (кроме этапа 1, который будет в этом чате) — новый чат. Используй эти промты.

### Этап 2

```
Продолжаем Phase 6 Orbit Messenger — звонки. Этап 1 (P2P стабилизация) закрыт
(коммит <ХЭШ>). Начинаем Этап 2: media state sync.

Прочитай:
1. D:\job\orbit\docs\calls-plan.md — раздел "Этап 2: Media state sync"
2. D:\job\orbit\PHASES.md — секция Phase 6 (что отмечено [x])
3. D:\job\orbit\CLAUDE.md — общие правила

Задачи E2.1-E2.6 из плана. Файлы:
- web/src/lib/secret-sauce/p2p.ts (screen share, mute, camera)
- web/src/components/calls/PhoneCall.tsx (media state UI)
- web/src/api/saturn/updates/wsHandler.ts (handleCallMuteChanged)

Тестовые юзеры:
- alice@orbit.test / TestPass123!
- bob@orbit.test / TestPass123!

После всех правок — собери, протестируй в двух окнах Chrome (см. Testing section в плане),
закоммить и обнови PHASES.md.
```

### Этап 3

```
Продолжаем Phase 6 Orbit Messenger — звонки. Этапы 1-2 закрыты. Начинаем Этап 3:
Pion SFU для групповых звонков. Самый крупный этап.

Прочитай в обязательном порядке:
1. D:\job\orbit\docs\calls-plan.md — раздел "Этап 3: Pion SFU (группы)" ПОЛНОСТЬЮ
2. D:\job\orbit\PHASES.md — секция Phase 6
3. D:\job\orbit\CLAUDE.md
4. services/calls/internal/service/call_service.go (как устроен существующий P2P flow)
5. services/gateway/internal/ws/handler.go (signaling relay для reference)

Перед кодом изучи Pion WebRTC через Context7:
- mcp__context7__resolve-library-id "pion webrtc"
- mcp__context7__query-docs про SFU, TrackLocalStaticRTP, track forwarding
- Посмотри pion/webrtc/examples/broadcast и examples/sfu на github

Задачи E3.B1-E3.B10 (backend) и E3.F1-E3.F5 (frontend) из плана.

Новые файлы:
- services/calls/internal/webrtc/sfu.go
- services/calls/internal/webrtc/room.go
- services/calls/internal/webrtc/peer.go
- web/src/lib/secret-sauce/sfu.ts (или адаптация secretsauce.ts)

Модификации:
- services/calls/go.mod (add pion/webrtc/v4)
- services/calls/internal/handler/call_handler.go (SFU WS endpoint)
- services/calls/cmd/main.go (SFU init + cleanup goroutine)
- services/gateway/internal/handler/proxy.go (WS proxy к /calls/:id/sfu-ws)
- web/src/api/saturn/methods/calls.ts (real group call methods)
- web/src/api/saturn/updates/wsHandler.ts (call_participant_joined/left handlers)

Тест: 3 окна Chrome (alice, bob, charlie) в group chat, scenarios 11-13 из плана.
```

### Этап 4

```
Продолжаем Phase 6 Orbit Messenger — звонки. Этапы 1-3 закрыты. Начинаем Этап 4:
push для закрытого app.

Прочитай:
1. D:\job\orbit\docs\calls-plan.md — раздел "Этап 4"
2. PHASES.md
3. CLAUDE.md
4. Существующий push flow для messages: grep "VAPID\|webpush\|push_subscriber" в services/gateway/
5. Service Worker: web/public/service-worker.js (или поиск "addEventListener('push'" в web/src/)

Задачи E4.1-E4.5 из плана.

VAPID ключи уже настроены (VAPID_PUBLIC_KEY в .env).

Тест: alice subscribe → закрой tab → bob звонит → system notification с accept/decline.
```

### Этап 5

```
Продолжаем Phase 6 Orbit Messenger — звонки. Этапы 1-4 закрыты. Последний Этап 5: polish.

Прочитай:
1. D:\job\orbit\docs\calls-plan.md — раздел "Этап 5"
2. PHASES.md

Задачи E5.1-E5.10:
- Network quality indicator через peerConnection.getStats() (E5.1-E5.4)
- Call rating: migration 037 + endpoint + RatePhoneCallModal wiring (E5.5-E5.10)

После этого Phase 6 полностью закрыта. Обнови PHASES.md финально и сделай summary commit.
```

---

## Git/Progress tracking

После каждого этапа:
1. Коммит по шаблону из соответствующего раздела.
2. `git push origin master` (Saturn.ac auto-deploy).
3. Обновить `PHASES.md` — отметить [x] закрытые пункты в Phase 6 секции.
4. Обновить этот файл (`docs/calls-plan.md`) — отметить [x] перед каждой задачей этапа, добавить дату.

## Точки провала и как их обойти

1. **coturn unreachable** — Этап 1 Chrome будет ругаться что TURN не достижим. Dev workaround: `host.docker.internal` работает в Chrome на том же хосте. Production: реальный FQDN.
2. **Pion codec negotiation** — VP8 vs VP9 vs H264. Стартовать с VP8+Opus (наиболее совместимо).
3. **WS proxy для SFU** — gateway должен proxy WebSocket upgrade корректно. Если fiber не умеет — либо прямой коннект клиент→calls:8084 (без gateway), либо reverse proxy через httputil.
4. **Vibration API** — не работает в desktop Chrome если window не focused. Это ожидаемо — vibration для мобильных PWA.
5. **Service Worker scope** — push notifications требуют SW зарегистрирован на корректный scope.

---

**Last updated:** 2026-04-09
**Author:** Claude (CTO role)
**Base commit:** aa6ef2f
**Stage 1 closed:** 083570d (fix(calls): stabilize P2P — TURN public URL, error propagation, ICE timeouts) + 6ec571e (fix ExpireRinging SQL type mismatch)
**Stage 2 closed:** f1c31a1 (feat) + b80a0d1 (fix: post-QA hardening B1–B8)
**Stage 2 QA:** PASS — all 8 scenarios green (live UI + code review); B1 decline-stuck-modal blocker confirmed fixed live.
**Stages 2-5:** pending — open a new chat using the starter prompts above.
