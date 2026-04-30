// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import type { SaturnMediaAttachment, SaturnPaginatedResponse } from '../types';

import { ApiError, getAccessToken, getBaseUrl } from '../client';
import { request } from '../client';

const SIMPLE_UPLOAD_LIMIT = 50 * 1024 * 1024;

interface MediaUploadResponse {
  id: string;
  type: string;
  mime_type: string;
  page_count?: number;
  original_filename?: string;
  size_bytes: number;
  url?: string;
  thumbnail_url?: string;
  medium_url?: string;
  width?: number;
  height?: number;
  duration_seconds?: number;
  waveform_data?: number[];
  processing_status: string;
}

interface ChunkedInitResponse {
  upload_id: string;
  chunk_size: number;
  total_chunks: number;
}

interface ChunkedPartResponse {
  uploaded_chunks: number;
  total_chunks: number;
}

type UploadMediaOptions = {
  fileName?: string;
  mimeType?: string;
  uploadId?: string;
};

type CancelableUpload<T> = {
  abort: NoneToVoidFunction;
  response: Promise<T>;
  uploadId: string;
};

const activeUploadControllers = new Map<string, AbortController>();
const activeChunkedUploadIds = new Map<string, string>();

// Upload a file via multipart/form-data with progress tracking.
// Uses XMLHttpRequest because fetch() doesn't support upload progress.
export function uploadMedia(
  file: File | Blob,
  type?: string,
  onProgress?: (loaded: number, total: number) => void,
  isOneTime = false,
  options: UploadMediaOptions = {},
): CancelableUpload<MediaUploadResponse> {
  const uploadId = options.uploadId || buildUploadId();
  const controller = new AbortController();

  activeUploadControllers.set(uploadId, controller);

  const abort = () => cancelMediaUpload(uploadId);

  const response = (file.size > SIMPLE_UPLOAD_LIMIT
    ? uploadChunkedMedia(file, type, onProgress, isOneTime, controller.signal, uploadId, options)
    : uploadSimpleMedia(file, type, onProgress, isOneTime, controller.signal)
  ).finally(() => {
    activeUploadControllers.delete(uploadId);
    activeChunkedUploadIds.delete(uploadId);
  });

  return {
    abort,
    response,
    uploadId,
  };
}

function uploadSimpleMedia(
  file: File | Blob,
  type: string | undefined,
  onProgress: ((loaded: number, total: number) => void) | undefined,
  isOneTime: boolean,
  signal: AbortSignal,
): Promise<MediaUploadResponse> {
  return new Promise((resolve, reject) => {
    const formData = new FormData();
    formData.append('file', file);
    if (type) formData.append('type', type);
    if (isOneTime) formData.append('is_one_time', 'true');

    const xhr = new XMLHttpRequest();
    xhr.open('POST', `${getBaseUrl()}/media/upload`);

    const token = getAccessToken();
    if (token) {
      xhr.setRequestHeader('Authorization', `Bearer ${token}`);
    }
    xhr.withCredentials = true;

    if (onProgress) {
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) onProgress(e.loaded, e.total);
      };
    }

    signal.addEventListener('abort', () => {
      xhr.abort();
    }, { once: true });

    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(JSON.parse(xhr.responseText));
      } else {
        reject(buildUploadError(xhr.status, xhr.statusText, xhr.responseText));
      }
    };
    xhr.onabort = () => reject(createAbortError());
    xhr.onerror = () => reject(new Error('Upload network error'));
    xhr.send(formData);
  });
}

/**
 * Phase 7.1: upload an AES-256-GCM ciphertext blob to the media service.
 *
 * The server stores the bytes as-is (no processing, no thumbnail), so the
 * caller must encrypt the plaintext client-side and pass the raw ciphertext
 * here. The declared media type is only used by the UI to pick a placeholder;
 * the backend treats the blob as `application/octet-stream`.
 *
 * Filename is optional and carried in a header so it never leaks via query
 * params. The encryption key/nonce stay inside the E2E envelope, not here.
 */
