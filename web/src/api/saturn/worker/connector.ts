// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Saturn connector — simplified, no Web Worker needed.
// REST/WebSocket calls run in the main thread (fetch is non-blocking).
// Preserves the same callApi/initApi interface that TG Web A global actions use.

import type { ApiInitialArgs, ApiOnProgress, OnApiUpdate } from '../../types';
import type { MethodArgs, MethodResponse, Methods } from '../methods/types';

import Deferred from '../../../util/Deferred';
import { callApi as callMethod, cancelApiProgress as cancelProgress, initApi as initMethods } from '../methods/init';

let isInited = false;
let initPromise: Promise<void> | undefined;
let apiRequestsQueue: { fnName: any; args: any; deferred: Deferred<any> }[] = [];

export function initApi(onUpdate: OnApiUpdate, initialArgs: ApiInitialArgs) {
  if (isInited) return Promise.resolve();

  initPromise = initMethods(onUpdate, initialArgs).then(() => {
    isInited = true;
    initPromise = undefined;

    apiRequestsQueue.forEach((request) => {
      callApi(request.fnName, ...request.args)
        .then(request.deferred.resolve)
        .catch(request.deferred.reject);
    });
    apiRequestsQueue = [];
  }).catch(() => {
    initPromise = undefined;
  });

  return initPromise;
}

type EnsurePromise<T> = Promise<Awaited<T>>;

export function callApi<T extends keyof Methods>(
  fnName: T, ...args: MethodArgs<T>
): EnsurePromise<MethodResponse<T>>;
export function callApi(fnName: string, ...args: any[]): Promise<any>;
export function callApi(fnName: string, ...args: any[]): Promise<any> {
  if (!isInited) {
    // If initApi is in progress, wait for it then execute
    if (initPromise) {
      return initPromise.then(() => callApi(fnName, ...args));
    }
    // Queue until initApi is called
    const deferred = new Deferred();
    apiRequestsQueue.push({ fnName, args, deferred });
    return deferred.promise;
  }

  try {
    const result = callMethod(fnName, ...args);
    return result instanceof Promise ? result : Promise.resolve(result);
  } catch (err) {
    return Promise.reject(err);
  }
}

export function callApiLocal<T extends keyof Methods>(
  fnName: T, ...args: MethodArgs<T>
): EnsurePromise<MethodResponse<T>>;
export function callApiLocal(fnName: string, ...args: any[]): Promise<any>;
export function callApiLocal(fnName: string, ...args: any[]): Promise<any> {
  return callApi(fnName, ...args);
}

export function cancelApiProgress(progressCallback: ApiOnProgress) {
  cancelProgress(progressCallback);
}

export function cancelApiProgressMaster(progressCallback: unknown) {
  if (typeof progressCallback === 'function') {
    cancelProgress(progressCallback as ApiOnProgress);
  }
}

export function handleMethodCallback(..._args: any[]) {
  // No-op for Saturn (no worker)
}

export function handleMethodResponse(..._args: any[]) {
  // No-op for Saturn (no worker)
}

export function updateFullLocalDb(..._args: any[]) {
  // No-op for Saturn (no GramJS localDb)
}

export function updateLocalDb(..._args: any[]) {
  // No-op for Saturn (no GramJS localDb)
}

export function setShouldEnableDebugLog(_value: boolean) {
  // No-op for Saturn
}
