import {
  DEBUG,
  MEDIA_CACHE_MAX_BYTES,
  MEDIA_PROGRESSIVE_CACHE_DISABLED,
  MEDIA_PROGRESSIVE_CACHE_NAME,
} from '../config';
import generateUniqueId from '../util/generateUniqueId';
import { getAccountSlot } from '../util/multiaccount';
import { pause } from '../util/schedulers';

declare const self: ServiceWorkerGlobalScope;

type PartInfo = {
  type: 'PartInfo';
  arrayBuffer: ArrayBuffer;
  mimeType: 'string';
  fullSize: number;
};

type RequestStates = {
  resolve: (response: PartInfo) => void;
  reject: () => void;
};

const MB = 1024 * 1024;
const DEFAULT_PART_SIZE = 0.5 * MB;
const MAX_END_TO_CACHE = 2 * MB - 1; // We only cache the first 2 MB of each file
const PART_TIMEOUT = 60000;

const requestStates = new Map<string, RequestStates>();

export async function respondForProgressive(e: FetchEvent) {
  const { url } = e.request;
  const accountSlot = getAccountSlot(url);
  const range = e.request.headers.get('range');
  const bytes = /^bytes=(\d+)-(\d+)?$/g.exec(range || '');

  // When no Range header (initial <video> request), treat as bytes=0-
  const start = bytes ? Number(bytes[1]) : 0;
  const originalEnd = bytes ? Number(bytes[2]) : NaN;

  let end = originalEnd;
  if (!end || Number.isNaN(end) || (end - start + 1) > DEFAULT_PART_SIZE) {
    end = start + DEFAULT_PART_SIZE - 1;
  }

  const parsedUrl = new URL(url);

  // Optimization for Safari
  if (start === 0 && end === 1) {
    const fileSizeParam = parsedUrl.searchParams.get('fileSize');
    const fileSize = fileSizeParam && Number(fileSizeParam);
    const mimeType = parsedUrl.searchParams.get('mimeType');

    if (fileSize && mimeType) {
      return new Response(new Uint8Array(2).buffer, {
        status: 206,
        statusText: 'Partial Content',
        headers: [
          ['Content-Range', `bytes 0-1/${fileSize}`],
          ['Accept-Ranges', 'bytes'],
          ['Content-Length', '2'],
          ['Content-Type', mimeType],
        ],
      });
    }
  }

  parsedUrl.searchParams.set('start', String(start));
  parsedUrl.searchParams.set('end', String(end));
  const cacheKey = parsedUrl.href;
  const [cachedArrayBuffer, cachedHeaders] = !MEDIA_PROGRESSIVE_CACHE_DISABLED
    ? await fetchFromCache(accountSlot, cacheKey) : [];

  if (DEBUG) {
    // eslint-disable-next-line no-console
    console.log(
      `FETCH PROGRESSIVE ${cacheKey} (request: ${start}-${originalEnd}) CACHED: ${Boolean(cachedArrayBuffer)}`,
    );
  }

  if (cachedArrayBuffer) {
    return new Response(cachedArrayBuffer, {
      status: 206,
      statusText: 'Partial Content',
      headers: cachedHeaders,
    });
  }

  let partInfo;
  try {
    partInfo = await requestPart(e, { url, start, end });
  } catch (err) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.error('FETCH PROGRESSIVE', err);
    }
  }

  if (!partInfo) {
    return new Response('', {
      status: 500,
      statusText: 'Failed to fetch progressive part',
    });
  }

  const { arrayBuffer, fullSize, mimeType } = partInfo;

  const partSize = Math.min(end - start + 1, arrayBuffer.byteLength);
  end = start + partSize - 1;
  const arrayBufferPart = arrayBuffer.slice(0, partSize);

  // When the original request had no Range header, respond with 200 (full content)
  // so <video> elements accept the response. Otherwise use 206 Partial Content.
  if (!range) {
    return new Response(arrayBufferPart, {
      status: 200,
      statusText: 'OK',
      headers: [
        ['Accept-Ranges', 'bytes'],
        ['Content-Length', String(partSize)],
        ['Content-Type', mimeType],
      ],
    });
  }

  const headers: [string, string][] = [
    ['Content-Range', `bytes ${start}-${end}/${fullSize}`],
    ['Accept-Ranges', 'bytes'],
    ['Content-Length', String(partSize)],
    ['Content-Type', mimeType],
  ];

  if (!MEDIA_PROGRESSIVE_CACHE_DISABLED && partSize <= MEDIA_CACHE_MAX_BYTES && end < MAX_END_TO_CACHE) {
    saveToCache(accountSlot, cacheKey, arrayBufferPart, headers);
  }

  return new Response(arrayBufferPart, {
    status: 206,
    statusText: 'Partial Content',
    headers,
  });
}

const LAST_ACCESS_HEADER = 'X-Last-Access';
const ACCESS_THROTTLE_MS = 24 * 60 * 60 * 1000;

