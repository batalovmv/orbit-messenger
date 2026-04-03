import type { FC } from '../../../../lib/teact/teact';
import { memo } from '../../../../lib/teact/teact';

import type { ApiAvailableReaction, ApiReaction } from '../../../../api/types';

import buildClassName from '../../../../util/buildClassName';
import { REM } from '../../../common/helpers/mediaDimensions';

import useFlag from '../../../../hooks/useFlag';
import useMedia from '../../../../hooks/useMedia';

import AnimatedSticker from '../../../common/AnimatedSticker';
import Icon from '../../../common/icons/Icon';
import ReactionStaticEmoji from '../../../common/reactions/ReactionStaticEmoji';

import styles from './ReactionSelectorReaction.module.scss';

const REACTION_SIZE = 2 * REM;

type OwnProps = {
  reaction: ApiAvailableReaction;
  isReady?: boolean;
  chosen?: boolean;
  noAppearAnimation?: boolean;
  isLocked?: boolean;
  style?: string;
  onToggleReaction: (reaction: ApiReaction) => void;
};

const ReactionSelectorReaction: FC<OwnProps> = ({
  reaction,
  isReady,
  noAppearAnimation,
  chosen,
  isLocked,
  style,
  onToggleReaction,
}) => {
  const hasAnimatedAppear = Boolean(reaction.appearAnimation?.id);
  const hasAnimatedSelect = Boolean(reaction.selectAnimation?.id);
  const shouldUseStaticIcon = noAppearAnimation || !hasAnimatedAppear || !hasAnimatedSelect;
  const mediaAppearData = useMedia(`sticker${reaction.appearAnimation?.id}`, !isReady || shouldUseStaticIcon);
  const mediaData = useMedia(`document${reaction.selectAnimation?.id}`, !isReady || shouldUseStaticIcon);
  const [isAnimationLoaded, markAnimationLoaded] = useFlag();

  const [isFirstPlay, , unmarkIsFirstPlay] = useFlag(true);
  const [isActivated, activate, deactivate] = useFlag();

  function handleClick() {
    onToggleReaction(reaction.reaction);
  }

  return (
    <div
      className={buildClassName(styles.root, chosen && styles.chosen)}
      onClick={handleClick}
      onMouseEnter={isReady && !isFirstPlay ? activate : undefined}
      style={style}
    >
      {shouldUseStaticIcon && (
        <ReactionStaticEmoji
          className={styles.staticIcon}
          reaction={reaction.reaction}
          availableReaction={reaction}
          size={REACTION_SIZE}
        />
      )}
      {!isAnimationLoaded && !shouldUseStaticIcon && (
        <AnimatedSticker
          key={reaction.appearAnimation?.id}
          tgsUrl={mediaAppearData}
          play={isFirstPlay}
          noLoop
          size={REACTION_SIZE}
          onEnded={unmarkIsFirstPlay}
          forceAlways
        />
      )}
      {!isFirstPlay && !shouldUseStaticIcon && (
        <AnimatedSticker
          key={reaction.selectAnimation?.id}
          tgsUrl={mediaData}
          play={isActivated}
          noLoop
          size={REACTION_SIZE}
          onLoad={markAnimationLoaded}
          onEnded={deactivate}
          forceAlways
        />
      )}
      {isLocked && (
        <Icon className={styles.lock} name="lock-badge" />
      )}
    </div>
  );
};

export default memo(ReactionSelectorReaction);