export function uploadEncryptedMedia(
  ciphertext: Uint8Array,
  declaredType: string,
  declaredFilename = '',
  isOneTime = false,
  signal?: AbortSignal,
): Promise<MediaUploadResponse> {
  if (ciphertext.byteLength === 0) {
    return Promise.reject(new Error('uploadEncryptedMedia: empty ciphertext'));
  }
  if (ciphertext.byteLength > SIMPLE_UPLOAD_LIMIT) {
    return Promise.reject(new Error('uploadEncryptedMedia: ciphertext exceeds simple upload limit'));
  }
  const token = getAccessToken();
  const headers: Record<string, string> = {
    'Content-Type': 'application/octet-stream',
    'X-Media-Type': declaredType,
  };
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  if (declaredFilename) {
    headers['X-Media-Filename'] = declaredFilename;
  }
  if (isOneTime) {
    headers['X-Is-One-Time'] = 'true';
  }
  return fetch(`${getBaseUrl()}/media/upload/encrypted`, {
    method: 'POST',
    headers,
    credentials: 'include',
    body: new Blob([
      ciphertext.buffer.slice(ciphertext.byteOffset, ciphertext.byteOffset + ciphertext.byteLength) as ArrayBuffer,
    ], { type: 'application/octet-stream' }),
    signal,
  }).then(async (resp) => {
    if (!resp.ok) {
      const body = await resp.text().catch(() => '');
      throw buildUploadError(resp.status, resp.statusText, body);
    }
    return resp.json();
  });
}

// Start a chunked upload for large files.
export function initChunkedUpload(
  filename: string,
  mimeType: string,
  totalSize: number,
  mediaType?: string,
  signal?: AbortSignal,
): Promise<ChunkedInitResponse> {
  return request<ChunkedInitResponse>('POST', '/media/upload/chunked/init', {
    filename,
    mime_type: mimeType,
    total_size: totalSize,
    media_type: mediaType || '',
  }, { signal });
}

// Upload a single chunk.
export async function uploadChunk(
  uploadId: string,
  partNumber: number,
  chunk: Blob,
  signal?: AbortSignal,
): Promise<ChunkedPartResponse> {
  const token = getAccessToken();
  const resp = await fetch(`${getBaseUrl()}/media/upload/chunked/${uploadId}`, {
    method: 'POST',
    headers: {
      Authorization: token ? `Bearer ${token}` : '',
      'X-Part-Number': String(partNumber),
    },
    credentials: 'include',
    body: chunk,
    signal,
  });

  if (!resp.ok) {
    const body = await resp.text().catch(() => '');
    throw buildUploadError(resp.status, resp.statusText, body);
  }
  return resp.json();
}

// Complete a chunked upload.
export function completeChunkedUpload(
  uploadId: string,
  isOneTime = false,
  signal?: AbortSignal,
): Promise<MediaUploadResponse> {
  return request<MediaUploadResponse>('POST', `/media/upload/chunked/${uploadId}/complete`, {
    is_one_time: isOneTime,
  }, { signal });
}

export function abortChunkedUpload(uploadId: string): Promise<void> {
  return request<void>('DELETE', `/media/upload/chunked/${uploadId}`);
}

export function cancelMediaUpload(uploadId: string) {
  const controller = activeUploadControllers.get(uploadId);
  if (!controller || controller.signal.aborted) {
    return;
  }

  controller.abort();

  const chunkedUploadId = activeChunkedUploadIds.get(uploadId);
  if (chunkedUploadId) {
    void abortChunkedUpload(chunkedUploadId).catch(() => {
      // Ignore best-effort cleanup failures on cancel.
    });
  }
}

// Get media metadata.
export function fetchMediaInfo(mediaId: string): Promise<MediaUploadResponse> {
  return request<MediaUploadResponse>('GET', `/media/${mediaId}/info`);
}

