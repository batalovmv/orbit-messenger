import { black, silence } from './blacksilence';
import type { ApiPhoneCallConnection, P2pParsedSdp } from './types';
import parseSdp from './parseSdp';
import type { MediaContent, MediaStateMessage, P2pMessage } from './p2pMessage';
import {
  fromTelegramSource,
  IS_ECHO_CANCELLATION_SUPPORTED,
  IS_NOISE_SUPPRESSION_SUPPORTED, 
  p2pPayloadTypeToConference,
} from './utils';
import buildSdp, { Conference } from './buildSdp';
import { StreamType } from './secretsauce';

type P2pState = {
  connection: RTCPeerConnection;
  dataChannel: RTCDataChannel;
  emitSignalingData: (data: P2pMessage) => void;
  onUpdate: (...args: any[]) => void;
  conference?: Partial<Conference>;
  isOutgoing: boolean;
  pendingCandidates: string[];
  pendingCandidateTimer?: ReturnType<typeof setTimeout>;
  streams: {
    video?: MediaStream;
    audio?: MediaStream;
    presentation?: MediaStream;
    ownAudio?: MediaStream;
    ownVideo?: MediaStream;
    ownPresentation?: MediaStream;
  };
  silence: MediaStream;
  blackVideo: MediaStream;
  blackPresentation: MediaStream;
  mediaState: Omit<MediaStateMessage, '@type'>;
  audio: HTMLAudioElement;
  gotInitialSetup?: boolean;
  facingMode?: VideoFacingModeEnum;
};

let state: P2pState | undefined;

const ICE_CANDIDATE_POOL_SIZE = 10;
// If InitialSetup never arrives, queued ICE candidates are orphaned and the
// call hangs silently. Abort the call after this timeout so the UI can recover.
const INITIAL_SETUP_TIMEOUT_MS = 15_000;

export function getStreams() {
  return state?.streams;
}

function updateStreams() {
  state?.onUpdate({
    ...state.mediaState,
    '@type': 'updatePhoneCallMediaState',
  });
}

function getUserStream(streamType: StreamType, facing: VideoFacingModeEnum = 'user') {
  if (streamType === 'presentation') {
    return (navigator.mediaDevices as any).getDisplayMedia({
      audio: false,
      video: true,
    });
  }

  return navigator.mediaDevices.getUserMedia({
    audio: streamType === 'audio' ? {
      ...(IS_ECHO_CANCELLATION_SUPPORTED && { echoCancellation: true }),
      ...(IS_NOISE_SUPPRESSION_SUPPORTED && { noiseSuppression: true }),
    } : false,
    video: streamType === 'video' ? {
      facingMode: facing,
    } : false,
  });
}

export async function switchCameraInputP2p() {
  if (!state || !state.facingMode) {
    return;
  }

  const stream = state.streams.ownVideo;

  if (!stream) return;

  const track = stream.getTracks()[0];

  if (!track) {
    return;
  }

  const sender = state.connection.getSenders().find((l) => track.id === l.track?.id);

  if (!sender) {
    return;
  }

  state.facingMode = state.facingMode === 'environment' ? 'user' : 'environment';
  try {
    const newStream = await getUserStream('video', state.facingMode);

    await sender.replaceTrack(newStream.getTracks()[0]);
    state.streams.ownVideo = newStream;
    updateStreams();
  } catch (e) {

  }
}

