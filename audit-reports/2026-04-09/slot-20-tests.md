# Slot 20 Audit Report

## Metadata
- Slot: `20-tests`
- Date: `2026-04-09`
- Commit: `82669bd35a1568f24eff710b1dd0074342f12dff`
- Scope: `services/*/internal/handler/*_test.go`, `services/*/internal/service/*_test.go`
- Focus: handler coverage, fn-field mock compliance, no mockgen/testify/mock, `miniredis` usage, `NewNoopNATSPublisher` usage, no skipped/focused tests, flaky patterns, isolation, critical-path gaps
- Severity gate: individual only `HIGH` / `CRITICAL`; `MEDIUM` only with high confidence; `LOW` as bucket

## Status
- [x] Pass 1 complete
- [x] Pass 2 complete
- [x] Report finalized

## File Checklist
- [x] `services/auth/internal/handler/auth_handler_test.go`
- [x] `services/calls/internal/handler/mock_stores_test.go`
- [x] `services/calls/internal/handler/rate_call_test.go`
- [x] `services/gateway/internal/handler/proxy_test.go`
- [x] `services/media/internal/handler/media_handler_test.go`
- [x] `services/messaging/internal/handler/chat_handler_test.go`
- [x] `services/messaging/internal/handler/gif_handler_test.go`
- [x] `services/messaging/internal/handler/invite_handler_test.go`
- [x] `services/messaging/internal/handler/media_message_test.go`
- [x] `services/messaging/internal/handler/message_handler_test.go`
- [x] `services/messaging/internal/handler/mock_phase5_stores_test.go`
- [x] `services/messaging/internal/handler/mock_stores_test.go`
- [x] `services/messaging/internal/handler/poll_handler_test.go`
- [x] `services/messaging/internal/handler/reaction_handler_test.go`
- [x] `services/messaging/internal/handler/scheduled_handler_test.go`
- [x] `services/messaging/internal/handler/search_handler_test.go`
- [x] `services/messaging/internal/handler/settings_handler_test.go`
- [x] `services/messaging/internal/handler/sticker_handler_test.go`
- [x] `services/messaging/internal/handler/user_handler_test.go`
- [x] `services/messaging/internal/service/chat_service_test.go`
- [x] `services/messaging/internal/service/gif_service_test.go`
- [x] `services/messaging/internal/service/invite_service_test.go`
- [x] `services/messaging/internal/service/message_service_test.go`
- [x] `services/messaging/internal/service/miniredis_test.go`
- [x] `services/messaging/internal/service/mock_phase5_stores_test.go`
- [x] `services/messaging/internal/service/mock_stores_test.go`
- [x] `services/messaging/internal/service/poll_service_test.go`
- [x] `services/messaging/internal/service/reaction_service_test.go`
- [x] `services/messaging/internal/service/recording_publisher_test.go`
- [x] `services/messaging/internal/service/scheduled_service_test.go`
- [x] `services/messaging/internal/service/sticker_service_test.go`

## Findings
### MEDIUM: messaging handler/service test packages do not build because `mockChatStore` drifted behind the current `store.ChatStore` interface
- Verified in `services/messaging/internal/handler/mock_stores_test.go:206` and `services/messaging/internal/service/mock_stores_test.go:187`: both mocks stop at `ListAll(...)` / `GetCommonChats(...)` / `GetOrCreateSavedChat(...)` and never implement the new `ListAllPaginated(...)` requirement.
- Pass 2 verification: `go test ./internal/handler ./internal/service` from `services/messaging` fails at compile time with `*mockChatStore does not implement store.ChatStore (missing method ListAllPaginated)`.
- Impact: the entire messaging handler/service suite is currently dead, so regressions in chat/message/invite/reaction/poll/scheduled flows are not executed at all.

### MEDIUM: auth handler tests are stale against current behavior and the package is red
- Verified in `services/auth/internal/handler/auth_handler_test.go:336-337`, `services/auth/internal/handler/auth_handler_test.go:402-404`, and `services/auth/internal/handler/auth_handler_test.go:463`.
- Pass 2 verification: `go test ./internal/handler` from `services/auth` fails with:
  - `TestBootstrap_HappyPath`: expected role `admin`, got `superadmin`
  - `TestLogin_HappyPath`: expected `200`, got `403 {"error":"forbidden","message":"Account is deactivated","status":403}`
  - `TestGetMe_WithValidToken`: panics on `loginResult["access_token"].(string)` because the previous login assumption is no longer true
- Impact: the auth handler package is not trustworthy as regression coverage right now, and follow-on tests rely on invalid token/bootstrap assumptions.

