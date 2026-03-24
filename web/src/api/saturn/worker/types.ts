// Saturn doesn't use a Web Worker — REST API calls run in the main thread.
// This file exists for interface compatibility with the gramjs worker pattern.

import type { ApiInitialArgs, ApiUpdate } from '../../types';
import type { MethodArgs, MethodResponse, Methods } from '../methods/types';

export type ThenArg<T> = T extends Promise<infer U> ? U : T;

export type WorkerPayload =
  {
    type: 'updates';
    updates: ApiUpdate[];
  }
  |
  {
    type: 'methodResponse';
    messageId: string;
    response?: ThenArg<MethodResponse<keyof Methods>>;
    error?: { message: string };
  }
  |
  {
    type: 'methodCallback';
    messageId: string;
    callbackArgs: any[];
  }
  |
  {
    type: 'unhandledError';
    error?: { message: string };
  };

export type OriginPayload =
  {
    type: 'initApi';
    messageId?: string;
    args: [ApiInitialArgs];
  }
  |
  {
    type: 'callMethod';
    messageId?: string;
    name: keyof Methods;
    args: MethodArgs<keyof Methods>;
    withCallback?: boolean;
  }
  |
  {
    type: 'ping';
    messageId?: string;
  }
  |
  {
    type: 'cancelProgress';
    messageId: string;
  };
