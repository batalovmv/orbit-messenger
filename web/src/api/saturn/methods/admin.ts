// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import { ensureAuth, getAccessToken } from '../client';

const API_PREFIX = '/api/v1';

// Resolve the current Bearer token from the Saturn auth client. Falls back
// to the in-memory token (no refresh) when ensureAuth is unavailable so tests
// that don't initialise the client still work.
async function authHeader(): Promise<Record<string, string>> {
  let token: string | undefined;
  try {
    token = await ensureAuth();
  } catch {
    token = getAccessToken();
  }
  return token ? { Authorization: `Bearer ${token}` } : {};
}

async function jsonOrThrow<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let message = `HTTP ${res.status}`;
    try {
      const body = await res.json();
      if (body && typeof body.message === 'string') message = body.message;
    } catch (_e) { /* ignore */ }
    throw new Error(message);
  }
  return res.json() as Promise<T>;
}

export async function fetchAdminChatExport(chatId: string): Promise<Response> {
  return fetch(`${API_PREFIX}/admin/chats/${chatId}/export?format=json`, {
    headers: await authHeader(),
  });
}

export async function fetchAdminUserExport(userId: string): Promise<Response> {
  return fetch(`${API_PREFIX}/admin/users/${userId}/export?format=json`, {
    headers: await authHeader(),
  });
}

// ---------------------------------------------------------------------------
// Feature flags
// ---------------------------------------------------------------------------

export type AdminFlag = {
  key: string;
  enabled: boolean;
  description: string;
  metadata: Record<string, unknown>;
  exposure: 'unauth' | 'auth' | 'admin' | 'server_only';
  class: string;
  known: boolean;
  updated_at?: string;
};

export async function fetchAdminFlags(): Promise<AdminFlag[]> {
  const res = await fetch(`${API_PREFIX}/admin/feature-flags`, { headers: await authHeader() });
  const body = await jsonOrThrow<{ flags: AdminFlag[] }>(res);
  return body.flags || [];
}

export async function setAdminFlag(
  key: string,
  enabled: boolean,
  metadata?: Record<string, unknown>,
): Promise<AdminFlag> {
  const res = await fetch(`${API_PREFIX}/admin/feature-flags/${encodeURIComponent(key)}`, {
    method: 'PATCH',
    headers: { ...(await authHeader()), 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled, metadata: metadata ?? {} }),
  });
  const body = await jsonOrThrow<{ flag: AdminFlag }>(res);
  return body.flag;
}

// ---------------------------------------------------------------------------
// Maintenance mode (convenience over /admin/feature-flags/maintenance_mode)
// ---------------------------------------------------------------------------

export type MaintenanceUpdate = {
  enabled: boolean;
  message: string;
  block_writes: boolean;
};

export async function setAdminMaintenance(update: MaintenanceUpdate): Promise<AdminFlag> {
  const res = await fetch(`${API_PREFIX}/admin/admin-maintenance`, {
    method: 'POST',
    headers: { ...(await authHeader()), 'Content-Type': 'application/json' },
    body: JSON.stringify(update),
  });
  const body = await jsonOrThrow<{ flag: AdminFlag }>(res);
  return body.flag;
}

// ---------------------------------------------------------------------------
// System config (auth + public)
// ---------------------------------------------------------------------------

export type PublicFlag = { key: string; enabled: boolean; description?: string };
export type MaintenanceState = {
  active: boolean;
  message?: string;
  block_writes?: boolean;
  since?: string;
  updated_by?: string;
};
export type SystemConfig = { flags: PublicFlag[]; maintenance: MaintenanceState };

export async function fetchSystemConfig(): Promise<SystemConfig> {
  const res = await fetch(`${API_PREFIX}/system/config`, { headers: await authHeader() });
  return jsonOrThrow<SystemConfig>(res);
}

export async function fetchPublicSystemConfig(): Promise<SystemConfig> {
  // No auth header — used on the login screen and as a fallback.
  const res = await fetch(`${API_PREFIX}/public/system/config`);
  return jsonOrThrow<SystemConfig>(res);
}

// ---------------------------------------------------------------------------
// Audit log
// ---------------------------------------------------------------------------

export type AuditEntry = {
  id: number;
  actor_id: string;
  actor_name?: string;
  action: string;
  target_type: string;
  target_id?: string;
  details?: Record<string, unknown>;
  ip_address?: string;
  user_agent?: string;
  created_at: string;
};

export type AuditPage = {
  data: AuditEntry[];
  cursor?: string;
  has_more: boolean;
};

export type AuditQuery = {
  q?: string;
  action?: string;
  actor_id?: string;
  target_type?: string;
  target_id?: string;
  since?: string;
  until?: string;
  cursor?: string;
  limit?: number;
};

export async function fetchAuditLog(query: AuditQuery): Promise<AuditPage> {
  const params = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) {
    if (v !== undefined && v !== '' && v !== null) {
      params.set(k, String(v));
    }
  }
  const url = `${API_PREFIX}/admin/audit-log${params.toString() ? `?${params.toString()}` : ''}`;
  const res = await fetch(url, { headers: await authHeader() });
  const raw = await jsonOrThrow<{ data?: AuditEntry[]; cursor?: string; has_more?: boolean }>(res);
  return {
    data: raw.data || [],
    cursor: raw.cursor,
    has_more: Boolean(raw.has_more),
  };
}

// ---------------------------------------------------------------------------
// Welcome flow (mig 069)
// ---------------------------------------------------------------------------

// setChatDefaultStatus toggles the per-chat is_default_for_new_users flag.
// Used by the chat-settings Switcher (admin/superadmin only — gateway proxies
// PUT /admin/chats/:id/default-status to messaging which enforces the role).
export async function setChatDefaultStatus(
  chatId: string,
  isDefault: boolean,
  joinOrder = 0,
): Promise<{ chat_id: string; is_default: boolean; default_join_order: number }> {
  const res = await fetch(`${API_PREFIX}/admin/chats/${chatId}/default-status`, {
    method: 'PUT',
    headers: { ...(await authHeader()), 'Content-Type': 'application/json' },
    body: JSON.stringify({ is_default: isDefault, default_join_order: joinOrder }),
  });
  return jsonOrThrow(res);
}

// backfillDefaultChats kicks off a manual cross-user backfill: every existing
// user is added to every chat marked as default. Returns the count of newly-
// inserted memberships so the UI can show "Joined N memberships."
export async function backfillDefaultChats(): Promise<{ inserted: number }> {
  const res = await fetch(`${API_PREFIX}/admin/default-chats/backfill`, {
    method: 'POST',
    headers: await authHeader(),
  });
  return jsonOrThrow(res);
}
