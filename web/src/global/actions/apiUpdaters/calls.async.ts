import type { ApiPhoneCall } from '../../../api/types';
import type { ApiCallProtocol } from '../../../lib/secret-sauce';
import type { ActionReturnType } from '../../types';

import {
  handleUpdateGroupCallConnection,
  handleUpdateGroupCallParticipants,
  joinPhoneCall, processSignalingMessage,
} from '../../../lib/secret-sauce';
import { ARE_CALLS_SUPPORTED } from '../../../util/browser/windowEnvironment';
import { getCurrentTabId } from '../../../util/establishMultitabRole';
import { omit } from '../../../util/iteratees';
import * as langProvider from '../../../util/oldLangProvider';
import { EMOJI_DATA, EMOJI_OFFSETS } from '../../../util/phoneCallEmojiConstants';
import { callApi } from '../../../api/saturn';
import { fetchICEServers, getActiveCallId, iceServersToConnections } from '../../../api/saturn/methods/calls';
import { addActionHandler, getGlobal, setGlobal } from '../../index';
import { updateGroupCall, updateGroupCallParticipant } from '../../reducers/calls';
import { updateTabState } from '../../reducers/tabs';
import { selectActiveGroupCall, selectGroupCallParticipant, selectPhoneCallUser } from '../../selectors/calls';