export async function toggleStreamP2p(streamType: StreamType, value: boolean | undefined = undefined) {
  if (!state) return;
  const stream = streamType === 'audio' ? state.streams.ownAudio
    : (streamType === 'video' ? state.streams.ownVideo : state.streams.ownPresentation);

  if (!stream) return;
  const track = stream.getTracks()[0];

  if (!track) {
    return;
  }

  const sender = state.connection.getSenders().find((l) => track.id === l.track?.id);

  if (!sender) {
    return;
  }

  value = value === undefined ? !track.enabled : value;

  // Track the stream we acquired so we can stop it if any subsequent step
  // throws — otherwise the mic/camera LED stays on and the device is
  // locked until the tab is closed.
  let acquiredStream: MediaStream | undefined;
  try {
    if (value && !track.enabled) {
      const newStream = await getUserStream(streamType);
      acquiredStream = newStream;
      newStream.getTracks()[0].onended = () => {
        toggleStreamP2p(streamType, false);
      };
      await sender.replaceTrack(newStream.getTracks()[0]);
      if (streamType === 'audio') {
        state.streams.ownAudio = newStream;
      } else if (streamType === 'video') {
        state.streams.ownVideo = newStream;
        state.facingMode = 'user';
      } else {
        state.streams.ownPresentation = newStream;
      }
      acquiredStream = undefined; // ownership handed off to state
      if (streamType === 'video' || streamType === 'presentation') {
        toggleStreamP2p(streamType === 'video' ? 'presentation' : 'video', false);
      }
      // if (streamType === 'video') {
      //   state.facingMode = 'user';
      // }
    } else if (!value && track.enabled) {
      track.stop();
      const newStream = streamType === 'audio' ? state.silence
        : (streamType === 'video' ? state.blackVideo : state.blackPresentation);
      if (!newStream) return;

      await sender.replaceTrack(newStream.getTracks()[0]);

      if (streamType === 'audio') {
        state.streams.ownAudio = newStream;
      } else if (streamType === 'video') {
        state.streams.ownVideo = newStream;
      } else {
        state.streams.ownPresentation = newStream;
      }
      // if (streamType === 'video') {
      //   state.facingMode = undefined;
      // }
    }
    updateStreams();
    sendMediaState();
  } catch (err) {
    // If getUserMedia or replaceTrack threw, we may hold an orphaned stream
    // that the browser never released. Stop its tracks so the mic/camera LED
    // doesn't stay lit and the device is available for the next attempt.
    acquiredStream?.getTracks().forEach((t) => t.stop());
    // eslint-disable-next-line no-console
    console.error('[p2p] toggleStreamP2p failed', err);
  }
}

export async function joinPhoneCall(
  connections: ApiPhoneCallConnection[],
  emitSignalingData: (data: P2pMessage) => void,
  isOutgoing: boolean,
  shouldStartVideo: boolean,
  isP2p: boolean,
  onUpdate: (...args: any[]) => void,
) {
  const conn = new RTCPeerConnection({
    iceServers: connections.map((connection) => (
      {
        urls: [
          connection.isTurn && `turn:${connection.ip}:${connection.port}`,
          connection.isStun && `stun:${connection.ip}:${connection.port}`,
        ].filter(Boolean),
        username: connection.username,
        credentialType: 'password',
        credential: connection.password,
      }
    )),
    iceTransportPolicy: isP2p ? 'all' : 'relay',
    bundlePolicy: 'max-bundle',
    iceCandidatePoolSize: ICE_CANDIDATE_POOL_SIZE,
  });

  conn.onicecandidate = (e) => {
    if (!e.candidate) {
      return;
    };
    emitSignalingData({
      '@type': 'Candidates',
      candidates: [{
        sdpString: e.candidate.candidate,
      }],
    });
  };

  conn.onconnectionstatechange = () => {
    onUpdate({
      '@type': 'updatePhoneCallConnectionState',
      connectionState: conn.connectionState,
    });
  };

  conn.ontrack = (e) => {
    if (!state) return;

    const stream = e.streams[0];
    if (e.track.kind === 'audio') {
      state.audio.srcObject = stream;
      state.audio.play().catch();
      state.streams.audio = stream;
    } else if (e.transceiver.mid === '1') {
      state.streams.video = stream;
    } else {
      state.streams.presentation = stream;
    }

    updateStreams();
  };

  conn.oniceconnectionstatechange = async (e) => {
    switch(conn.iceConnectionState) {
      case 'disconnected':
      case 'failed':
        if (isOutgoing) {
          await createOffer(conn, {
            offerToReceiveAudio: true,
            offerToReceiveVideo: true,
            iceRestart: true,
          });
        }
      default:
        break;
    }
  }

  const slnc = silence(new AudioContext());
  const video = black({ width: 640, height: 480 });
  const screenshare = black({ width: 640, height: 480 });
  conn.addTrack(slnc.getTracks()[0], slnc);
  conn.addTrack(video.getTracks()[0], video);
  conn.addTrack(screenshare.getTracks()[0], screenshare);

  const dc = conn.createDataChannel('data', {
    id: 0,
    negotiated: true,
  });

  dc.onmessage = (e) => {
    // Malformed messages from the peer must not crash the whole channel.
    try {
      const parsed = JSON.parse(e.data);
      processSignalingMessage(parsed);
    } catch (err) {
      // eslint-disable-next-line no-console
      console.warn('[p2p] failed to parse data channel message', err);
    }
  };

  const audio = new Audio();

  state = {
    audio,
    connection: conn,
    emitSignalingData,
    isOutgoing,
    pendingCandidates: [],
    onUpdate,
    streams: {
      ownVideo: video,
      ownAudio: slnc,
      ownPresentation: screenshare,
    },
    mediaState: {
      isBatteryLow: false,
      screencastState: 'inactive',
      videoState: 'inactive',
      videoRotation: 0,
      isMuted: true,
    },
    blackVideo: video,
    blackPresentation: screenshare,
    silence: slnc,
    dataChannel: dc,
  };

  try {
    toggleStreamP2p('audio', true);
  } catch (err) {
    console.error(err)
  }

  if (isOutgoing) {
    try {
      await createOffer(conn, {
        offerToReceiveAudio: true,
        offerToReceiveVideo: true,
      });
    } catch (err) {
      // eslint-disable-next-line no-console
      console.error('[p2p] createOffer failed, discarding call', err);
      onUpdate({
        '@type': 'updatePhoneCallConnectionState',
        connectionState: 'failed',
      });
      stopPhoneCall();
    }
  }
}