// We can not cache 206 responses: https://github.com/GoogleChrome/workbox/issues/1644#issuecomment-638741359
async function fetchFromCache(accountSlot: number | undefined, cacheKey: string) {
  const cacheName = !accountSlot ? MEDIA_PROGRESSIVE_CACHE_NAME : `${MEDIA_PROGRESSIVE_CACHE_NAME}_${accountSlot}`;
  const cache = await self.caches.open(cacheName);

  const arrayBufferKey = `${cacheKey}&type=arrayBuffer`;
  const headersKey = `${cacheKey}&type=headers`;
  const headersRequest = new Request(headersKey);
  const [bodyResponse, headersResponse] = await Promise.all([
    cache.match(arrayBufferKey),
    cache.match(headersRequest),
  ]);

  // Only refresh X-Last-Access on the lightweight headers entry, never on
  // the multi-MB body. Re-`cache.put`-ing the body Response while a <video>
  // element is streaming through it causes Chromium to abort the in-flight
  // stream — random media playback failure under cache pressure.
  if (headersResponse) {
    void touchLastAccess(cache, headersRequest, headersResponse);
  }

  return Promise.all([
    bodyResponse ? bodyResponse.arrayBuffer() : Promise.resolve(undefined),
    headersResponse ? headersResponse.json() : Promise.resolve(undefined),
  ]);
}

async function saveToCache(
  accountSlot: number | undefined, cacheKey: string, arrayBuffer: ArrayBuffer, headers: HeadersInit,
) {
  const cacheName = !accountSlot ? MEDIA_PROGRESSIVE_CACHE_NAME : `${MEDIA_PROGRESSIVE_CACHE_NAME}_${accountSlot}`;
  const cache = await self.caches.open(cacheName);

  // Stamp X-Last-Access so quota/TTL eviction in cacheApi.ts can prune
  // progressive entries. Also stamp Content-Length so the quota pass can
  // size the body cheaply via headers — without it, listEntries() would fall
  // back to `response.clone().blob()`, pulling every chunk into main thread
  // memory each cycle (OOM under load).
  const now = Date.now().toString();
  return Promise.all([
    cache.put(
      new Request(`${cacheKey}&type=arrayBuffer`),
      new Response(arrayBuffer, {
        headers: {
          [LAST_ACCESS_HEADER]: now,
          'Content-Length': String(arrayBuffer.byteLength),
        },
      }),
    ),
    cache.put(
      new Request(`${cacheKey}&type=headers`),
      new Response(JSON.stringify(headers), { headers: { [LAST_ACCESS_HEADER]: now } }),
    ),
  ]);
}

async function touchLastAccess(cache: Cache, request: Request, response: Response) {
  try {
    const lastAccess = Number(response.headers.get(LAST_ACCESS_HEADER));
    const now = Date.now();
    if (lastAccess && now - lastAccess < ACCESS_THROTTLE_MS) return;

    const next = new Headers(response.headers);
    next.set(LAST_ACCESS_HEADER, now.toString());
    const refreshed = new Response(response.clone().body, {
      status: response.status, statusText: response.statusText, headers: next,
    });
    await cache.put(request, refreshed);
  } catch {
    // best-effort
  }
}

export async function requestPart(
  e: FetchEvent,
  params: { url: string; start: number; end: number },
): Promise<PartInfo | undefined> {
  const isDownload = params.url.includes('/download/');
  const client = await (isDownload ? getClientForRequest(params.url) : self.clients.get(e.clientId));
  if (!client) {
    return undefined;
  }

  const messageId = generateUniqueId();
  const requestState = {} as RequestStates;

  let isResolved = false;
  const promise = Promise.race([
    pause(PART_TIMEOUT).then(() => (isResolved ? undefined : Promise.reject(new Error('ERROR_PART_TIMEOUT')))),
    new Promise<PartInfo>((resolve, reject) => {
      Object.assign(requestState, { resolve, reject });
    }),
  ]);

  requestStates.set(messageId, requestState);
  promise
    .catch(() => undefined)
    .finally(() => {
      requestStates.delete(messageId);
      isResolved = true;
    });

  client.postMessage({
    type: 'requestPart',
    messageId,
    params,
  });

  return promise;
}

async function getClientForRequest(url: string) {
  const urlAccountSlot = getAccountSlot(url);
  const clients = await self.clients.matchAll();
  return clients.find((c) => (
    c.type === 'window' && c.frameType === 'top-level' && getAccountSlot(c.url) === urlAccountSlot
  ));
}

self.addEventListener('message', (e) => {
  const { type, messageId, result } = e.data as {
    type: string;
    messageId: string;
    result: PartInfo;
  };

  if (type === 'partResponse') {
    const requestState = requestStates.get(messageId);
    if (requestState) {
      requestState.resolve(result);
    }
  }
});
