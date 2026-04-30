// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// localizeAdminError maps known backend error messages to russian-first
// frontend strings. The backend (`apperror.BadRequest("…")` etc.) returns
// english-only `.message` payloads — surfacing them raw in the admin UI
// produced lines like "end_at must be strictly after start_at" mid-russian
// flow during the 2026-04-29 audit.
//
// Approach: substring match against the raw error message; first hit wins.
// Falls through to the original text if nothing matches, so unknown errors
// still surface (visible-and-ugly is better than silent-and-fine for ops).
//
// When adding entries, prefer the most stable substring — the backend code
// is the source of truth, so the substring should survive small wording
// tweaks but still be unique. If two patterns share a phrase, list the more
// specific one first.

import type { LangFn } from '../../../util/localization';

type AdminErrorPattern = {
  // Substring we look for in the raw backend message (case-insensitive).
  match: string;
  // Localization key (string literal so lang() typing accepts it).
  key: 'AdminErrorMaintenanceWindowInverted'
    | 'AdminErrorUnknownFlag'
    | 'AdminErrorRateLimit'
    | 'AdminErrorUnauthorized'
    | 'AdminErrorForbidden'
    | 'AdminErrorInviteRoleForbidden'
    | 'AdminErrorNotFound'
    | 'AdminErrorMaintenance';
};

const PATTERNS: AdminErrorPattern[] = [
  { match: 'must be strictly after', key: 'AdminErrorMaintenanceWindowInverted' },
  { match: 'unknown feature flag', key: 'AdminErrorUnknownFlag' },
  { match: 'cannot create invites with this role', key: 'AdminErrorInviteRoleForbidden' },
  { match: 'rate limit', key: 'AdminErrorRateLimit' },
  // HTTP-status passthrough from apiClient — we keep the substring loose so
  // both `HTTP 401` and `unauthorized` route to the same string.
  { match: 'unauthorized', key: 'AdminErrorUnauthorized' },
  { match: 'http 401', key: 'AdminErrorUnauthorized' },
  { match: 'forbidden', key: 'AdminErrorForbidden' },
  { match: 'http 403', key: 'AdminErrorForbidden' },
  { match: 'not found', key: 'AdminErrorNotFound' },
  { match: 'http 404', key: 'AdminErrorNotFound' },
  { match: 'maintenance', key: 'AdminErrorMaintenance' },
  { match: 'http 503', key: 'AdminErrorMaintenance' },
];

export function localizeAdminError(lang: LangFn, raw: unknown, fallback?: string): string {
  const message = errorToString(raw, fallback);
  if (!message) return fallback || '';

  const lower = message.toLowerCase();
  for (const { match, key } of PATTERNS) {
    if (lower.includes(match)) {
      return lang(key);
    }
  }
  return message;
}

function errorToString(raw: unknown, fallback?: string): string {
  if (raw instanceof Error) return raw.message || fallback || '';
  if (typeof raw === 'string') return raw;
  if (raw && typeof raw === 'object') {
    const m = (raw as { message?: unknown }).message;
    if (typeof m === 'string') return m;
  }
  return fallback || '';
}