addActionHandler('apiUpdate', (global, actions, update): ActionReturnType => {
  const { activeGroupCallId } = global.groupCalls;

  switch (update['@type']) {
    case 'updateGroupCallLeavePresentation': {
      actions.toggleGroupCallPresentation({ value: false });
      break;
    }
    case 'updateGroupCallStreams': {
      if (!update.userId || !activeGroupCallId) break;
      if (!selectGroupCallParticipant(global, activeGroupCallId, update.userId)) break;

      return updateGroupCallParticipant(global, activeGroupCallId, update.userId, omit(update, ['@type', 'userId']));
    }
    case 'updateGroupCallConnectionState': {
      if (!activeGroupCallId) break;

      if (update.connectionState === 'disconnected') {
        if ('leaveGroupCall' in actions) actions.leaveGroupCall({ isFromLibrary: true, tabId: getCurrentTabId() });
        break;
      }

      return updateGroupCall(global, activeGroupCallId, {
        connectionState: update.connectionState,
        isSpeakerDisabled: update.isSpeakerDisabled,
      });
    }
    case 'updateGroupCallParticipants': {
      const { groupCallId, participants } = update;
      if (activeGroupCallId === groupCallId) {
        void handleUpdateGroupCallParticipants(participants);
      }
      break;
    }
    case 'updateGroupCallConnection': {
      if (update.data.stream) {
        actions.showNotification({ message: 'Big live streams are not yet supported', tabId: getCurrentTabId() });
        if ('leaveGroupCall' in actions) actions.leaveGroupCall({ tabId: getCurrentTabId() });
        break;
      }
      void handleUpdateGroupCallConnection(update.data, update.presentation);

      const groupCall = selectActiveGroupCall(global);
      if (groupCall?.participants && Object.keys(groupCall.participants).length > 0) {
        void handleUpdateGroupCallParticipants(Object.values(groupCall.participants));
      }
      break;
    }
    case 'updatePhoneCallMediaState':
      return {
        ...global,
        phoneCall: {
          ...global.phoneCall,
          ...omit(update, ['@type']),
        } as ApiPhoneCall,
      };
    case 'updatePhoneCallPeerState': {
      if (!global.phoneCall) return undefined;
      return {
        ...global,
        phoneCall: {
          ...global.phoneCall,
          ...omit(update, ['@type']),
        } as ApiPhoneCall,
      };
    }
    case 'updatePhoneCall': {
      if (!ARE_CALLS_SUPPORTED) return undefined;
      const { phoneCall, currentUserId } = global;

      // Terminal "discarded" updates must always be applied to the current
      // phoneCall when IDs match, even if the incoming payload is minimal.
      // Previously a state='discarded' update with a mismatched id would be
      // silently dropped which left the caller stuck in "requesting…" when
      // the callee declined. Now we only bail on ID mismatch for NON-terminal
      // events (actual concurrent call from a different peer).
      const call: ApiPhoneCall = {
        ...phoneCall,
        ...update.call,
      };

      const isOutgoing = phoneCall?.adminId === currentUserId;
      const isTerminalUpdate = update.call.state === 'discarded';

      global = {
        ...global,
        phoneCall: call,
      };
      setGlobal(global);
      global = getGlobal();

      if (!isTerminalUpdate && phoneCall && phoneCall.id && call.id !== phoneCall.id) {
        if (call.state !== 'discarded') {
          callApi('discardCall', {
            call,
            isBusy: true,
          });
        }
        return undefined;
      }

      const {
        accessHash, state, connections, gB,
      } = call;

      const isSaturnCall = !accessHash || accessHash === '';

      if (state === 'active' || state === 'accepted') {
        if (!isSaturnCall && !verifyPhoneCallProtocol(call.protocol)) {
          const user = selectPhoneCallUser(global);
          if ('hangUp' in actions) actions.hangUp({ tabId: getCurrentTabId() });
          actions.showNotification({
            message: langProvider.oldTranslate('VoipPeerIncompatible', user?.firstName),
            tabId: getCurrentTabId(),
          });
          return undefined;
        }
      }

      if (state === 'discarded') {
        // Terminal transition — always hide the call panel. The old guard
        // `if (!phoneCall) return undefined;` left the panel stuck visible
        // when the decline WS event raced the initial phoneCall setup.
        // eslint-disable-next-line no-console
        console.info('[Calls] phoneCall → discarded', { callId: call.id, reason: call.reason });
        return updateTabState(global, {
          ...(call.needRating && { ratingPhoneCall: call }),
          isCallPanelVisible: undefined,
        }, getCurrentTabId());
      } else if (state === 'accepted' && !isSaturnCall && accessHash && gB) {
        // DH confirmation — only for Telegram calls, never Saturn
        (async () => {
          const { gA, keyFingerprint, emojis } = await callApi('confirmPhoneCall', [gB, EMOJI_DATA, EMOJI_OFFSETS]);

          global = getGlobal();
          const newCall = {
            ...global.phoneCall,
            emojis,
          } as ApiPhoneCall;

          global = {
            ...global,
            phoneCall: newCall,
          };
          setGlobal(global);

          callApi('confirmCall', {
            call, gA, keyFingerprint,
          });
        })();
      } else if (state === 'active' && phoneCall?.state !== 'active') {
        if (isSaturnCall) {
          // Saturn path: fetch ICE servers if not already on the call, then join
          (async () => {
            let callConnections = connections;
            if (!callConnections) {
              const callId = getActiveCallId() || call.id;
              if (callId) {
                const iceServers = await fetchICEServers({ callId });
                if (iceServers) {
                  callConnections = iceServersToConnections(iceServers);
                }
              }
              if (!callConnections || !callConnections.length) {
                callConnections = [{
                  ip: 'stun.l.google.com', ipv6: '', port: 19302,
                  username: '', password: '', isStun: true,
                }];
              }
            }
            void joinPhoneCall(
              callConnections,
              actions.sendSignalingData,
              isOutgoing,
              Boolean(call?.isVideo),
              true,
              actions.apiUpdate,
            );
          })();
        } else if (connections) {
          // Telegram path: DH confirmation + join
          if (!isOutgoing) {
            callApi('receivedCall', { call });
            (async () => {
              const { emojis } = await callApi('confirmPhoneCall', [call.gAOrB!, EMOJI_DATA, EMOJI_OFFSETS]);

              global = getGlobal();
              const newCall = {
                ...global.phoneCall,
                emojis,
              } as ApiPhoneCall;

              global = {
                ...global,
                phoneCall: newCall,
              };
              setGlobal(global);
            })();
          }
          void joinPhoneCall(
            connections,
            actions.sendSignalingData,
            isOutgoing,
            Boolean(call?.isVideo),
            Boolean(call.isP2pAllowed),
            actions.apiUpdate,
          );
        }
      }

      return global;
    }
    case 'updatePhoneCallConnectionState': {
      const { connectionState } = update;

      if (!global.phoneCall) return global;

      // `disconnected` is a recoverable state — RTCPeerConnection may
      // automatically restart ICE and transition back to `connected`. Hanging
      // up immediately killed calls on transient network blips. Only hang up
      // on `closed` or `failed` (both are terminal).
      if (connectionState === 'closed' || connectionState === 'failed') {
        if ('hangUp' in actions) actions.hangUp({ tabId: getCurrentTabId() });
        return undefined;
      }

      return {
        ...global,
        phoneCall: {
          ...global.phoneCall,
          isConnected: connectionState === 'connected',
        },
      };
    }
    case 'updatePhoneCallSignalingData': {
      const { phoneCall } = global;

      if (!phoneCall) {
        break;
      }

      callApi('decodePhoneCallData', [update.data])?.then((msg) => {
        if (msg) processSignalingMessage(msg);
      });
      break;
    }
    case 'updateWebRTCSignaling': {
      // Saturn WebRTC signaling: parse JSON payload and feed to secret-sauce
      const { phoneCall: currentCall } = global;
      if (!currentCall) break;

      const { signalingType, sdp, candidate } = update as any;
      const rawData = signalingType === 'webrtc_ice_candidate' ? candidate : sdp;
      if (!rawData) break;

      try {
        const message = JSON.parse(rawData);
        processSignalingMessage(message);
      } catch (e) {
        // eslint-disable-next-line no-console
        console.error('[Calls] Failed to parse WebRTC signaling:', e);
      }
      break;
    }
  }

  return undefined;
});

function verifyPhoneCallProtocol(protocol?: ApiCallProtocol) {
  return protocol?.libraryVersions.some((version) => {
    return version === '4.0.0' || version === '4.0.1';
  });
}
