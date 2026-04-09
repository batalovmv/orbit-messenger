# Slot 12: Calls UI

## Metadata

- Date: 2026-04-09
- Commit: `82669bd35a1568f24eff710b1dd0074342f12dff`
- Scope:
  - `web/src/components/calls/`
  - `web/src/global/reducers/calls.ts`
  - `web/src/global/actions/api/calls.ts`
- Focus areas:
  - WebRTC client flow
  - `requestCall` dedupe on UI side
  - network quality indicator rendering
  - call rating modal
  - ICE restart handling
  - media permissions UX
  - reconnection on WS drop
  - call state machine correctness
  - memory leaks (streams, peer connections not closed)
- Severity gate:
  - Report individually: HIGH / CRITICAL
  - Report MEDIUM only with high confidence
  - LOW only in bucket

## Status

- Pass 1: completed
- Pass 2: completed
- Report: completed

## Scope Note

- Requested path `web/src/global/actions/api/calls.ts` does not exist at frozen commit `82669bd35a1568f24eff710b1dd0074342f12dff`.
- To cover the same call flow, I inspected the actual call action/update entrypoints that own this logic:
  - `web/src/global/actions/calls.ts`
  - `web/src/global/actions/api/calls.async.ts`
  - `web/src/global/actions/apiUpdaters/calls.ts`
  - `web/src/global/actions/apiUpdaters/calls.async.ts`
  - `web/src/global/actions/ui/calls.ts`

## File Checklist

- [x] `web/src/global/actions/api/calls.ts` (verified absent at this commit)
- [x] `web/src/global/reducers/calls.ts`
- [x] `web/src/components/calls/ActiveCallHeader.async.tsx`
- [x] `web/src/components/calls/ActiveCallHeader.scss`
- [x] `web/src/components/calls/ActiveCallHeader.tsx`
- [x] `web/src/components/calls/group/GroupCall.async.tsx`
- [x] `web/src/components/calls/group/GroupCall.module.scss`
- [x] `web/src/components/calls/group/GroupCall.tsx`
- [x] `web/src/components/calls/group/GroupCallParticipant.module.scss`
- [x] `web/src/components/calls/group/GroupCallParticipant.tsx`
- [x] `web/src/components/calls/group/GroupCallParticipantList.module.scss`
- [x] `web/src/components/calls/group/GroupCallParticipantList.tsx`
- [x] `web/src/components/calls/group/GroupCallParticipantMenu.scss`
- [x] `web/src/components/calls/group/GroupCallParticipantMenu.tsx`
- [x] `web/src/components/calls/group/GroupCallParticipantVideo.module.scss`
- [x] `web/src/components/calls/group/GroupCallParticipantVideo.tsx`
- [x] `web/src/components/calls/group/GroupCallTopPane.scss`
- [x] `web/src/components/calls/group/GroupCallTopPane.tsx`
- [x] `web/src/components/calls/group/helpers/formatGroupCallVolume.ts`
- [x] `web/src/components/calls/group/hooks/useGroupCallVideoLayout.ts`
- [x] `web/src/components/calls/group/MicrophoneButton.module.scss`
- [x] `web/src/components/calls/group/MicrophoneButton.tsx`
- [x] `web/src/components/calls/group/OutlinedMicrophoneIcon.tsx`
- [x] `web/src/components/calls/phone/PhoneCall.async.tsx`
- [x] `web/src/components/calls/phone/PhoneCall.module.scss`
- [x] `web/src/components/calls/phone/PhoneCall.tsx`
- [x] `web/src/components/calls/phone/PhoneCallButton.module.scss`
- [x] `web/src/components/calls/phone/PhoneCallButton.tsx`
- [x] `web/src/components/calls/phone/RatePhoneCallModal.async.tsx`
- [x] `web/src/components/calls/phone/RatePhoneCallModal.module.scss`
- [x] `web/src/components/calls/phone/RatePhoneCallModal.tsx`

## Findings

No confirmed HIGH / CRITICAL issues in the scoped call UI code after pass 2.

### 1. MEDIUM: Incoming call requests camera/microphone access before the callee accepts

- Confidence: high
- Files:
  - `web/src/global/actions/apiUpdaters/calls.ts:117`
  - `web/src/global/actions/ui/calls.ts:447`
