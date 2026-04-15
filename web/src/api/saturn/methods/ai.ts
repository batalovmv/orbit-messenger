// Phase 8A AI service Saturn client — Claude + Whisper integration.
//
// Two response shapes:
//   - summarizeChat / translateMessages return an AsyncGenerator<string>
//     backed by Server-Sent Events. Consumers read chunks and append to UI.
//   - suggestReply / transcribeVoice / fetchAiUsage are regular JSON calls.
//
// When the AI service returns 503 service_unavailable (API keys not yet
// configured), the streaming helpers yield nothing and return — the caller
// should detect this as "no output" and show the disabled banner.

import { ensureAuth, getAccessToken, getBaseUrl, request } from '../client';

export type AiSummarizeRequest = {
  chatId: string;
  timeRange?: '1h' | '6h' | '24h' | '7d';
  language?: string;
};

export type AiTranslateRequest = {
  messageIds: string[];
  chatId?: string;
  targetLanguage: string;
};

export type AiReplySuggestRequest = {
  chatId: string;
};

export type AiTranscribeRequest = {
  mediaId: string;
};

export type AiTranscribeResponse = {
  text: string;
  language?: string;
};

export type AiUsageStats = {
  total_requests: number;
  by_endpoint: Record<string, number>;
  input_tokens: number;
  output_tokens: number;
  period_start: string;
  cost_cents?: Record<string, number>;
  recent_samples?: Array<{
    endpoint: string;
    model: string;
    input_tokens: number;
    output_tokens: number;
    cost_cents: number;
    created_at: string;
  }>;
};

// ---------------------------------------------------------------------------
// SSE streaming helper
// ---------------------------------------------------------------------------

// Stream frame shape matches the backend encodeStreamEvent:
//   {"type":"delta","text":"Hello"}
//   {"type":"done","input_tokens":42,"output_tokens":87}
//   {"type":"error","message":"..."}
type StreamFrame =
  | { type: 'delta'; text: string }
  | { type: 'done'; input_tokens: number; output_tokens: number }
  | { type: 'error'; message: string };

/**
 * postSseStream opens a POST SSE stream to the given path and yields parsed
 * StreamFrame values. Always finishes cleanly (no throw) on `[DONE]` or end
 * of stream. Throws an ApiError-like object if the server responds non-2xx
 * BEFORE the stream starts.
 *
 * Why not EventSource: EventSource only supports GET and does not let us
 * send a JSON body, so we reimplement the tiny subset we need over fetch +
 * ReadableStream.
 */
async function* postSseStream<Body extends Record<string, unknown>>(
  path: string,
  body: Body,
): AsyncGenerator<StreamFrame, void, void> {
  await ensureAuth();

  const token = getAccessToken();
  const base = getBaseUrl() || `${window.location.origin}/api/v1`;

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    'Accept': 'text/event-stream',
    'X-Requested-With': 'XMLHttpRequest',
  };
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  const response = await fetch(`${base}${path}`, {
    method: 'POST',
    headers,
    credentials: 'include',
    body: JSON.stringify(body),
  });

  if (!response.ok) {
    // Try to surface JSON error payload; fall back to status text.
    let message = response.statusText;
    let code = 'unknown';
    try {
      const err = await response.json();
      message = err?.message || message;
      code = err?.error || code;
    } catch { /* noop */ }
    const error = new Error(message) as Error & { status: number; code: string };
    error.status = response.status;
    error.code = code;
    throw error;
  }

  if (!response.body) {
    return;
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder('utf-8');
  let buffer = '';

  try {
    while (true) {
      const { value, done } = await reader.read();
      if (done) return;

      buffer += decoder.decode(value, { stream: true });

      // SSE frames are separated by blank lines. Each frame can have one
      // or more `data: ...` lines; we join with \n per spec.
      let sepIndex = buffer.indexOf('\n\n');
      while (sepIndex !== -1) {
        const frameText = buffer.slice(0, sepIndex);
        buffer = buffer.slice(sepIndex + 2);

        const dataLines: string[] = [];
        for (const line of frameText.split('\n')) {
          if (line.startsWith('data:')) {
            dataLines.push(line.slice(5).trim());
          }
        }
        const data = dataLines.join('\n');
        if (data === '[DONE]' || data === '') {
          if (data === '[DONE]') return;
        } else {
          try {
            const parsed = JSON.parse(data) as StreamFrame;
            yield parsed;
            if (parsed.type === 'error' || parsed.type === 'done') {
              return;
            }
          } catch {
            // Malformed chunk — skip silently to avoid breaking the whole stream
          }
        }

        sepIndex = buffer.indexOf('\n\n');
      }
    }
  } finally {
    try { reader.releaseLock(); } catch { /* noop */ }
  }
}

// ---------------------------------------------------------------------------
// Streaming endpoints
// ---------------------------------------------------------------------------

/**
 * Opens a streaming summarize request for the given chat. Yields successive
 * text chunks; caller should concatenate and render progressively.
 *
 * Returns an AsyncGenerator<string> — each yielded value is a text delta
 * (NOT a full accumulated response). If the stream ends without yielding
 * anything, the UI should display an "AI unavailable" banner.
 */
export async function* summarizeChat(
  args: AiSummarizeRequest,
): AsyncGenerator<string, void, void> {
  const stream = postSseStream('/ai/summarize', {
    chat_id: args.chatId,
    time_range: args.timeRange ?? '1h',
    language: args.language ?? 'ru',
  });

  for await (const frame of stream) {
    if (frame.type === 'delta') {
      yield frame.text;
    } else if (frame.type === 'error') {
      throw new Error(frame.message);
    }
  }
}

export async function* translateMessages(
  args: AiTranslateRequest,
): AsyncGenerator<string, void, void> {
  const stream = postSseStream('/ai/translate', {
    message_ids: args.messageIds,
    chat_id: args.chatId,
    target_language: args.targetLanguage,
  });

  for await (const frame of stream) {
    if (frame.type === 'delta') {
      yield frame.text;
    } else if (frame.type === 'error') {
      throw new Error(frame.message);
    }
  }
}

// ---------------------------------------------------------------------------
// Non-streaming endpoints
// ---------------------------------------------------------------------------

export async function suggestReply(args: AiReplySuggestRequest): Promise<string[]> {
  const response = await request<{ suggestions: string[] }>(
    'POST',
    '/ai/reply-suggest',
    { chat_id: args.chatId },
  );
  return response?.suggestions ?? [];
}

export async function transcribeVoice(args: AiTranscribeRequest): Promise<AiTranscribeResponse> {
  return request<AiTranscribeResponse>(
    'POST',
    '/ai/transcribe',
    { media_id: args.mediaId },
  );
}

export async function fetchAiUsage(): Promise<AiUsageStats | undefined> {
  return request<AiUsageStats>('GET', '/ai/usage');
}

/**
 * semanticSearch currently returns an empty array — backend responds with
 * 501 Not Implemented (Phase 8A.2 pending embeddings + pgvector). Stub
 * exists so the UI can be wired now and start working as soon as the
 * backend implementation lands.
 */
export async function semanticSearch(_args: { query: string; chatId?: string }): Promise<[]> {
  try {
    await request('POST', '/ai/search', {
      query: _args.query,
      chat_id: _args.chatId,
    });
  } catch {
    // 501 expected until Phase 8A.2 — swallow silently
  }
  return [];
}
