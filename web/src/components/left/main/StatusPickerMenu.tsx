import type { ElementRef } from '../../../lib/teact/teact';
import {
  memo, useEffect, useRef,
} from '../../../lib/teact/teact';
import { getActions, withGlobal } from '../../../global';

import { selectIsContextMenuTranslucent, selectUser } from '../../../global/selectors';

import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import Menu from '../../ui/Menu';
import Portal from '../../ui/Portal';

import styles from './StatusPickerMenu.module.scss';

const STATUS_PRESETS = [
  { emoji: '💼', key: 'CustomStatusWorking' },
  { emoji: '🌴', key: 'CustomStatusDayOff' },
  { emoji: '🏖️', key: 'CustomStatusVacation' },
  { emoji: '🤒', key: 'CustomStatusSick' },
  { emoji: '📵', key: 'CustomStatusUnavailable' },
] as const;

export type OwnProps = {
  isOpen: boolean;
  statusButtonRef: ElementRef<HTMLButtonElement>;
  onClose: () => void;
};

interface StateProps {
  isTranslucent?: boolean;
  currentCustomStatus?: string;
}

const StatusPickerMenu = ({
  isOpen,
  statusButtonRef,
  isTranslucent,
  currentCustomStatus,
  onClose,
}: OwnProps & StateProps) => {
  const { setCustomStatus } = getActions();
  const lang = useLang();

  const transformOriginX = useRef<number>(0);
  useEffect(() => {
    transformOriginX.current = statusButtonRef.current!.getBoundingClientRect().right;
  }, [isOpen, statusButtonRef]);

  const handlePresetClick = useLastCallback((emoji: string, text: string) => {
    setCustomStatus({ text, emoji });
    onClose();
  });

  const handleClearStatus = useLastCallback(() => {
    setCustomStatus({ text: '', emoji: '' });
    onClose();
  });

  return (
    <Portal>
      <Menu
        isOpen={isOpen}
        noCompact
        positionX="left"
        bubbleClassName={styles.menuContent}
        onClose={onClose}
        transformOriginX={transformOriginX.current}
      >
        <div className={styles.presets}>
          <div className={styles.presetsHeader}>{lang('CustomStatusPresets')}</div>
          {STATUS_PRESETS.map(({ emoji, key }) => {
            const text = lang(key);
            return (
              <button
                key={key}
                type="button"
                className={styles.presetItem}
                onClick={() => handlePresetClick(emoji, text)}
              >
                <span className={styles.presetEmoji}>{emoji}</span>
                <span className={styles.presetText}>{text}</span>
              </button>
            );
          })}
          {currentCustomStatus && (
            <button
              type="button"
              className={styles.presetItem}
              onClick={handleClearStatus}
            >
              <span className={styles.presetEmoji}>✕</span>
              <span className={styles.presetText}>{lang('CustomStatusClear')}</span>
            </button>
          )}
        </div>
      </Menu>
    </Portal>
  );
};

export default memo(withGlobal<OwnProps>((global): Complete<StateProps> => {
  const { currentUserId } = global;
  const currentUser = currentUserId ? selectUser(global, currentUserId) : undefined;
  return {
    isTranslucent: selectIsContextMenuTranslucent(global),
    currentCustomStatus: currentUser?.customStatus,
  };
})(StatusPickerMenu));
