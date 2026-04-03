import type {
  StatisticsMessageInteractionCounter,
} from '../../../api/types';

import { getCurrentTabId } from '../../../util/establishMultitabRole';
import { callApi } from '../../../api/saturn';
import { addActionHandler, getGlobal, setGlobal } from '../../index';
import {
  updateMessageStatistics,
  updateStatistics,
  updateStatisticsGraph,
} from '../../reducers';
import {
  selectChat,
  selectChatFullInfo,
  selectChatMessages,
  selectTabState,
} from '../../selectors';

addActionHandler('loadStatistics', async (global, actions, payload): Promise<void> => {
  const { chatId, isGroup, tabId = getCurrentTabId() } = payload;
  const chat = selectChat(global, chatId);
  const fullInfo = selectChatFullInfo(global, chatId);
  if (!chat || !fullInfo) {
    return;
  }

  const result = await callApi(
    isGroup ? 'fetchGroupStatistics' : 'fetchChannelStatistics',
    { chat, dcId: fullInfo.statisticsDcId },
  );
  if (!result) {
    return;
  }

  const { stats } = result;
  global = getGlobal();
  global = updateStatistics(global, chatId, stats, tabId);
  setGlobal(global);

  if (stats.type === 'channel') {
    const messageInteractions = stats.recentPosts.filter(
      (post: StatisticsMessageInteractionCounter) => post.type === 'message',
    );

    if (messageInteractions.length > 0) {
      actions.loadMessagesById({
        chatId,
        messageIds: messageInteractions.map(
          (interaction: StatisticsMessageInteractionCounter) => interaction.msgId,
        ),
      });
    }
  }
});

addActionHandler('loadMessageStatistics', async (global, actions, payload): Promise<void> => {
  const { chatId, messageId, tabId = getCurrentTabId() } = payload;
  const chat = selectChat(global, chatId);
  const fullInfo = selectChatFullInfo(global, chatId);
  if (!chat || !fullInfo) {
    return;
  }

  const dcId = fullInfo.statisticsDcId;
  let result = await callApi('fetchMessageStatistics', { chat, messageId, dcId });
  if (!result) {
    result = {};
  }

  global = getGlobal();

  const {
    viewsCount,
    forwardsCount,
    reactions,
  } = selectChatMessages(global, chatId)[messageId] || {};
  result.viewsCount = viewsCount;
  result.forwardsCount = forwardsCount;
  result.reactionsCount = reactions?.results
    ? reactions?.results.reduce((acc, reaction) => acc + reaction.count, 0)
    : undefined;

  global = updateMessageStatistics(global, result, tabId);
  setGlobal(global);

  actions.loadMessagePublicForwards({
    chatId,
    messageId,
    tabId,
  });
});

addActionHandler('loadMessagePublicForwards', async (global, actions, payload): Promise<void> => {
  const { chatId, messageId, tabId = getCurrentTabId() } = payload;
  const chat = selectChat(global, chatId);
  const fullInfo = selectChatFullInfo(global, chatId);
  if (!chat || !fullInfo) {
    return;
  }

  const dcId = fullInfo.statisticsDcId;
  const stats = selectTabState(global, tabId).statistics.currentMessage || {};

  if (stats?.publicForwards && !stats.nextOffset) return;

  const publicForwards = await callApi('fetchMessagePublicForwards', {
    chat, messageId, dcId, offset: stats.nextOffset,
  });
  const {
    forwards,
    nextOffset,
    count,
  } = publicForwards || {};

  global = getGlobal();
  global = updateMessageStatistics(global, {
    ...stats,
    publicForwards: count || forwards?.length,
    publicForwardsData: (stats.publicForwardsData || []).concat((forwards || [])),
    nextOffset,
  }, tabId);
  setGlobal(global);
});

addActionHandler('loadStatisticsAsyncGraph', async (global, actions, payload): Promise<void> => {
  const {
    chatId, token, name, isPercentage, tabId = getCurrentTabId(),
  } = payload;
  const fullInfo = selectChatFullInfo(global, chatId);
  if (!fullInfo) {
    return;
  }

  const dcId = fullInfo.statisticsDcId;
  const result = await callApi('fetchStatisticsAsyncGraph', { token, dcId, isPercentage });

  if (!result) {
    return;
  }

  global = getGlobal();
  global = updateStatisticsGraph(global, chatId, name, result, tabId);
  setGlobal(global);
});