export function stopPhoneCall() {
  if (!state) return;

  if (state.pendingCandidateTimer) {
    clearTimeout(state.pendingCandidateTimer);
  }
  state.streams.ownVideo?.getTracks().forEach((track) => track.stop());
  state.streams.ownPresentation?.getTracks().forEach((track) => track.stop());
  state.streams.ownAudio?.getTracks().forEach((track) => track.stop());
  try { state.dataChannel.close(); } catch { /* already closed */ }
  try { state.connection.close(); } catch { /* already closed */ }
  state = undefined;
}

function sendMediaState() {
  if (!state) return;
  const { emitSignalingData, streams } = state;

  emitSignalingData({
    '@type': 'MediaState',
    videoRotation: 0,
    isMuted: !streams.ownAudio?.getTracks()[0].enabled,
    isBatteryLow: true,
    videoState: streams.ownVideo?.getTracks()[0].enabled ? 'active' : 'inactive',
    screencastState: streams.ownPresentation?.getTracks()[0].enabled ? 'active' : 'inactive',
  });
}

function filterVP8(mediaContent: MediaContent) {
  if (!state || state.isOutgoing) return mediaContent;

  const { payloadTypes } = mediaContent!;
  const idx = payloadTypes.findIndex((payloadType) => payloadType.name === 'VP8');
  const vp8PayloadType = payloadTypes[idx];
  const rtxIdx = payloadTypes.findIndex((payloadType) => Number(payloadType.parameters?.apt) === vp8PayloadType.id);
  mediaContent.payloadTypes = [payloadTypes[idx], payloadTypes[rtxIdx]];

  return mediaContent;
}

function sendInitialSetup(sdp: P2pParsedSdp) {
  if (!state) return;
  const { emitSignalingData } = state;

  if (!sdp.ssrc || !sdp['ssrc-groups'] || !sdp['ssrc-groups'][0] || !sdp['ssrc-groups'][1]) return;

  emitSignalingData({
    '@type': 'InitialSetup',
    fingerprints: sdp.fingerprints,
    ufrag: sdp.ufrag,
    pwd: sdp.pwd,
    audio: {
      ssrc: fromTelegramSource(sdp.ssrc).toString(),
      ssrcGroups: [],
      payloadTypes: sdp.audioPayloadTypes,
      rtpExtensions: sdp.audioExtmap,
    },
    video: filterVP8({
      ssrc: fromTelegramSource(sdp['ssrc-groups'][0].sources[0]).toString(),
      ssrcGroups: [{
        semantics: sdp['ssrc-groups'][0].semantics,
        ssrcs: sdp['ssrc-groups'][0].sources.map(fromTelegramSource),
      }],
      payloadTypes: sdp.videoPayloadTypes,
      rtpExtensions: sdp.videoExtmap,
    }),
    screencast: filterVP8({
      ssrc: fromTelegramSource(sdp['ssrc-groups'][1].sources[0]).toString(),
      ssrcGroups: [{
        semantics: sdp['ssrc-groups'][1].semantics,
        ssrcs: sdp['ssrc-groups'][1].sources.map(fromTelegramSource),
      }],
      payloadTypes: sdp.screencastPayloadTypes,
      rtpExtensions: sdp.screencastExtmap,
    }),
  });
}

