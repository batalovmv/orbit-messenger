// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Saturn stub — toggleAutoTranslation.
// In our self-hosted environment every chat supports auto-translation;
// the setting is stored client-side in chat.hasAutoTranslation via the
// reducer. There's no server-side flag to persist (unlike Telegram which
// stores it per-channel). We simply resolve(true) so the global state
// update in the action handler proceeds.

export function toggleAutoTranslation({
  isEnabled,
}: {
  chat: { id: string };
  isEnabled: boolean;
}) {
  return Promise.resolve(isEnabled);
}
