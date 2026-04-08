import { memo, useRef } from '../../../lib/teact/teact';
import { getActions, withGlobal } from '../../../global';

import { selectIsCurrentUserFrozen, selectUser } from '../../../global/selectors';

import useAppLayout from '../../../hooks/useAppLayout';
import useFlag from '../../../hooks/useFlag';
import useLastCallback from '../../../hooks/useLastCallback';

import StarIcon from '../../common/icons/StarIcon';
import Button from '../../ui/Button';
import StatusPickerMenu from './StatusPickerMenu.async';

interface StateProps {
  customStatusEmoji?: string;
  isAccountFrozen?: boolean;
}

const StatusButton = ({ customStatusEmoji, isAccountFrozen }: StateProps) => {
  const { openFrozenAccountModal } = getActions();

  const buttonRef = useRef<HTMLButtonElement>();
  const [isStatusPickerOpen, openStatusPicker, closeStatusPicker] = useFlag(false);
  const { isMobile } = useAppLayout();

  const handleClick = useLastCallback(() => {
    if (isAccountFrozen) {
      openFrozenAccountModal();
      return;
    }
    openStatusPicker();
  });

  return (
    <div className="StatusButton extra-spacing">
      <Button
        round
        ref={buttonRef}
        ripple={!isMobile}
        size="smaller"
        color="translucent"
        className="emoji-status"
        onClick={handleClick}
      >
        {customStatusEmoji ? (
          <span style="font-size: 1.25rem; line-height: 1">{customStatusEmoji}</span>
        ) : <StarIcon />}
      </Button>
      <StatusPickerMenu
        statusButtonRef={buttonRef}
        isOpen={isStatusPickerOpen}
        onClose={closeStatusPicker}
      />
    </div>
  );
};

export default memo(withGlobal((global): Complete<StateProps> => {
  const { currentUserId } = global;
  const currentUser = currentUserId ? selectUser(global, currentUserId) : undefined;
  const isAccountFrozen = selectIsCurrentUserFrozen(global);

  return {
    customStatusEmoji: currentUser?.customStatusEmoji,
    isAccountFrozen,
  };
})(StatusButton));
