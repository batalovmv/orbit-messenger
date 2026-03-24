import type { ApiInitialArgs, ApiOnProgress, OnApiUpdate } from '../../types';
import type { MethodArgs, MethodResponse, Methods } from './types';

import { DEBUG } from '../../../config';
import { init as initClient } from './client';
import * as methods from './index';

export function initApi(onUpdate: OnApiUpdate, initialArgs: ApiInitialArgs) {
  initClient(initialArgs, onUpdate);
  return Promise.resolve();
}

export function callApi<T extends keyof Methods>(fnName: T, ...args: MethodArgs<T>): MethodResponse<T>;
export function callApi(fnName: string, ...args: any[]): any;
export function callApi(fnName: string, ...args: any[]): any {
  const method = (methods as Record<string, Function>)[fnName];
  if (!method) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.warn(`[Saturn] Method not implemented: ${fnName}`);
    }
    return Promise.resolve(undefined);
  }
  return method(...args);
}

export function cancelApiProgress(progressCallback: ApiOnProgress) {
  progressCallback.isCanceled = true;
}
