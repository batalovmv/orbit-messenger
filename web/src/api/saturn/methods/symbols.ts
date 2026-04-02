import type {
  ApiSticker,
  ApiStickerSet,
  ApiStickerSetInfo,
  ApiVideo,
} from '../../types';
import type {
  SaturnSavedGIF,
  SaturnSticker,
  SaturnStickerPack,
  SaturnTenorGIF,
} from '../types';

import { buildCollectionByKey } from '../../../util/iteratees';
import {
  buildApiGif,
  buildApiSticker,
  buildApiStickerSet,
  getRegisteredAsset,
} from '../apiBuilders/symbols';
import * as client from '../client';
import { sendApiUpdate } from '../updates/apiUpdateEmitter';

const FAVORITE_STICKERS_STORAGE_KEY = 'orbit.favorite-stickers';

function getGlobalState() {
  return (window as any).getGlobal?.();
}

function getKnownStickerMaps() {
  const global = getGlobalState();
  const stickerSetsById = global?.stickers?.setsById || {};
  const customEmojisById = global?.customEmojis?.byId || {};
  const favorite = global?.stickers?.favorite?.stickers || [];
  const recent = global?.stickers?.recent?.stickers || [];

  const stickers = Object.values(stickerSetsById)
    .flatMap((set) => ((set as ApiStickerSet | undefined)?.stickers || []))
    .concat(Object.values(customEmojisById), favorite, recent)
    .filter(Boolean);

  return buildCollectionByKey(stickers, 'id') as Record<string, ApiSticker>;
}

function getLocalFavoriteStickerIds() {
  try {
    const raw = window.localStorage?.getItem(FAVORITE_STICKERS_STORAGE_KEY);
    if (!raw) return [] as string[];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed.filter((id): id is string => typeof id === 'string') : [];
  } catch {
    return [];
  }
}

function setLocalFavoriteStickerIds(ids: string[]) {
  window.localStorage?.setItem(FAVORITE_STICKERS_STORAGE_KEY, JSON.stringify(ids));
}

function getStickerSetId(stickerSetInfo: ApiStickerSetInfo) {
  if ('id' in stickerSetInfo) {
    return stickerSetInfo.id;
  }
  return undefined;
}

async function resolvePackByShortName(shortName: string) {
  const search = await client.request<SaturnStickerPack[]>(
    'GET',
    `/stickers/search?q=${encodeURIComponent(shortName)}&limit=20`,
  );

  return (search || []).find((pack) => pack.short_name === shortName);
}

async function loadPack(stickerSetInfo: ApiStickerSetInfo) {
  if ('isMissing' in stickerSetInfo) {
    return undefined;
  }

  const setId = getStickerSetId(stickerSetInfo)
    || ('shortName' in stickerSetInfo ? (await resolvePackByShortName(stickerSetInfo.shortName))?.id : undefined);
  if (!setId) {
    return undefined;
  }

  return client.request<SaturnStickerPack>('GET', `/stickers/sets/${setId}`);
}

function buildEmojiSets(packs: SaturnStickerPack[]) {
  return packs.map((pack) => buildApiStickerSet(pack, { isEmoji: true, isCustomEmoji: true }));
}

async function listInstalledStickerPacks() {
  return client.request<SaturnStickerPack[]>('GET', '/stickers/installed');
}

async function listFeaturedStickerPacks(limit = 50) {
  return client.request<SaturnStickerPack[]>('GET', `/stickers/featured?limit=${limit}`);
}

async function listSavedGifsRaw() {
  return client.request<SaturnSavedGIF[]>('GET', '/gifs/saved?limit=200');
}

function buildSavedGifHash() {
  return 'saturn-saved-gifs-v1';
}

export async function fetchStickerSets({ hash }: { hash?: string }) {
  const packs = (await listInstalledStickerPacks()) || [];
  return {
    hash: hash || 'saturn-installed-stickers-v1',
    sets: packs.map((pack) => buildApiStickerSet(pack)),
  };
}

export async function fetchRecentStickers({ hash }: { hash?: string }) {
  const stickers = (await client.request<SaturnSticker[]>('GET', '/stickers/recent?limit=30')) || [];

  return {
    hash: hash || 'saturn-recent-stickers-v1',
    stickers: stickers.map((sticker) => buildApiSticker(sticker)),
  };
}

