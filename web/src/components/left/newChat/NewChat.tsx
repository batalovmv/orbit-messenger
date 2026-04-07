import type { FC } from '@teact';
import { memo, useCallback, useState } from '@teact';
import { getActions } from '../../../global';

import { type AnimationLevel, LeftColumnContent } from '../../../types';

import { resolveTransitionName } from '../../../util/resolveTransitionName';

import useLastCallback from '../../../hooks/useLastCallback';

import Transition from '../../ui/Transition';
import NewChatStep1 from './NewChatStep1';
import NewChatStep2 from './NewChatStep2';

import './NewChat.scss';

export type OwnProps = {
  isActive: boolean;
  content: LeftColumnContent;
  animationLevel: AnimationLevel;
  onReset: () => void;
};

const RENDER_COUNT = Object.keys(LeftColumnContent).length / 2;

const NewChat: FC<OwnProps> = ({
  isActive,
  content,
  animationLevel,
  onReset,
}) => {
  const { openLeftColumnContent, setGlobalSearchQuery } = getActions();
  const [newChatMemberIds, setNewChatMemberIds] = useState<string[]>([]);

  const handleNextStep = useCallback(() => {
    openLeftColumnContent({
      contentKey: LeftColumnContent.NewGroupStep2,
    });
  }, []);

  const changeSelectedMemberIdsHandler = useLastCallback((ids: string[]) => {
    const isSelection = ids.length > newChatMemberIds.length;

    setNewChatMemberIds(ids);
    if (isSelection) {
      setGlobalSearchQuery({ query: '' });
    }
  });

  return (
    <Transition
      id="NewChat"
      name={resolveTransitionName('layers', animationLevel)}
      renderCount={RENDER_COUNT}
      activeKey={content}
    >
      {(isStepActive) => {
        switch (content) {
          case LeftColumnContent.NewGroupStep1:
            return (
              <NewChatStep1
                isActive={isActive}
                selectedMemberIds={newChatMemberIds}
                onSelectedMemberIdsChange={changeSelectedMemberIdsHandler}
                onNextStep={handleNextStep}
                onReset={onReset}
              />
            );
          case LeftColumnContent.NewGroupStep2:
            return (
              <NewChatStep2
                isActive={isStepActive && isActive}
                memberIds={newChatMemberIds}
                onReset={onReset}
              />
            );
          default:
            return undefined;
        }
      }}
    </Transition>
  );
};

export default memo(NewChat);
