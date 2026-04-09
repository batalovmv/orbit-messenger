import '../../../global/actions/calls';

import {
  memo, useCallback, useEffect, useMemo, useRef,
} from '../../../lib/teact/teact';
import { getActions, withGlobal } from '../../../global';

import type { ApiPhoneCall, ApiUser } from '../../../api/types';

import {
  getStreams, IS_SCREENSHARE_SUPPORTED, switchCameraInputP2p, toggleStreamP2p,
} from '../../../lib/secret-sauce';
import {
  startScreenShareApi,
  stopScreenShareApi,
  toggleCallMute as apiToggleCallMute,
} from '../../../api/saturn/methods/calls';
import { selectTabState } from '../../../global/selectors';
import { selectPhoneCallUser } from '../../../global/selectors/calls';
import {
  IS_ANDROID,
  IS_IOS,
  IS_REQUEST_FULLSCREEN_SUPPORTED,
} from '../../../util/browser/windowEnvironment';
import buildClassName from '../../../util/buildClassName';
import { formatMediaDuration } from '../../../util/dates/dateFormat';
import { getServerTime } from '../../../util/serverTime';
import { LOCAL_TGS_URLS } from '../../common/helpers/animatedAssets';
import renderText from '../../common/helpers/renderText';

import useInterval from '../../../hooks/schedulers/useInterval';
import useAppLayout from '../../../hooks/useAppLayout';
import useFlag from '../../../hooks/useFlag';
import useForceUpdate from '../../../hooks/useForceUpdate';
import useOldLang from '../../../hooks/useOldLang';

import AnimatedIcon from '../../common/AnimatedIcon';
import Avatar from '../../common/Avatar';
import Button from '../../ui/Button';
import Modal from '../../ui/Modal';
import PhoneCallButton from './PhoneCallButton';

import styles from './PhoneCall.module.scss';

type StateProps = {
  user?: ApiUser;
  phoneCall?: ApiPhoneCall;
  isOutgoing: boolean;
  isCallPanelVisible?: boolean;
};