export function fetchFavoriteStickers({ hash }: { hash?: string }) {
  const stickerById = getKnownStickerMaps();
  const stickers = getLocalFavoriteStickerIds()
    .map((id) => stickerById[id])
    .filter(Boolean);

  return Promise.resolve({
    hash: hash || 'orbit-local-favorite-stickers-v1',
    stickers,
  });
}

export async function fetchFeaturedStickers({ hash }: { hash?: string }) {
  const packs = (await listFeaturedStickerPacks()) || [];

  return {
    hash: hash || 'saturn-featured-stickers-v1',
    sets: packs.map((pack) => buildApiStickerSet(pack)),
  };
}

export async function searchStickers({ query, hash }: { query: string; hash?: string }) {
  const packs = (await client.request<SaturnStickerPack[]>(
    'GET',
    `/stickers/search?q=${encodeURIComponent(query)}&limit=50`,
  )) || [];

  return {
    hash: hash || `saturn-search-stickers:${query}`,
    sets: packs.map((pack) => buildApiStickerSet(pack)),
  };
}

export async function fetchStickers({ stickerSetInfo }: { stickerSetInfo: ApiStickerSetInfo }) {
  const pack = await loadPack(stickerSetInfo);
  if (!pack) {
    return undefined;
  }

  const set = buildApiStickerSet(pack);
  return {
    set,
    stickers: set.stickers || [],
    packs: set.packs || {},
  };
}

export async function installStickerSet({ stickerSetId }: { stickerSetId: string; accessHash?: string }) {
  await client.request('POST', `/stickers/sets/${stickerSetId}/install`);
  sendApiUpdate({
    '@type': 'updateStickerSet',
    id: stickerSetId,
    stickerSet: { installedDate: Math.floor(Date.now() / 1000), isArchived: undefined },
  });
  return true;
}

export async function uninstallStickerSet({ stickerSetId }: { stickerSetId: string; accessHash?: string }) {
  await client.request('DELETE', `/stickers/sets/${stickerSetId}/install`);
  sendApiUpdate({
    '@type': 'updateStickerSet',
    id: stickerSetId,
    stickerSet: { installedDate: undefined, isArchived: true },
  });
  return true;
}

export async function addRecentSticker({ sticker }: { sticker: ApiSticker }) {
  await client.request('POST', '/stickers/recent', { sticker_id: sticker.id });
  sendApiUpdate({ '@type': 'updateRecentStickers' });
  return true;
}

export async function removeRecentSticker({ sticker }: { sticker: ApiSticker }) {
  await client.request('DELETE', `/stickers/recent/${sticker.id}`);
  sendApiUpdate({ '@type': 'updateRecentStickers' });
  return true;
}

export async function clearRecentStickers() {
  await client.request('DELETE', '/stickers/recent');
  sendApiUpdate({ '@type': 'updateRecentStickers' });
  return true;
}

export function addFavoriteSticker({ sticker }: { sticker: ApiSticker }) {
  const ids = getLocalFavoriteStickerIds();
  if (!ids.includes(sticker.id)) {
    setLocalFavoriteStickerIds([sticker.id, ...ids]);
  }
  sendApiUpdate({ '@type': 'updateFavoriteStickers' });
  return Promise.resolve(true);
}

export function removeFavoriteSticker({ sticker }: { sticker: ApiSticker }) {
  const ids = getLocalFavoriteStickerIds().filter((id) => id !== sticker.id);
  setLocalFavoriteStickerIds(ids);
  sendApiUpdate({ '@type': 'updateFavoriteStickers' });
  return Promise.resolve(true);
}

export async function faveSticker({
  sticker,
  unfave,
}: {
  sticker: ApiSticker;
  unfave?: boolean;
}) {
  return unfave
    ? removeFavoriteSticker({ sticker })
    : addFavoriteSticker({ sticker });
}

export function fetchCustomEmoji({ documentId }: { documentId: string[] }) {
  const stickerById = getKnownStickerMaps();
  return Promise.resolve(documentId
    .map((id) => stickerById[id])
    .filter((sticker): sticker is ApiSticker => Boolean(sticker))
    .map((sticker) => ({
      ...sticker,
      isCustomEmoji: true as const,
    })));
}