export async function processSignalingMessage(message: P2pMessage) {
  if (!state || !state.connection) return;

  switch (message['@type']) {
    case 'MediaState': {
      state.mediaState = message;
      updateStreams();
      sendMediaState();
      break;
    }
    case 'Candidates': {
      const { pendingCandidates, gotInitialSetup } = state;
      message.candidates.forEach((candidate) => {
        pendingCandidates.push(candidate.sdpString);
      });
      if (gotInitialSetup) {
        await commitPendingIceCandidates();
      } else if (!state.pendingCandidateTimer) {
        // Guard against orphaned candidates: if InitialSetup never arrives
        // we'd wait forever. Force-fail the call after a sane timeout so the
        // UI can discard and the user can retry.
        state.pendingCandidateTimer = setTimeout(() => {
          if (!state || state.gotInitialSetup) return;
          // eslint-disable-next-line no-console
          console.warn('[p2p] InitialSetup never arrived, discarding call');
          state.pendingCandidates = [];
          state.onUpdate({
            '@type': 'updatePhoneCallConnectionState',
            connectionState: 'failed',
          });
          stopPhoneCall();
        }, INITIAL_SETUP_TIMEOUT_MS);
      }
      break;
    }
    case 'InitialSetup': {
      const {
        connection, isOutgoing,
      } = state;
      if (!connection) return;

      const newConference = {
        transport: {
          candidates: [],
          ufrag: message.ufrag,
          pwd: message.pwd,
          fingerprints: message.fingerprints,
          'rtcp-mux': false,
          xmlns: '',
        },
        sessionId: Date.now(),
        ssrcs: [
          message.audio && {
            isVideo: false,
            isMain: false,
            userId: '123',
            endpoint: '0',
            mid: '0',
            sourceGroups: [{
              sources: [message.audio.ssrc],
            }],
          },
          message.video && {
            isVideo: true,
            isPresentation: false,
            isMain: false,
            userId: '123',
            endpoint: '1',
            mid: '1',
            sourceGroups: message.video.ssrcGroups.map((l) => ({
              semantics: l.semantics,
              sources: l.ssrcs,
            })),
          },
          message.screencast && {
            isVideo: true,
            isPresentation: true,
            isMain: false,
            userId: '123',
            endpoint: '2',
            mid: '2',
            sourceGroups: message.screencast.ssrcGroups.map((l) => ({
              semantics: l.semantics,
              sources: l.ssrcs,
            })),
          },
        ],
        audioPayloadTypes: message.audio!.payloadTypes?.map(p2pPayloadTypeToConference) || [],
        audioExtensions: message.audio!.rtpExtensions,
        videoPayloadTypes: filterVP8(message.video!).payloadTypes?.map(p2pPayloadTypeToConference) || [],
        videoExtensions: message.video!.rtpExtensions,
      } as Conference;

      await connection.setRemoteDescription({
        sdp: buildSdp(newConference, isOutgoing, undefined, true),
        type: isOutgoing ? 'answer' : 'offer',
      });

      state.conference = newConference;

      if (!isOutgoing) {
        const answer = await connection.createAnswer();
        await connection.setLocalDescription(answer);
        sendInitialSetup(parseSdp(connection.localDescription!, true) as P2pParsedSdp);
      }
      state.gotInitialSetup = true;
      if (state.pendingCandidateTimer) {
        clearTimeout(state.pendingCandidateTimer);
        state.pendingCandidateTimer = undefined;
      }
      await commitPendingIceCandidates();
      break;
    }
  }
}

async function commitPendingIceCandidates() {
  if (!state) {
    return;
  }
  const { pendingCandidates, connection } = state;
  if (!pendingCandidates.length) {
    return;
  }
  await Promise.all(pendingCandidates.map((c) => tryAddCandidate(connection, c)));
  state.pendingCandidates = [];
}

async function tryAddCandidate(connection: RTCPeerConnection, candidate: string) {
  try {
    await connection.addIceCandidate({
      candidate,
      sdpMLineIndex: 0,
    })
  } catch (err) {
    console.error(err);
  }
}

async function createOffer(conn: RTCPeerConnection, params: RTCOfferOptions) {
  const offer = await conn.createOffer(params);
  await conn.setLocalDescription(offer);
  sendInitialSetup(parseSdp(conn.localDescription!, true) as P2pParsedSdp);
}