const PhoneCall = ({
  user,
  isOutgoing,
  phoneCall,
  isCallPanelVisible,
}: StateProps) => {
  const lang = useOldLang();
  const {
    hangUp, requestMasterAndAcceptCall, playGroupCallSound, toggleGroupCallPanel, connectToActivePhoneCall,
  } = getActions();
  const containerRef = useRef<HTMLDivElement>();

  const [isFullscreen, openFullscreen, closeFullscreen] = useFlag();
  const { isMobile } = useAppLayout();

  const toggleFullscreen = useCallback(() => {
    if (isFullscreen) {
      closeFullscreen();
    } else {
      openFullscreen();
    }
  }, [closeFullscreen, isFullscreen, openFullscreen]);

  const handleToggleFullscreen = useCallback(() => {
    if (!containerRef.current) return;

    if (isFullscreen) {
      document.exitFullscreen().then(closeFullscreen);
    } else {
      containerRef.current.requestFullscreen().then(openFullscreen);
    }
  }, [closeFullscreen, isFullscreen, openFullscreen]);

  useEffect(() => {
    if (!IS_REQUEST_FULLSCREEN_SUPPORTED) return undefined;
    const container = containerRef.current;
    if (!container) return undefined;

    container.addEventListener('fullscreenchange', toggleFullscreen);

    return () => {
      container.removeEventListener('fullscreenchange', toggleFullscreen);
    };
  }, [toggleFullscreen]);

  const handleClose = useCallback(() => {
    toggleGroupCallPanel();
    if (isFullscreen) {
      closeFullscreen();
    }
  }, [closeFullscreen, isFullscreen, toggleGroupCallPanel]);

  const isDiscarded = phoneCall?.state === 'discarded';
  const isBusy = phoneCall?.reason === 'busy';

  const isIncomingRequested = phoneCall?.state === 'requested' && !isOutgoing;
  const isOutgoingRequested = (phoneCall?.state === 'requested' || phoneCall?.state === 'waiting') && isOutgoing;
  const isActive = phoneCall?.state === 'active';
  const isConnected = phoneCall?.isConnected;

  const [isHangingUp, startHangingUp, stopHangingUp] = useFlag();
  const handleHangUp = useCallback(() => {
    startHangingUp();
    hangUp();
  }, [hangUp, startHangingUp]);

  useEffect(() => {
    if (isHangingUp) {
      playGroupCallSound({ sound: 'end' });
    } else if (isIncomingRequested) {
      playGroupCallSound({ sound: 'incoming' });
    } else if (isBusy) {
      playGroupCallSound({ sound: 'busy' });
    } else if (isDiscarded) {
      playGroupCallSound({ sound: 'end' });
    } else if (isOutgoingRequested) {
      playGroupCallSound({ sound: 'ringing' });
    } else if (isConnected) {
      playGroupCallSound({ sound: 'connect' });
    }
  }, [isBusy, isDiscarded, isIncomingRequested, isOutgoingRequested, isConnected, playGroupCallSound, isHangingUp]);

  useEffect(() => {
    if (phoneCall?.id) {
      stopHangingUp();
    } else {
      connectToActivePhoneCall();
    }
  }, [connectToActivePhoneCall, phoneCall?.id, stopHangingUp]);

  const forceUpdate = useForceUpdate();

  useInterval(forceUpdate, isConnected ? 1000 : undefined);

  const callStatus = useMemo(() => {
    const state = phoneCall?.state;
    if (isHangingUp) {
      return lang('CallStatusHanging');
    }
    if (isBusy) return 'busy';
    if (state === 'requesting') {
      return lang('CallStatusRequesting');
    } else if (state === 'requested') {
      return isOutgoing ? lang('CallStatusRinging') : lang('CallStatusIncoming');
    } else if (state === 'waiting') {
      return lang('CallStatusWaiting');
    } else if (state === 'active' && isConnected) {
      return undefined;
    } else {
      return lang('CallStatusExchanging');
    }
  }, [isBusy, isConnected, isHangingUp, isOutgoing, lang, phoneCall?.state]);

  const hasVideo = phoneCall?.videoState === 'active';
  const hasPresentation = phoneCall?.screencastState === 'active';

  // Stage 2 — phoneCall.mediaState is the single source of truth for UI buttons.
  // The underlying MediaStream is still read via getStreams() for the <video>
  // tags (it doesn't live in global state), but button state is driven by
  // the reactive global.phoneCall so toggles render immediately.
  const streams = getStreams();
  const hasOwnAudio = phoneCall ? phoneCall.isMuted === false : false;
  const hasOwnPresentation = phoneCall?.screencastState === 'active';
  const hasOwnVideo = phoneCall?.videoState === 'active';

  const peerIsMuted = Boolean(phoneCall?.peerIsMuted);
  const peerIsScreenSharing = Boolean(phoneCall?.peerIsScreenSharing);

  const [isHidingPresentation, startHidingPresentation, stopHidingPresentation] = useFlag();
  const [isHidingVideo, startHidingVideo, stopHidingVideo] = useFlag();

  const handleTogglePresentation = useCallback(() => {
    if (hasOwnPresentation) {
      startHidingPresentation();
    }
    if (hasOwnVideo) {
      startHidingVideo();
    }
    setTimeout(async () => {
      await toggleStreamP2p('presentation');
      stopHidingPresentation();
      stopHidingVideo();
      // REST sync happens in the state-driven useEffect below — that also
      // covers the case where the browser's "Stop sharing" bar ended the
      // track without the user touching the in-app button.
    }, 250);
  }, [
    hasOwnPresentation, hasOwnVideo,
    startHidingPresentation, startHidingVideo, stopHidingPresentation, stopHidingVideo,
  ]);

  const handleToggleVideo = useCallback(() => {
    if (hasOwnVideo) {
      startHidingVideo();
    }
    if (hasOwnPresentation) {
      startHidingPresentation();
    }
    setTimeout(async () => {
      await toggleStreamP2p('video');
      stopHidingPresentation();
      stopHidingVideo();
    }, 250);
  }, [
    hasOwnPresentation, hasOwnVideo, startHidingPresentation, startHidingVideo, stopHidingPresentation, stopHidingVideo,
  ]);

  const handleToggleAudio = useCallback(() => {
    // Purely local toggle — the effect below syncs phoneCall.isMuted with the
    // backend (via PUT /calls/:id/mute) so the peer gets a WS event regardless
    // of whether the WebRTC connection is fully up yet (fixes pre-connected
    // mute rot).
    void toggleStreamP2p('audio');
  }, []);

  const [isEmojiOpen, openEmoji, closeEmoji] = useFlag();

  const [isFlipping, startFlipping, stopFlipping] = useFlag();

  const handleFlipCamera = useCallback(() => {
    startFlipping();
    switchCameraInputP2p();
    setTimeout(stopFlipping, 250);
  }, [startFlipping, stopFlipping]);

  const timeElapsed = phoneCall?.startDate && (getServerTime() - phoneCall.startDate);

  useEffect(() => {
    if (phoneCall?.state === 'discarded') {
      setTimeout(hangUp, 250);
    }
  }, [hangUp, phoneCall?.reason, phoneCall?.state]);

  // REST sync effects — mirror phoneCall.isMuted / .screencastState to the
  // backend. Using the global state (rather than calling REST from click
  // handlers) guarantees:
  //  • sync happens even when track.onended was triggered by the browser's
  //    "Stop sharing" bar (no click handler runs in that path)
  //  • sync happens when handleToggleVideo implicitly stops screen share
  //  • sync happens in pre-connected call state (WebRTC still handshaking)
  //
  // Skip initial mount so we don't REST-broadcast the default state on open.
  const didMountRef = useRef(false);
  const lastSyncedMutedRef = useRef<boolean | undefined>(phoneCall?.isMuted);
  const lastSyncedSharingRef = useRef<boolean | undefined>(phoneCall?.screencastState === 'active');

  useEffect(() => {
    if (!didMountRef.current) {
      didMountRef.current = true;
      lastSyncedMutedRef.current = phoneCall?.isMuted;
      lastSyncedSharingRef.current = phoneCall?.screencastState === 'active';
      return;
    }
    if (!phoneCall?.id || phoneCall.state === 'discarded') return;

    const currentMuted = Boolean(phoneCall.isMuted);
    if (currentMuted !== lastSyncedMutedRef.current) {
      lastSyncedMutedRef.current = currentMuted;
      void apiToggleCallMute({ callId: phoneCall.id, muted: currentMuted });
    }

    const currentSharing = phoneCall.screencastState === 'active';
    if (currentSharing !== lastSyncedSharingRef.current) {
      lastSyncedSharingRef.current = currentSharing;
      void (currentSharing
        ? startScreenShareApi({ callId: phoneCall.id })
        : stopScreenShareApi({ callId: phoneCall.id }));
    }
  }, [phoneCall?.id, phoneCall?.state, phoneCall?.isMuted, phoneCall?.screencastState]);

  return (
    <Modal
      isOpen={phoneCall && phoneCall?.state !== 'discarded' && !isCallPanelVisible}
      onClose={handleClose}
      className={buildClassName(
        styles.root,
        isMobile && styles.singleColumn,
      )}
      dialogRef={containerRef}
    >
      <Avatar
        peer={user}
        size="jumbo"
        className={hasVideo || hasPresentation ? styles.blurred : ''}
      />
      {phoneCall?.screencastState === 'active' && streams?.presentation
        && <video className={styles.mainVideo} muted autoPlay playsInline srcObject={streams.presentation} />}
      {phoneCall?.videoState === 'active' && streams?.video
        && <video className={styles.mainVideo} muted autoPlay playsInline srcObject={streams.video} />}
      <video
        className={buildClassName(
          styles.secondVideo,
          !isHidingPresentation && hasOwnPresentation && styles.visible,
          isFullscreen && styles.fullscreen,
        )}
        muted
        autoPlay
        playsInline
        srcObject={streams?.ownPresentation}
      />
      <video
        className={buildClassName(
          styles.secondVideo,
          !isHidingVideo && hasOwnVideo && styles.visible,
          isFullscreen && styles.fullscreen,
        )}
        muted
        autoPlay
        playsInline
        srcObject={streams?.ownVideo}
      />
      <div className={styles.header}>
        {IS_REQUEST_FULLSCREEN_SUPPORTED && (
          <Button
            round
            size="smaller"
            color="translucent"
            iconName={isFullscreen ? 'smallscreen' : 'fullscreen'}
            onClick={handleToggleFullscreen}
            ariaLabel={lang(isFullscreen ? 'AccExitFullscreen' : 'AccSwitchToFullscreen')}
          />
        )}

        <Button
          round
          size="smaller"
          color="translucent"
          iconName="close"
          onClick={handleClose}
          className={styles.closeButton}
        />
      </div>
      <div
        className={buildClassName(styles.emojisBackdrop, isEmojiOpen && styles.open)}
        onClick={!isEmojiOpen ? openEmoji : closeEmoji}
      >
        <div className={buildClassName(styles.emojis, isEmojiOpen && styles.open)}>
          {phoneCall?.isConnected && phoneCall?.emojis && renderText(phoneCall.emojis, ['emoji'])}
        </div>
        <div className={buildClassName(styles.emojiTooltip, isEmojiOpen && styles.open)}>
          {lang('CallEmojiKeyTooltip', user?.firstName).replace('%%', '%')}
        </div>
      </div>
      <div className={styles.userInfo}>
        <h1>{user?.firstName}</h1>
        <span className={styles.status}>{callStatus || formatMediaDuration(timeElapsed || 0)}</span>
      </div>
      {isActive && (peerIsMuted || peerIsScreenSharing) && (
        <div className={styles.peerIndicators}>
          {peerIsMuted && (
            <div className={styles.peerBadge}>
              <i className="icon icon-microphone-alt" aria-hidden />
              <span>{lang('FilterMuted')}</span>
            </div>
          )}
          {peerIsScreenSharing && (
            <div className={styles.peerBadge}>
              <i className="icon icon-share-screen" aria-hidden />
              <span>{lang('CallScreencast')}</span>
            </div>
          )}
        </div>
      )}
      <div className={styles.buttons}>
        <PhoneCallButton
          onClick={handleToggleAudio}
          icon="microphone"
          isDisabled={!isActive}
          isActive={hasOwnAudio}
          label={lang(hasOwnAudio ? 'CallMuteAudio' : 'CallUnmuteAudio')}
        />
        <PhoneCallButton
          onClick={handleToggleVideo}
          icon="video"
          isDisabled={!isActive}
          isActive={hasOwnVideo}
          label={lang(hasOwnVideo ? 'CallStopVideo' : 'CallStartVideo')}
        />
        {hasOwnVideo && (IS_ANDROID || IS_IOS) && (
          <PhoneCallButton
            onClick={handleFlipCamera}
            customIcon={(
              <AnimatedIcon
                tgsUrl={LOCAL_TGS_URLS.CameraFlip}
                playSegment={!isFlipping ? [0, 1] : [0, 10]}
                size={32}
              />
            )}
            isDisabled={!isActive}
            label={lang('VoipFlip')}
          />
        )}
        {IS_SCREENSHARE_SUPPORTED && (
          <PhoneCallButton
            onClick={handleTogglePresentation}
            icon="share-screen"
            isDisabled={!isActive}
            isActive={hasOwnPresentation}
            label={lang('CallScreencast')}
          />
        )}
        {isIncomingRequested && (
          <PhoneCallButton
            onClick={requestMasterAndAcceptCall}
            icon="phone-discard"
            isDisabled={isDiscarded}
            label={lang('CallAccept')}
            className={styles.accept}
            iconClassName={styles.acceptIcon}
          />
        )}
        <PhoneCallButton
          onClick={handleHangUp}
          icon="phone-discard"
          isDisabled={isDiscarded}
          label={lang(isIncomingRequested ? 'CallDecline' : 'CallEndCall')}
          className={styles.leave}
        />
      </div>
    </Modal>
  );
};

export default memo(withGlobal(
  (global): Complete<StateProps> => {
    const { phoneCall, currentUserId } = global;
    const { isCallPanelVisible, isMasterTab } = selectTabState(global);
    const user = selectPhoneCallUser(global);

    return {
      isCallPanelVisible: Boolean(isCallPanelVisible),
      user,
      isOutgoing: phoneCall?.adminId === currentUserId,
      phoneCall: isMasterTab ? phoneCall : undefined,
    };
  },
)(PhoneCall));