export async function fetchCustomEmojiSets({ hash }: { hash?: string }) {
  const packs = (await listInstalledStickerPacks()) || [];
  return {
    hash: hash || 'saturn-custom-emoji-sets-v1',
    sets: buildEmojiSets(packs),
  };
}

export async function fetchFeaturedEmojiStickers({ hash }: { hash?: string }) {
  const packs = (await listFeaturedStickerPacks(20)) || [];
  return {
    hash: hash || 'saturn-featured-emoji-stickers-v1',
    sets: buildEmojiSets(packs),
  };
}

export async function fetchAnimatedEmojis() {
  const result = await fetchCustomEmojiSets({});
  const first = result?.sets[0];
  if (!first) return undefined;

  return {
    set: first,
    stickers: first.stickers || [],
  };
}

export async function fetchAnimatedEmojiEffects() {
  return fetchAnimatedEmojis();
}

export function fetchStickersForEmoji({ emoji, hash }: { emoji: string; hash?: string }) {
  const stickers = Object.values(getKnownStickerMaps()).filter((sticker) => sticker.emoji === emoji);

  return Promise.resolve({
    hash: hash || `saturn-stickers-for-emoji:${emoji}`,
    stickers,
  });
}

export async function fetchSavedGifs({ hash }: { hash?: string }) {
  const gifs = (await listSavedGifsRaw()) || [];
  return {
    hash: hash || buildSavedGifHash(),
    gifs: gifs.map((gif) => buildApiGif(gif)),
  };
}

export async function saveGif({ gif, shouldUnsave }: { gif: ApiVideo; shouldUnsave?: boolean }) {
  if (shouldUnsave) {
    return removeGif({ gif });
  }

  const rawId = gif.id.startsWith('tenor_') ? gif.id.slice('tenor_'.length) : gif.id;
  const asset = getRegisteredAsset(gif.id, 'document');
  const url = asset?.fullUrl;
  const previewUrl = asset?.previewUrl
    || (gif.thumbnail?.dataUri?.startsWith('data:') ? undefined : gif.thumbnail?.dataUri);

  if (!url) {
    return undefined;
  }

  await client.request('POST', '/gifs/saved', {
    tenor_id: rawId,
    url,
    preview_url: previewUrl,
    width: gif.width,
    height: gif.height,
  });
  sendApiUpdate({ '@type': 'updateSavedGifs' });
  return true;
}

export async function removeGif({ gif }: { gif: ApiVideo }) {
  const gifId = gif.id;
  let deleteId = gifId;

  const uuidLike = /^[0-9a-f-]{36}$/i.test(gifId);
  if (!uuidLike) {
    const saved = (await listSavedGifsRaw()) || [];
    const rawTenorId = gifId.startsWith('tenor_') ? gifId.slice('tenor_'.length) : gifId;
    const matched = saved.find((item) => item.tenor_id === rawTenorId || item.url === gifId);
    if (!matched) {
      sendApiUpdate({ '@type': 'updateSavedGifs' });
      return true;
    }
    deleteId = matched.id;
  }

  await client.request('DELETE', `/gifs/saved/${deleteId}`);
  sendApiUpdate({ '@type': 'updateSavedGifs' });
  return true;
}

export async function fetchGifs({ offset }: { offset?: string; username?: string }) {
  const params = new URLSearchParams({ limit: '24' });
  if (offset) params.set('pos', offset);

  const result = await client.request<{ data: SaturnTenorGIF[]; next_pos?: string }>(
    'GET',
    `/gifs/trending?${params.toString()}`,
  );

  return {
    nextOffset: result.next_pos,
    gifs: (result.data || []).map((gif) => buildApiGif(gif)),
  };
}

export async function searchGifs({
  query,
  offset,
}: {
  query: string;
  offset?: string;
  username?: string;
}) {
  const params = new URLSearchParams({
    q: query,
    limit: '24',
  });
  if (offset) params.set('pos', offset);

  const result = await client.request<{ data: SaturnTenorGIF[]; next_pos?: string }>(
    'GET',
    `/gifs/search?${params.toString()}`,
  );

  return {
    nextOffset: result.next_pos,
    gifs: (result.data || []).map((gif) => buildApiGif(gif)),
  };
}
