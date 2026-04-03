import type React from '../../../../lib/teact/teact';
import {
  memo, useEffect, useRef, useState,
} from '../../../../lib/teact/teact';
import { getActions } from '../../../../global';

import type {
  ApiPeer, ApiReaction, ApiReactionCount,
} from '../../../../api/types';
import type { ObserveFn } from '../../../../hooks/useIntersectionObserver';

import { isReactionChosen } from '../../../../global/helpers';
import buildClassName from '../../../../util/buildClassName';
import { formatIntegerCompact } from '../../../../util/textFormat';
import { REM } from '../../../common/helpers/mediaDimensions';

import useContextMenuHandlers from '../../../../hooks/useContextMenuHandlers';
import useEffectWithPrevDeps from '../../../../hooks/useEffectWithPrevDeps';
import useLang from '../../../../hooks/useLang';
import useLastCallback from '../../../../hooks/useLastCallback';
import usePrevious from '../../../../hooks/usePrevious';
import useShowTransition from '../../../../hooks/useShowTransition';

import AnimatedCounter from '../../../common/AnimatedCounter';
import AvatarList from '../../../common/AvatarList';
import PaidReactionEmoji from '../../../common/reactions/PaidReactionEmoji';
import ReactionAnimatedEmoji from '../../../common/reactions/ReactionAnimatedEmoji';
import Sparkles from '../../../common/Sparkles';
import Button from '../../../ui/Button';

import styles from './ReactionButton.module.scss';

const REACTION_SIZE = 1.25 * REM;

type OwnProps = {
  chatId: string;
  messageId: number;
  reaction: ApiReactionCount;
  containerId: string;
  isOwnMessage?: boolean;
  recentReactors?: ApiPeer[];
  className?: string;
  chosenClassName?: string;
  isOutside?: boolean;
  observeIntersection?: ObserveFn;
  onClick?: (reaction: ApiReaction) => void;
  onPaidClick?: (count: number) => void;
};

const ReactionButton = ({
  reaction,
  containerId,
  isOwnMessage,
  recentReactors,
  className,
  chosenClassName,
  chatId,
  messageId,
  isOutside,
  observeIntersection,
  onClick,
  onPaidClick,
}: OwnProps) => {
  const { requestWave } = getActions();
  const ref = useRef<HTMLButtonElement>();
  const counterRef = useRef<HTMLSpanElement>();

  const [isBouncing, setIsBouncing] = useState(false);
  const [isCountBumping, setIsCountBumping] = useState(false);

  const lang = useLang();

  const isPaid = reaction.reaction.type === 'paid';

  const handlePaidClick = useLastCallback((count = 1) => {
    onPaidClick?.(count);
  });

  const handleClick = useLastCallback((e: React.MouseEvent<HTMLButtonElement, MouseEvent>) => {
    if (reaction.reaction.type === 'paid') {
      e.stopPropagation(); // Prevent default message double click behavior
      handlePaidClick();

      return;
    }

    onClick?.(reaction.reaction);
  });

  const {
    isContextMenuOpen,
    handleBeforeContextMenu,
    handleContextMenu,
    handleContextMenuClose,
    handleContextMenuHide,
  } = useContextMenuHandlers(ref, reaction.reaction.type !== 'paid', undefined, undefined, undefined, true);

  useEffect(() => {
    if (isContextMenuOpen) {
      handleContextMenuClose();
      handleContextMenuHide();
    }
  }, [handleContextMenuClose, handleContextMenuHide, isContextMenuOpen]);

  useEffectWithPrevDeps(([prevReaction]) => {
    const amount = reaction.localAmount;
    const button = ref.current;
    if (!amount || !button || amount === prevReaction?.localAmount) return;

    if (reaction.localAmount) {
      const { left, top } = button.getBoundingClientRect();
      const startX = left + button.offsetWidth / 2;
      const startY = top + button.offsetHeight / 2;
      requestWave({ startX, startY });
    }

    setIsBouncing(true);
    setIsCountBumping(true);
  }, [reaction, chatId, messageId]);

  useEffect(() => {
    if (!isBouncing) return undefined;
    const timer = setTimeout(() => {
      setIsBouncing(false);
      setIsCountBumping(false);
    }, 400);
    return () => clearTimeout(timer);
  }, [isBouncing]);

  const prevAmount = usePrevious(reaction.localAmount);

  const {
    shouldRender: shouldRenderPaidCounter,
  } = useShowTransition({
    isOpen: Boolean(reaction.localAmount),
    ref: counterRef,
    className: 'slow',
    withShouldRender: true,
  });

  return (
    <Button
      className={buildClassName(
        styles.root,
        styles.popIn,
        isOwnMessage && styles.own,
        isPaid && styles.paid,
        isOutside && styles.outside,
        isReactionChosen(reaction) && styles.chosen,
        isReactionChosen(reaction) && chosenClassName,
        isBouncing && styles.bounce,
        className,
      )}
      size="tiny"
      ref={ref}
      onMouseDown={handleBeforeContextMenu}
      onContextMenu={handleContextMenu}
      onClick={handleClick}
    >
      {reaction.reaction.type === 'paid' ? (
        <>
          <Sparkles preset="button" />
          <PaidReactionEmoji
            className={styles.animatedEmoji}
            containerId={containerId}
            reaction={reaction.reaction}
            size={REACTION_SIZE}
            localAmount={reaction.localAmount}
            observeIntersection={observeIntersection}
          />
          {shouldRenderPaidCounter && (
            <AnimatedCounter
              ref={counterRef}
              text={`+${formatIntegerCompact(lang, reaction.localAmount || prevAmount!)}`}
              className={styles.paidCounter}
            />
          )}
        </>
      ) : (
        <ReactionAnimatedEmoji
          className={styles.animatedEmoji}
          containerId={containerId}
          reaction={reaction.reaction}
          size={REACTION_SIZE}
          observeIntersection={observeIntersection}
        />
      )}
      {recentReactors?.length ? (
        <AvatarList size="mini" peers={recentReactors} />
      ) : (
        <AnimatedCounter
          text={formatIntegerCompact(lang, reaction.count + (reaction.localAmount || 0))}
          className={buildClassName(styles.counter, isCountBumping && styles.countBump)}
        />
      )}
    </Button>
  );
};

export default memo(ReactionButton);
