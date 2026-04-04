import type { FC } from '../../../lib/teact/teact';
import { memo, useMemo } from '../../../lib/teact/teact';

import type { ApiAvailableReaction, ApiReaction } from '../../../api/types';
import type { ObserveFn } from '../../../hooks/useIntersectionObserver';

import { isSameReaction, normalizeReactionEmoticon } from '../../../global/helpers';
import buildClassName from '../../../util/buildClassName';
import { getEmojiImagePath } from '../../../util/emoji/emoji';

import useThumbnail from '../../../hooks/media/useThumbnail';
import useMedia from '../../../hooks/useMedia';
import useMediaTransition from '../../../hooks/useMediaTransition';

import CustomEmoji from '../CustomEmoji';
import Icon from '../icons/Icon';

import './ReactionStaticEmoji.scss';

import blankUrl from '../../../assets/blank.png';

type OwnProps = {
  reaction: ApiReaction;
  availableReaction?: ApiAvailableReaction;
  availableReactions?: ApiAvailableReaction[];
  className?: string;
  size?: number;
  withIconHeart?: boolean;
  observeIntersection?: ObserveFn;
};

const ReactionStaticEmoji: FC<OwnProps> = ({
  reaction,
  availableReaction,
  availableReactions,
  className,
  size,
  withIconHeart,
  observeIntersection,
}) => {
  const resolvedAvailableReaction = useMemo(() => (
    availableReaction || availableReactions?.find((availableItem) => isSameReaction(availableItem.reaction, reaction))
  ), [availableReaction, availableReactions, reaction]);
  const staticIcon = resolvedAvailableReaction?.staticIcon;
  const staticIconId = staticIcon?.id;
  const mediaHash = staticIconId ? `document${staticIconId}` : undefined;
  const cacheBuster = 1;
  const mediaData = useMedia(mediaHash, false, undefined, undefined, cacheBuster);
  const thumbDataUri = useThumbnail(staticIcon?.thumbnail);
  const emojiImagePath = reaction.type === 'emoji' && reaction.emoticon.length <= 16
    ? getEmojiImagePath(reaction.emoticon, size && size > 32 ? 'big' : 'small')
    : undefined;
  const fallbackImagePath = thumbDataUri || emojiImagePath;
  const shouldUseEmojiFallback = reaction.type === 'emoji' && !fallbackImagePath && !mediaData;

  const { ref: thumbRef } = useMediaTransition<HTMLImageElement>({
    hasMediaData: Boolean(thumbDataUri && !mediaData),
  });
  const { ref: mediaRef } = useMediaTransition<HTMLImageElement>({
    hasMediaData: Boolean(mediaData || (!thumbDataUri && emojiImagePath)),
  });

  const shouldApplySizeFix = reaction.type === 'emoji' && reaction.emoticon === '🦄';
  const shouldReplaceWithHeartIcon = withIconHeart
    && reaction.type === 'emoji'
    && normalizeReactionEmoticon(reaction.emoticon) === '❤';

  if (reaction.type === 'custom') {
    return (
      <CustomEmoji
        documentId={reaction.documentId}
        className={buildClassName('ReactionStaticEmoji', className)}
        size={size}
        observeIntersectionForPlaying={observeIntersection}
      />
    );
  }

  if (shouldReplaceWithHeartIcon) {
    return (
      <Icon name="heart" className="ReactionStaticEmoji" style={size ? `font-size: ${size}px; width: ${size}px; height: ${size}px` : undefined} />
    );
  }

  return (
    <div
      className={buildClassName('ReactionStaticEmoji', className)}
      style={size ? `width: ${size}px; height: ${size}px` : undefined}
    >
      {shouldUseEmojiFallback && (
        <span className="emoji-fallback" aria-hidden="true">
          {reaction.emoticon}
        </span>
      )}
      {!shouldUseEmojiFallback && thumbDataUri && !mediaData && (
        <img
          ref={thumbRef}
          className="thumb"
          src={thumbDataUri}
          alt=""
          draggable={false}
        />
      )}
      {!shouldUseEmojiFallback && (mediaData || (!thumbDataUri && fallbackImagePath)) && (
        <>
          <img
            ref={mediaRef}
            className={buildClassName('media', shouldApplySizeFix && 'with-unicorn-fix')}
            src={mediaData || fallbackImagePath || blankUrl}
            alt={resolvedAvailableReaction?.title || (reaction.type === 'emoji' ? reaction.emoticon : '')}
            draggable={false}
          />
        </>
      )}
    </div>
  );
};

export default memo(ReactionStaticEmoji);
