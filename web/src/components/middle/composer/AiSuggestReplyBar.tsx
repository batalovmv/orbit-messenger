import { memo } from '../../../lib/teact/teact';

import buildClassName from '../../../util/buildClassName';

import useLang from '../../../hooks/useLang';

import Icon from '../../common/icons/Icon';

import styles from './AiSuggestReplyBar.module.scss';

type OwnProps = {
  isOpen: boolean;
  isLoading: boolean;
  onToggle: NoneToVoidFunction;
};

/**
 * Inline trigger button that sits between emoji and attach in the
 * composer's message-input row. Clicking toggles the floating
 * AiSuggestReplyTooltip that renders above the composer — state lives in
 * useAiSuggestReply at the Composer level so this component is purely
 * presentational.
 *
 * Hidden entirely when composer has text (we never want to steal focus
 * from in-progress typing), which the parent handles by not rendering
 * us in that case.
 */
const AiSuggestReplyBar = ({ isOpen, isLoading, onToggle }: OwnProps) => {
  const lang = useLang();

  return (
    <button
      type="button"
      className={buildClassName(
        styles.trigger,
        isOpen && styles.triggerOpen,
        isLoading && styles.triggerLoading,
      )}
      onClick={onToggle}
      title={lang('AiSuggestReply')}
      aria-label={lang('AiSuggestReply')}
    >
      <Icon name="lamp" />
    </button>
  );
};

export default memo(AiSuggestReplyBar);
