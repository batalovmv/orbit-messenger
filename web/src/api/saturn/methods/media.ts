import { getAccessToken, getBaseUrl } from '../client';
import { request } from '../client';
import type { SaturnMediaAttachment, SaturnPaginatedResponse } from '../types';

interface MediaUploadResponse {
  id: string;
  type: string;
  mime_type: string;
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

// Upload a file via multipart/form-data with progress tracking.
// Uses XMLHttpRequest because fetch() doesn't support upload progress.
export function uploadMedia(
  file: File | Blob,
  type?: string,
  onProgress?: (loaded: number, total: number) => void,
  isOneTime = false,
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

    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(JSON.parse(xhr.responseText));
      } else {
        reject(new Error(`Upload failed: ${xhr.status} ${xhr.statusText}`));
      }
    };
    xhr.onerror = () => reject(new Error('Upload network error'));
    xhr.send(formData);
  });
}

// Start a chunked upload for large files.
export function initChunkedUpload(
  filename: string,
  mimeType: string,
  totalSize: number,
  mediaType?: string,
): Promise<ChunkedInitResponse> {
  return request<ChunkedInitResponse>('POST', '/media/upload/chunked/init', {
    filename,
    mime_type: mimeType,
    total_size: totalSize,
    media_type: mediaType || '',
  });
}

// Upload a single chunk.
export async function uploadChunk(
  uploadId: string,
  partNumber: number,
  chunk: Blob,
): Promise<ChunkedPartResponse> {
  const token = getAccessToken();
  const resp = await fetch(`${getBaseUrl()}/media/upload/chunked/${uploadId}`, {
    method: 'POST',
    headers: {
      'Authorization': token ? `Bearer ${token}` : '',
      'X-Part-Number': String(partNumber),
    },
    credentials: 'include',
    body: chunk,
  });

  if (!resp.ok) {
    throw new Error(`Chunk upload failed: ${resp.status}`);
  }
  return resp.json();
}

// Complete a chunked upload.
export function completeChunkedUpload(
  uploadId: string,
  isOneTime = false,
): Promise<MediaUploadResponse> {
  return request<MediaUploadResponse>('POST', `/media/upload/chunked/${uploadId}/complete`, {
    is_one_time: isOneTime,
  });
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