- What happens:
  - On every incoming `updatePhoneCall` with `state === 'requested'`, the UI immediately calls `checkNavigatorUserMediaPermissions(...)`.
  - That helper directly invokes `navigator.mediaDevices.getUserMedia({ video: true })` and/or `navigator.mediaDevices.getUserMedia({ audio: true })`.
  - This runs before the user clicks Accept.
- Impact:
  - A caller can trigger unsolicited browser permission prompts on the callee side.
  - If the browser already has persistent permission, the device can be opened briefly before the callee accepts the call.
  - In practice this is a user-facing privacy/UX bug and an annoyance vector via repeated call spam.
- Why I’m confident:
  - The incoming-call path has no acceptance gate around the permission probe.
  - The helper does real `getUserMedia`, not `navigator.permissions.query`.
- Fix direction:
  - Do not probe `getUserMedia` on incoming `requested`.
  - Move device access to explicit accept / join actions, and keep pre-accept UI to passive status only.

### 2. MEDIUM: Concurrent call updates can overwrite the active `phoneCall`, then the delayed hang-up cleanup wipes whatever state is there

- Confidence: high
- Files:
  - `web/src/global/actions/apiUpdaters/calls.async.ts:98`
  - `web/src/global/actions/apiUpdaters/calls.async.ts:106`
  - `web/src/global/actions/apiUpdaters/calls.async.ts:113`
  - `web/src/global/actions/api/calls.async.ts:386`
- What happens:
  - `updatePhoneCall` merges `update.call` into the current `phoneCall` and commits it with `setGlobal(...)` before checking whether the incoming call ID matches the active call ID.
  - Only after mutating global state does it detect the mismatch and return.
  - Separately, `hangUp` keeps `phoneCall` alive for `HANG_UP_UI_DELAY = 500ms` and later clears `global.phoneCall` unconditionally.
- Impact:
  - A second incoming call or fast re-dial during the hang-up window can clobber the current call state locally.
  - The later delayed cleanup can then clear a newer `phoneCall`, causing dropped UI, incorrect busy/discard handling, or missed subsequent calls.
- Why I’m confident:
  - The mutation order is explicit in the updater.
  - The timeout cleanup does not verify call ID before nulling `global.phoneCall`.
- Fix direction:
  - Check `call.id` before writing to global state.
  - In delayed cleanup, clear only if the stored `phoneCall.id` still matches the call being torn down.

### 3. MEDIUM: Group-call audio bootstrap leaks `AudioContext` / oscillator resources across join-leave cycles

- Confidence: high
- Files:
  - `web/src/global/actions/ui/calls.ts:411`
  - `web/src/global/actions/ui/calls.ts:415`
  - `web/src/global/actions/ui/calls.ts:422`
  - `web/src/global/actions/ui/calls.ts:438`
- What happens:
  - `createAudioElement()` creates a fresh `AudioContext`, an oscillator, and a `MediaStream` for the silent bootstrap track.
  - `removeGroupCallAudioElement()` only pauses the element and drops JS references.
  - It never stops the generated track, never stops the oscillator, and never closes the `AudioContext`.
- Impact:
  - Repeated group-call joins/leaves accumulate browser audio resources.
  - Over time this can hit the browser’s `AudioContext` limits or leave stale media resources around, breaking later calls until the tab reloads.
- Why I’m confident:
  - Resource creation is explicit.
  - Cleanup does not include `track.stop()` or `audioContext.close()`.
- Fix direction:
  - Persist the bootstrap stream/oscillator handles and dispose them on leave.
  - Call `audioContext.close()` during teardown.

## Low Severity Bucket

- `web/src/components/calls/phone/PhoneCall.tsx` uses several uncancelled `setTimeout(...)` callbacks for media toggles, flip-camera animation, and auto-hangup on discard. They can fire after modal teardown and mutate a later call session.
- `web/src/global/actions/apiUpdaters/calls.ts:106` writes `ratingPhoneCall` onto root global state instead of tab state. The async updater currently covers the main path, but this synchronous updater still pollutes state and depends on handler ordering.
- `web/src/global/actions/ui/calls.ts:404` plus `web/src/components/calls/phone/PhoneCall.tsx:134` still double-dispatch `connectToActivePhoneCall()` by design. It is currently masked by deeper dedupe, but the UI layer itself has no local in-flight guard.
