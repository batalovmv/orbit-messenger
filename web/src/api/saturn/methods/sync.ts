// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Sync methods for reconnect scenarios

export function fetchDifference() {
  // Saturn doesn't have a diff protocol like MTProto.
  // On reconnect, the frontend re-fetches active chats.
  // This is a no-op stub; real sync happens via WS reconnect + fetchChats.
  return Promise.resolve(undefined);
}
