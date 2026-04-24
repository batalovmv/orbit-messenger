// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import type { ApiUpdate, OnApiUpdate } from '../../types';

let onUpdate: OnApiUpdate | undefined;
let pendingUpdates: ApiUpdate[] = [];
let flushScheduled = false;

export function init(callback: OnApiUpdate) {
  onUpdate = callback;
}

export function sendApiUpdate(update: ApiUpdate) {
  pendingUpdates.push(update);
  scheduleFlush();
}

export function sendImmediateApiUpdate(update: ApiUpdate) {
  onUpdate?.(update);
}

function scheduleFlush() {
  if (flushScheduled) return;
  flushScheduled = true;
  Promise.resolve().then(flush);
}

function flush() {
  flushScheduled = false;
  const updates = pendingUpdates;
  pendingUpdates = [];

  for (const update of updates) {
    onUpdate?.(update);
  }
}
