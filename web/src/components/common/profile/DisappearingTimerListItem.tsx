import type { TeactNode } from '../../../lib/teact/teact';
import {
  memo, useRef, useState,
} from '../../../lib/teact/teact';
import { getActions } from '../../../global';

import type { ApiChat } from '../../../api/types';

import useFlag from '../../../hooks/useFlag';
import useLastCallback from '../../../hooks/useLastCallback';

import ListItem from '../../ui/ListItem';
import Menu from '../../ui/Menu';
import MenuItem from '../../ui/MenuItem';

type OwnProps = {
  chat: ApiChat;
};

type TimerOption = {
  seconds: number;
  label: string;
};

const TIMER_OPTIONS: TimerOption[] = [
  { seconds: 0, label: 'Выключено' },
  { seconds: 24 * 60 * 60, label: '24 часа' },
  { seconds: 7 * 24 * 60 * 60, label: '7 дней' },
  { seconds: 30 * 24 * 60 * 60, label: '30 дней' },
];

function currentLabel(seconds: number | undefined): string {
  if (!seconds || seconds <= 0) return 'Выключено';
  const match = TIMER_OPTIONS.find((o) => o.seconds === seconds);
  if (match) return match.label;
  if (seconds < 60 * 60) return `${Math.round(seconds / 60)} мин`;
  if (seconds < 24 * 60 * 60) return `${Math.round(seconds / 3600)} ч`;
  return `${Math.round(seconds / 86400)} дн`;
}

// In-profile entry for the per-chat disappearing-messages timer
// (design doc §9). Shown only for E2E chats — plaintext chats keep
// their messages forever by default.
//
// Writes go directly to the Saturn `setDisappearingTimer` method via
// a dynamic import so the chunk only lands when a user opens an E2E
// profile. The local chat is optimistically patched through the
// `apiUpdate → updateChat` reducer path.
const DisappearingTimerListItem = ({ chat }: OwnProps) => {
  const { apiUpdate, showNotification } = getActions();
  const [isMenuOpen, openMenu, closeMenu] = useFlag(false);
  const [pendingSeconds, setPendingSeconds] = useState<number | undefined>(undefined);
  const containerRef = useRef<HTMLDivElement>();

  const currentSeconds = chat.disappearingTimer;
  const displaySeconds = pendingSeconds ?? currentSeconds ?? 0;

  const handleSelect = useLastCallback(async (seconds: number) => {
    closeMenu();
    if (seconds === currentSeconds || (seconds === 0 && !currentSeconds)) return;

    setPendingSeconds(seconds);
    try {
      const { setDisappearingTimer } = await import('../../../api/saturn/methods/keys');
      await setDisappearingTimer({ chatId: chat.id, seconds });
      apiUpdate({
        '@type': 'updateChat',
        id: chat.id,
        chat: { disappearingTimer: seconds > 0 ? seconds : undefined },
      });
    } catch (err) {
      showNotification({
        message: err instanceof Error && err.message
          ? `Не удалось обновить таймер: ${err.message}`
          : 'Не удалось обновить таймер',
      });
    } finally {
      setPendingSeconds(undefined);
    }
  });

  const subtitle: TeactNode = displaySeconds > 0
    ? `Сообщения удаляются через ${currentLabel(displaySeconds).toLowerCase()}`
    : 'Сообщения не удаляются автоматически';

  return (
    <div ref={containerRef} style="position: relative;">
      <ListItem
        icon="timer"
        narrow
        ripple
        onClick={openMenu}
      >
        <span className="title">Исчезающие сообщения</span>
        <span className="subtitle">{subtitle}</span>
      </ListItem>
      <Menu
        isOpen={isMenuOpen}
        positionX="right"
        positionY="top"
        onClose={closeMenu}
      >
        {TIMER_OPTIONS.map((option) => (
          <MenuItem
            key={option.seconds}
            onClick={() => handleSelect(option.seconds)}
          >
            {option.label}
            {option.seconds === currentSeconds && ' ✓'}
          </MenuItem>
        ))}
      </Menu>
    </div>
  );
};

export default memo(DisappearingTimerListItem);
