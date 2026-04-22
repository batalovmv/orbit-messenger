
// ---------------------------------------------------------------------------
// Saturn translateText — bridges the TG-original translateMessages action
// (which calls callApi('translateText')) to our cached translation endpoints.
// Emits the same sendApiUpdate events as the GramJS original so the existing
// updater/reducer pipeline stores results in global.translations.
// ---------------------------------------------------------------------------

import type { ApiChat, ApiFormattedText } from '../../types';

import { resolveMessageUuid } from './messages';
import { fetchTranslationsBatch } from './ai';
import { sendApiUpdate } from '../updates/apiUpdateEmitter';

type TranslateTextParams = ({
  text: ApiFormattedText[];
} | {
  chat: ApiChat;
  messageIds: number[];
}) & {
  toLanguageCode: string;
};

export async function translateText(params: TranslateTextParams) {
  const isMessageTranslation = 'chat' in params;

  if (!isMessageTranslation) {
    // Free-text translation not implemented via Saturn yet
    return undefined;
  }

  const { chat, messageIds, toLanguageCode } = params;

  // Resolve numeric message IDs to UUIDs
  const uuids: string[] = [];
  const idToSeq = new Map<string, number>();
  for (const seqNum of messageIds) {
    const uuid = resolveMessageUuid(chat.id, seqNum);
    if (uuid) {
      uuids.push(uuid);
      idToSeq.set(uuid, seqNum);
    }
  }

  if (!uuids.length) {
    sendApiUpdate({
      '@type': 'failedMessageTranslations',
      chatId: chat.id,
      messageIds,
      toLanguageCode,
    });
    return undefined;
  }

  const result = await fetchTranslationsBatch(uuids, toLanguageCode);

  if (!result?.translations || !Object.keys(result.translations).length) {
    sendApiUpdate({
      '@type': 'failedMessageTranslations',
      chatId: chat.id,
      messageIds,
      toLanguageCode,
    });
    return undefined;
  }

  // Build ApiFormattedText array in the same order as messageIds,
  // guarding against empty translations from partial AI responses
  const successMessageIds: number[] = [];
  const successTranslations: ApiFormattedText[] = [];
  const failedMessageIds: number[] = [];

  for (let i = 0; i < messageIds.length; i++) {
    const seqNum = messageIds[i];
    const uuid = resolveMessageUuid(chat.id, seqNum);
    const entry = uuid ? result.translations[uuid] : undefined;
    const text = entry?.translated_text;

    if (text && text.length > 0) {
      successMessageIds.push(seqNum);
      successTranslations.push({ text, entities: [] });
    } else {
      failedMessageIds.push(seqNum);
    }
  }

  if (!successMessageIds.length) {
    sendApiUpdate({
      '@type': 'failedMessageTranslations',
      chatId: chat.id,
      messageIds,
      toLanguageCode,
    });
    return undefined;
  }

  if (failedMessageIds.length) {
    sendApiUpdate({
      '@type': 'failedMessageTranslations',
      chatId: chat.id,
      messageIds: failedMessageIds,
      toLanguageCode,
    });
  }

  sendApiUpdate({
    '@type': 'updateMessageTranslations',
    chatId: chat.id,
    messageIds: successMessageIds,
    translations: successTranslations,
    toLanguageCode,
  });

  return successTranslations;
}