## Low Bucket
- No `t.Skip`, focused tests, `time.Sleep`, `testify/mock`, or `mockgen` usage found in scope.
- No obvious flaky real-network patterns found. The only networked case is `httptest.NewServer` in `services/gateway/internal/handler/proxy_test.go`, which is in-process and deterministic.
- `miniredis` usage is present and isolated where it matters: `services/auth/internal/handler/auth_handler_test.go` and `services/messaging/internal/service/message_service_test.go` use it for Redis-backed behavior; `services/messaging/internal/service/miniredis_test.go` wraps cleanup correctly.
- `NewNoopNATSPublisher()` usage is generally correct in handler tests that construct services with publisher dependencies: calls, chat, invite, message, poll, reaction, scheduled. Service tests sensibly switch to `RecordingPublisher`.
- Fn-field mock compliance is mixed. `services/messaging/internal/{handler,service}/mock*_stores_test.go` and `services/calls/internal/handler/mock_stores_test.go` follow the pattern; `services/auth/internal/handler/auth_handler_test.go` uses stateful map-backed mocks instead, and `services/media/internal/handler/media_handler_test.go` bypasses repo-standard mocks entirely.
- `services/media/internal/handler/media_handler_test.go` is the weakest handler suite structurally. The file explicitly says it does not construct real handlers (`:47-55`) and later tests `/media/upload` and `/media/:id/info` through ad-hoc inline Fiber routes instead of `MediaHandler` / `UploadHandler` (`:548-596`). That means real stream/get/info/delete/chunk auth/validation paths are largely untested despite the filename implying handler coverage.
- `services/gateway/internal/handler/proxy_test.go` only covers auth proxy middleware bucket wiring. There is no coverage for upstream failure/timeout handling, header forwarding, or unhappy proxy paths.
- `services/calls/internal/handler` only has `rate_call_test.go`; `CreateCall`, `AcceptCall`, `DeclineCall`, `EndCall`, `GetCall`, `ListCallHistory`, participant management, mute/screen-share, ICE, and group-call join/leave have no handler tests in scope.
- Handler coverage in `services/auth/internal/handler/auth_handler_test.go` is far short of the repo rule `happy + auth fail + validation fail` per endpoint. Entire endpoint groups are missing or partial: `Setup2FA`, `Verify2FA`, `Disable2FA`, happy-path `Refresh`, happy-path `Logout`, `ResetAdmin` success, `ValidateInvite` success, `RevokeInvite`, plus negative-path coverage for sessions/invites.
- Handler coverage in `services/messaging/internal/handler/chat_handler_test.go` is sparse relative to the surface area in `chat_handler.go`. There is no coverage for `ListChats`, `CreateDirectChat`, `UpdateChat`, `DeleteChat`, `RemoveMember`, `UpdateMemberRole`, `UpdateMemberPermissions`, `SetSlowMode`, `GetChat`, `UpdateChatPhoto`, `DeleteChatPhoto`, or `GetSavedChat`.
- `services/messaging/internal/handler/message_handler_test.go` and `services/messaging/internal/handler/media_message_test.go` cover several send-message validation cases, but they do not cover `GetMessage`, `FindByDate`, happy-path `EditMessage`, `DeleteMessage`, `PinMessage`, `UnpinMessage`, `UnpinAll`, `ListPinned`, happy-path `MarkRead`, or `GetLinkPreview`.
- `services/messaging/internal/handler/poll_handler_test.go`, `services/messaging/internal/handler/reaction_handler_test.go`, and `services/messaging/internal/handler/scheduled_handler_test.go` are heavily skewed toward auth/validation failures. Happy paths for vote/unvote/close poll, add/remove/list/get available reactions, and list/schedule/edit/delete/send-now scheduled messages are mostly absent.
- `services/messaging/internal/handler/reaction_handler_test.go:254-265` contains a non-asserting test: it sends a selected-mode request, explicitly tolerates a `500`, and then discards the response. That test would stay green even if the handler regressed badly.
- `services/messaging/internal/handler/search_handler_test.go` only exercises `/search`; `GetSearchHistory`, `SaveSearchHistory`, and `ClearSearchHistory` have no handler coverage.
- `services/messaging/internal/handler/settings_handler_test.go` only covers internal push-subscription and muted-user routes. Privacy settings, user settings, block/unblock, chat notification CRUD, subscribe/unsubscribe push, global notify settings, and notification exceptions are uncovered.
- `services/messaging/internal/handler/sticker_handler_test.go`, `services/messaging/internal/handler/gif_handler_test.go`, and `services/messaging/internal/handler/user_handler_test.go` are also one-sided. Many endpoints only have auth/validation checks, while happy paths for featured/installed/recent/import/admin sticker flows, saved GIF listing/removal success, contact/common-chat lookups, and several profile paths are missing.
- `services/messaging/internal/service` coverage is materially better than handler coverage, but still incomplete on critical helpers and endpoint-facing methods. Notably uncovered or undercovered are `ChatService.UpdateDefaultPermissions`, `ChatService.UpdateMemberPermissions`, `ChatService.GetAdmins/GetMember/SearchMembers/GetMembers/GetMemberIDs/GetCommonChats/GetOrCreateSavedChat/ClearChatPhoto`, `InviteService.ListInviteLinks/EditInviteLink/RevokeInviteLink/GetInviteInfo/ListJoinRequests`, `MessageService.ListMessages/FindByDate/GetMessage/ForwardMessages/UnpinMessage/ListPinned/ListSharedMedia/MarkRead`, `StickerService.ListInstalled/ListRecent/AddRecent/RemoveRecent/ClearRecent/CreateAdminPack/AddStickerToPack/UpdateAdminPack/DeleteAdminPack`, `GIFService.ListSaved`, and `ScheduledMessageService.ListScheduled`.

## Notes
- Verification commands:
  - `go test ./internal/handler` in `services/auth` -> fails
  - `go test ./internal/handler` in `services/calls` -> passes
  - `go test ./internal/handler` in `services/gateway` -> passes
  - `go test ./internal/handler` in `services/media` -> passes
  - `go test ./internal/handler ./internal/service` in `services/messaging` -> build fails
- Pass 2 was completed by re-reading the failing files after the command outputs, not by reporting from grep alone.
