#!/usr/bin/env node
/**
 * Download Telegram reaction TGS animations via Bot API.
 *
 * Telegram stores reaction animations in internal sticker sets:
 * - "AnimatedEmojies" — animated emoji center icons
 * - "EmojiAnimations" — appear/effect animations
 *
 * Usage: node download.mjs
 */

import { writeFile, mkdir } from 'node:fs/promises';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));

const BOT_TOKEN = '8503568123:AAFiuobI5EtaiOU1hIejrMM7Ep8WP8XkKes';
const API_BASE = `https://api.telegram.org/bot${BOT_TOKEN}`;
const FILE_BASE = `https://api.telegram.org/file/bot${BOT_TOKEN}`;

const OUTPUT_DIR = join(__dirname, '..', '..', 'web', 'src', 'assets', 'tgs', 'reactions');

// The 22 default reaction emojis used in Orbit
const REACTION_EMOJIS = [
  '❤️', '👍', '👎', '🔥', '🥰', '👏', '😁', '🎉',
  '🤔', '😢', '😡', '👀', '🤯', '🤝', '🙏', '👌',
  '💯', '🤣', '😎', '🤩', '💔', '✅',
];

// Known Telegram internal sticker sets with emoji animations
const STICKER_SET_NAMES = [
  'AnimatedEmojies',
  'EmojiAnimations',
];

async function botApi(method, params = {}) {
  const url = new URL(`${API_BASE}/${method}`);
  Object.entries(params).forEach(([k, v]) => url.searchParams.set(k, v));
  const res = await fetch(url);
  const data = await res.json();
  if (!data.ok) {
    throw new Error(`Bot API ${method} failed: ${data.description}`);
  }
  return data.result;
}

async function downloadFile(fileId) {
  const fileInfo = await botApi('getFile', { file_id: fileId });
  const url = `${FILE_BASE}/${fileInfo.file_path}`;
  const res = await fetch(url);
  if (!res.ok) throw new Error(`Failed to download ${url}: ${res.status}`);
  return Buffer.from(await res.arrayBuffer());
}

function emojiToSafeId(emoji) {
  return Array.from(emoji)
    .map((char) => char.codePointAt(0).toString(16))
    .join('_');
}

async function main() {
  await mkdir(OUTPUT_DIR, { recursive: true });

  const emojiToFiles = new Map();
  REACTION_EMOJIS.forEach((e) => emojiToFiles.set(e, { center: null, effect: null }));

  // Try to get sticker sets
  for (const setName of STICKER_SET_NAMES) {
    console.log(`\nFetching sticker set: ${setName}`);
    try {
      const set = await botApi('getStickerSet', { name: setName });
      console.log(`  Found ${set.stickers.length} stickers in "${set.name}" (${set.title})`);

      for (const sticker of set.stickers) {
        const emoji = sticker.emoji;
        if (!emoji) continue;

        // Check if this emoji is one of our reaction emojis
        const matchedEmoji = REACTION_EMOJIS.find((re) => re === emoji || re.includes(emoji) || emoji.includes(re.replace('\uFE0F', '')));
        if (!matchedEmoji) continue;

        const entry = emojiToFiles.get(matchedEmoji);
        if (!entry) continue;

        const type = sticker.is_animated ? 'tgs' : sticker.is_video ? 'webm' : 'webp';
        if (type !== 'tgs') continue; // We only want TGS

        // First TGS match goes to center, second to effect
        if (!entry.center) {
          entry.center = sticker;
          console.log(`  ✓ ${matchedEmoji} center: file_id=${sticker.file_id.slice(0, 20)}...`);
        } else if (!entry.effect) {
          entry.effect = sticker;
          console.log(`  ✓ ${matchedEmoji} effect: file_id=${sticker.file_id.slice(0, 20)}...`);
        }
      }
    } catch (err) {
      console.error(`  ✗ Failed: ${err.message}`);
    }
  }

  // Download all found TGS files
  const manifest = {};
  let downloaded = 0;

  for (const emoji of REACTION_EMOJIS) {
    const entry = emojiToFiles.get(emoji);
    const safeId = emojiToSafeId(emoji);

    manifest[emoji] = { center: null, effect: null };

    if (entry?.center) {
      try {
        const data = await downloadFile(entry.center.file_id);
        const fileName = `${safeId}_center.tgs`;
        await writeFile(join(OUTPUT_DIR, fileName), data);
        manifest[emoji].center = fileName;
        downloaded++;
        console.log(`Downloaded ${emoji} center → ${fileName} (${data.length} bytes)`);
      } catch (err) {
        console.error(`Failed to download ${emoji} center: ${err.message}`);
      }
    }

    if (entry?.effect) {
      try {
        const data = await downloadFile(entry.effect.file_id);
        const fileName = `${safeId}_effect.tgs`;
        await writeFile(join(OUTPUT_DIR, fileName), data);
        manifest[emoji].effect = fileName;
        downloaded++;
        console.log(`Downloaded ${emoji} effect → ${fileName} (${data.length} bytes)`);
      } catch (err) {
        console.error(`Failed to download ${emoji} effect: ${err.message}`);
      }
    }
  }

  // Save manifest
  const manifestPath = join(OUTPUT_DIR, 'manifest.json');
  await writeFile(manifestPath, JSON.stringify(manifest, null, 2));

  console.log(`\n=== Summary ===`);
  console.log(`Downloaded: ${downloaded} TGS files`);
  console.log(`Manifest: ${manifestPath}`);

  // Report missing
  const missing = REACTION_EMOJIS.filter((e) => !manifest[e]?.center);
  if (missing.length > 0) {
    console.log(`\nMissing center animations for: ${missing.join(', ')}`);
  }
}

main().catch(console.error);
