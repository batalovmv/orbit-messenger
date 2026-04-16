import type { ApiInitialArgs, ApiOnProgress, OnApiUpdate } from '../../types';
import type { MethodArgs, MethodResponse, Methods } from './types';

import { DEBUG } from '../../../config';
import { init as initClient } from './client';
import * as methods from './index';

const SILENCED_METHODS = new Set([
  'acceptBotUrlAuth',
  'acceptLinkUrlAuth',
  'acceptPhoneCall',
  'answerCallbackButton',
  'fetchPremiumPromo',
  'fetchSavedGifs',
  'fetchStickerSetsForEmoji',
  'fetchTopInlineBots',
  'requestMainWebView',
  'requestSimpleWebView',
]);

export function initApi(onUpdate: OnApiUpdate, initialArgs: ApiInitialArgs) {
  initClient(initialArgs, onUpdate);
  return Promise.resolve();
}

export function callApi<T extends keyof Methods>(fnName: T, ...args: MethodArgs<T>): MethodResponse<T>;
export function callApi(fnName: string, ...args: any[]): any;
export function callApi(fnName: string, ...args: any[]): any {
  const method = (methods as unknown as Record<string, (...args: any[]) => unknown>)[fnName];
  if (!method) {
    if (DEBUG && !SILENCED_METHODS.has(fnName)) {
      // eslint-disable-next-line no-console
      console.warn(`[Saturn] Method not implemented: ${fnName}`);
    }
    return Promise.resolve(undefined);
  }
  if (fnName === 'sendMessage') {
    // eslint-disable-next-line no-console
    console.error('[SEND DEBUG] callApi sendMessage', { hasMethod: !!method, argKeys: Object.keys(args[0] || {}) });
  }
  try {
    const result = method(...args);
    if (fnName === 'sendMessage') {
      // eslint-disable-next-line no-console
      console.error('[SEND DEBUG] callApi sendMessage returned', typeof result);
      if (result instanceof Promise) {
        result.catch((err: unknown) => {
          // eslint-disable-next-line no-console
          console.error('[SEND DEBUG] callApi sendMessage REJECTED', err);
        });
      }
    }
    return result;
  } catch (err) {
    // eslint-disable-next-line no-console
    console.error('[SEND DEBUG] callApi sendMessage THREW', err);
    throw err;
  }
}

export function cancelApiProgress(progressCallback: ApiOnProgress) {
  progressCallback.isCanceled = true;
  progressCallback.abort?.();
}
