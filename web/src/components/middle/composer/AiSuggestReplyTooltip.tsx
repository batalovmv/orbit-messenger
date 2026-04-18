import { memo, useRef } from '../../../lib/teact/teact';

import buildClassName from '../../../util/buildClassName';
import useShowTransitionDeprecated from '../../../hooks/useShowTransitionDeprecated';

import Icon from '../../common/icons/Icon';
import Spinner from '../../ui/Spinner';

import styles from './AiSuggestReplyTooltip.module.scss';

type OwnProps = {
  isOpen: boolean;
  isLoading: boolean;
  suggestions?: string[];
  error?: string;
  onPick: (text: string) => void;
};

/**
 * The floating panel that slides in above the Composer when the user
 * triggers Orbit AI's "Suggest reply". Positioned via the shared
 * .composer-tooltip class (see components/common/Composer.scss) so it
 * visually matches MentionTooltip / ChatCommandTooltip / InlineBotTooltip —
 * same slide-up animation, same white pill, same drop shadow.
 *
 * All state (open/loading/suggestions/error) is owned by
 * useAiSuggestReply, called once at the Composer level.
 */
const AiSuggestReplyTooltip = ({
  isOpen, isLoading, suggestions, error, onPick,
}: OwnProps) => {
  const containerRef = useRef<HTMLDivElement>();
  const { shouldRender, transitionClassNames } = useShowTransitionDeprecated(
    isOpen, undefined, undefined, false,
  );

  if (!shouldRender) return undefined;

  const className = buildClassName(
    styles.root,
    'composer-tooltip',
    transitionClassNames,
  );

  return (
    <div className={className} ref={containerRef}>
      {isLoading && (
        <div className={styles.status}>
          <Spinner color="gray" />
          <span className={styles.statusLabel}>Генерация...</span>
        </div>
      )}

      {error && !isLoading && (
        <div className={buildClassName(styles.status, styles.errorStatus)}>
          <Icon name="warning" />
          <span className={styles.statusLabel}>{error}</span>
        </div>
      )}

      {!isLoading && !error && suggestions && suggestions.length > 0 && (
        <div className={styles.chips}>
          {suggestions.map((text) => (
            <button
              key={text}
              type="button"
              className={styles.chip}
              onClick={() => onPick(text)}
              title={text}
            >
              {text}
            </button>
          ))}
        </div>
      )}
    </div>
  );
};

export default memo(AiSuggestReplyTooltip);
