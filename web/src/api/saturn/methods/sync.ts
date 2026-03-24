// Sync methods for reconnect scenarios

export async function fetchDifference() {
  // Saturn doesn't have a diff protocol like MTProto.
  // On reconnect, the frontend re-fetches active chats.
  // This is a no-op stub; real sync happens via WS reconnect + fetchChats.
  return undefined;
}
