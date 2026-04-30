import type { FC } from '../../../lib/teact/teact';
import { memo, useCallback, useMemo } from '../../../lib/teact/teact';
import { withGlobal } from '../../../global';

import type { ApiChatMember, ApiUser, ApiUserStatus } from '../../../api/types';
import { ManagementScreens } from '../../../types';

import { sortUserIds } from '../../../global/helpers';
import { selectChatFullInfo } from '../../../global/selectors';

import useHistoryBack from '../../../hooks/useHistoryBack';

import NothingFound from '../../common/NothingFound';
import PrivateChatInfo from '../../common/PrivateChatInfo';
import ListItem from '../../ui/ListItem';

type OwnProps = {
  chatId: string;
  onScreenSelect: (screen: ManagementScreens) => void;
  onChatMemberSelect: (memberId: string) => void;
  onClose: NoneToVoidFunction;
  isActive: boolean;
};

type StateProps = {
  usersById: Record<string, ApiUser>;
  userStatusesById: Record<string, ApiUserStatus>;
  members?: ApiChatMember[];
};

const ManageGroupUserPermissionsCreate: FC<OwnProps & StateProps> = ({
  usersById,
  userStatusesById,
  members,
  onScreenSelect,
  onChatMemberSelect,
  onClose,
  isActive,
}) => {
  useHistoryBack({
    isActive,
    onBack: onClose,
  });

  const memberIds = useMemo(() => {
    if (!members || !usersById) {
      return undefined;
    }

    return sortUserIds(
      members.filter((member) => !member.isOwner).map(({ userId }) => userId),
      usersById,
      userStatusesById,
    );
  }, [members, usersById, userStatusesById]);

  const handleExceptionMemberClick = useCallback((memberId: string) => {
    onChatMemberSelect(memberId);
    onScreenSelect(ManagementScreens.GroupUserPermissions);
  }, [onChatMemberSelect, onScreenSelect]);

  return (
    <div className="Management">
      <div className="custom-scroll">
        <div className="section" teactFastList>
          {memberIds ? (
            memberIds.map((id, i) => (
              <ListItem
                key={id}
                teactOrderKey={i}
                className="chat-item-clickable scroll-item"

                onClick={() => handleExceptionMemberClick(id)}
              >
                <PrivateChatInfo userId={id} forceShowSelf />
              </ListItem>
            ))
          ) : (
            <NothingFound
              teactOrderKey={0}
              key="nothing-found"
              text="No members found"
            />
          )}
        </div>
      </div>
    </div>
  );
};

export default memo(withGlobal<OwnProps>(
  (global, { chatId }): Complete<StateProps> => {
    const { byId: usersById, statusesById: userStatusesById } = global.users;
    const members = selectChatFullInfo(global, chatId)?.members;
    return {
      members,
      usersById,
      userStatusesById,
    };
  },
)(ManageGroupUserPermissionsCreate));
