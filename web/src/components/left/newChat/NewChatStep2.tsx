import type { FC } from '../../../lib/teact/teact';
import type React from '../../../lib/teact/teact';
import {
  memo,
  useCallback, useEffect, useRef, useState,
} from '../../../lib/teact/teact';
import { getActions, getGlobal, withGlobal } from '../../../global';

import { ChatCreationProgress } from '../../../types';

import { getUserFirstOrLastName } from '../../../global/helpers';
import { selectTabState } from '../../../global/selectors';

import useHistoryBack from '../../../hooks/useHistoryBack';
import useOldLang from '../../../hooks/useOldLang';

import PrivateChatInfo from '../../common/PrivateChatInfo';
import AvatarEditable from '../../ui/AvatarEditable';
import Button from '../../ui/Button';
import FloatingActionButton from '../../ui/FloatingActionButton';
import InputText from '../../ui/InputText';
import ListItem from '../../ui/ListItem';

export type OwnProps = {
  isActive: boolean;
  memberIds: string[];
  onReset: (forceReturnToChatList?: boolean) => void;
};

type StateProps = {
  creationProgress?: ChatCreationProgress;
  creationError?: string;
  maxGroupSize?: number;
};

const MAX_MEMBERS_FOR_GENERATE_CHAT_NAME = 4;

const NewChatStep2: FC<OwnProps & StateProps> = ({
  isActive,
  memberIds,
  maxGroupSize,
  creationProgress,
  creationError,
  onReset,
}) => {
  const {
    createGroupChat,
  } = getActions();

  const lang = useOldLang();

  useHistoryBack({
    isActive,
    onBack: onReset,
  });

  const [title, setTitle] = useState('');
  const [photo, setPhoto] = useState<File | undefined>();
  const [error, setError] = useState<string | undefined>();
  const hasManualTitle = useRef(false);

  const chatTitleEmptyError = 'Chat title can\'t be empty';
  const chatTooManyUsersError = 'Sorry, creating supergroups is not yet supported';

  const isLoading = creationProgress === ChatCreationProgress.InProgress;

  useEffect(() => {
    if (hasManualTitle.current) {
      return;
    }
    if (!memberIds.length || memberIds.length > MAX_MEMBERS_FOR_GENERATE_CHAT_NAME) {
      setTitle('');
      return;
    }
    const global = getGlobal();
    const usersById = global.users.byId;
    const memberFirstNames = [global.currentUserId!, ...memberIds]
      .map((userId) => getUserFirstOrLastName(usersById[userId]))
      .filter(Boolean);
    const delimiter = lang('CreateGroupPeersTitleLastDelimeter');
    // If lang key isn't loaded yet, fall back to " and "
    const lastDelimiter = delimiter === 'CreateGroupPeersTitleLastDelimeter' ? ' and ' : delimiter;
    const generatedChatName = memberFirstNames.slice(0, -1).join(', ')
      + lastDelimiter
      + memberFirstNames[memberFirstNames.length - 1];
    setTitle(generatedChatName);
  }, [memberIds, lang]);

  const handleTitleChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const { value } = e.currentTarget;
    const newValue = value.replace(/^\s+/, '');

    hasManualTitle.current = true;
    setTitle(newValue);

    if (newValue !== value) {
      e.currentTarget.value = newValue;
    }
  }, []);

  const handleCreateGroup = useCallback(() => {
    if (!title.length) {
      setError(chatTitleEmptyError);
      return;
    }

    if (maxGroupSize && memberIds.length >= maxGroupSize) {
      setError(chatTooManyUsersError);
      return;
    }

    createGroupChat({
      title,
      photo,
      memberIds,
    });
  }, [title, memberIds, maxGroupSize, createGroupChat, photo]);

  useEffect(() => {
    if (creationProgress === ChatCreationProgress.Complete) {
      onReset(true);
    }
  }, [creationProgress, onReset]);

  const renderedError = (creationError && lang(creationError)) || (
    error !== chatTitleEmptyError
      ? error
      : undefined
  );

  return (
    <div className="NewChat">
      <div className="left-header">
        <Button
          round
          size="smaller"
          color="translucent"
          onClick={() => onReset()}
          ariaLabel="Return to member selection"
          iconName="arrow-left"
        />
        <h3>{lang('NewGroup')}</h3>
      </div>
      <div className="NewChat-inner step-2">
        <AvatarEditable
          onChange={setPhoto}
          title={lang('AddPhoto')}
        />
        <InputText
          value={title}
          onChange={handleTitleChange}
          label={lang('GroupName')}
          error={error === chatTitleEmptyError ? error : undefined}
        />

        {renderedError && (
          <p className="error">{renderedError}</p>
        )}

        {memberIds.length > 0 && (
          <>
            <h3 className="chat-members-heading">{lang('GroupInfoParticipantCount', memberIds.length, 'i')}</h3>

            <div className="chat-members-list custom-scroll">
              {memberIds.map((id) => (
                <ListItem inactive className="chat-item-clickable">
                  <PrivateChatInfo userId={id} />
                </ListItem>
              ))}
            </div>
          </>
        )}
      </div>

      <FloatingActionButton
        isShown={title.length !== 0}
        onClick={handleCreateGroup}
        disabled={isLoading}
        ariaLabel="Create Group"
        iconName="arrow-right"
        isLoading={isLoading}
      />
    </div>
  );
};

export default memo(withGlobal<OwnProps>(
  (global): Complete<StateProps> => {
    const {
      progress: creationProgress,
      error: creationError,
    } = selectTabState(global).chatCreation || {};

    return {
      creationProgress,
      creationError,
      maxGroupSize: global.config?.maxGroupSize,
    };
  },
)(NewChatStep2));
