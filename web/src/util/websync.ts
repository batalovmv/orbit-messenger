// Orbit: websync disabled — this was Telegram-specific session sync via t.me

export const forceWebsync = (_authed: boolean) => Promise.resolve();
export const startWebsync = () => {};
export const stopWebsync = () => {};
export const clearWebsync = () => {};