// Delete a media file.
export function deleteMedia(mediaId: string): Promise<void> {
  return request<void>('DELETE', `/media/${mediaId}`);
}

// Fetch shared media for a chat (gallery tab).
export function fetchSharedMedia(
  chatId: string,
  type?: string,
  cursor?: string,
  limit = 20,
): Promise<SaturnPaginatedResponse<SaturnMediaAttachment>> {
  let path = `/chats/${chatId}/media?limit=${limit}`;
  if (type) path += `&type=${type}`;
  if (cursor) path += `&cursor=${cursor}`;
  return request<SaturnPaginatedResponse<SaturnMediaAttachment>>('GET', path);
}

// Update chat photo.
export function updateChatPhoto(chatId: string, avatarUrl: string): Promise<void> {
  return request<void>('PUT', `/chats/${chatId}/photo`, { avatar_url: avatarUrl });
}

// Delete chat photo.
export function deleteChatPhoto(chatId: string): Promise<void> {
  return request<void>('DELETE', `/chats/${chatId}/photo`);
}

async function uploadChunkedMedia(
  file: File | Blob,
  type: string | undefined,
  onProgress: ((loaded: number, total: number) => void) | undefined,
  isOneTime: boolean,
  signal: AbortSignal,
  uploadId: string,
  options: UploadMediaOptions,
) {
  const fileName = options.fileName || getFileName(file);
  const mimeType = options.mimeType || file.type || 'application/octet-stream';
  const initResult = await initChunkedUpload(fileName, mimeType, file.size, type, signal);

  activeChunkedUploadIds.set(uploadId, initResult.upload_id);

  let loaded = 0;

  try {
    for (let partNumber = 0; partNumber < initResult.total_chunks; partNumber++) {
      const start = partNumber * initResult.chunk_size;
      const end = Math.min(start + initResult.chunk_size, file.size);
      const chunk = file.slice(start, end);

      await uploadChunk(initResult.upload_id, partNumber + 1, chunk, signal);

      loaded += chunk.size;
      onProgress?.(loaded, file.size);
    }

    return await completeChunkedUpload(initResult.upload_id, isOneTime, signal);
  } catch (error) {
    if (isAbortError(error)) {
      await abortChunkedUpload(initResult.upload_id).catch(() => {
        // Ignore best-effort cleanup failures on cancel.
      });
    }

    throw error;
  }
}

function buildUploadId() {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID();
  }

  return `upload-${Date.now()}-${Math.round(Math.random() * 1e9)}`;
}

function createAbortError() {
  if (typeof DOMException !== 'undefined') {
    return new DOMException('Upload aborted', 'AbortError');
  }

  const error = new Error('Upload aborted');
  error.name = 'AbortError';

  return error;
}

function getFileName(file: File | Blob) {
  return file instanceof File ? file.name : 'upload.bin';
}

function isAbortError(error: unknown) {
  return error instanceof Error && error.name === 'AbortError';
}

// Backend (services/media) returns AppError-shaped JSON: {code, message, status}.
// Carry the code so the UI can branch on virus_detected, file_too_large, etc.
function buildUploadError(status: number, statusText: string, body: string): ApiError {
  let code = 'unknown';
  let message = `Upload failed: ${status} ${statusText}`.trim();
  if (body) {
    try {
      const parsed = JSON.parse(body) as { code?: unknown; error?: unknown; message?: unknown };
      if (typeof parsed.code === 'string' && parsed.code) {
        code = parsed.code;
      } else if (typeof parsed.error === 'string' && parsed.error) {
        code = parsed.error;
      }
      if (typeof parsed.message === 'string' && parsed.message) {
        message = parsed.message;
      }
    } catch {
      // body wasn't JSON — keep defaults.
    }
  }
  return new ApiError(message, status, code);
}
