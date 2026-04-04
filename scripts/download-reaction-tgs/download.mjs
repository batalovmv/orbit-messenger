#!/usr/bin/env node
/**
 * Download Telegram reaction TGS animations via Bot API.
 *
 * Telegram stores reaction animations across multiple internal sticker sets:
 * - "EmojiCenterAnimations"  — small looping center icon on the reaction counter
 * - "EmojiAroundAnimations"  — burst/explosion effect around the reaction
 * - "EmojiAppearAnimations"  — bounce-in animation when reaction appears in picker
 * - "EmojiShortAnimations"   — short hover-loop animation in the picker (select)
 * - "EmojiAnimations"        — large particle effect played above the message
 * - "AnimatedEmojies"        — full-size animated emoji (activate animation)
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

// Each animation type maps to a specific Telegram sticker set
const ANIMATION_SETS = {
  center:   'EmojiCenterAnimations',   // small looping icon on reaction counter
  around:   'EmojiAroundAnimations',   // burst effect around the reaction
  appear:   'EmojiAppearAnimations',   // appear animation in picker
  select:   'EmojiShortAnimations',    // hover-loop in picker
  effect:   'EmojiAnimations',         // large particle effect above message
  activate: 'AnimatedEmojies',         // full-size animation on click
};

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

function normalizeEmoji(emoji) {
  return emoji.replace(/\uFE0F/g, '');
}

function matchEmoji(stickerEmoji, targetEmoji) {
  const a = normalizeEmoji(stickerEmoji);
  const b = normalizeEmoji(targetEmoji);
  return a === b || a.includes(b) || b.includes(a);
}

async function main() {
  await mkdir(OUTPUT_DIR, { recursive: true });

  // Initialize manifest structure
  const manifest = {};
  REACTION_EMOJIS.forEach((e) => {
    manifest[e] = { center: null, around: null, appear: null, select: null, effect: null, activate: null };
  });

  let totalDownloaded = 0;

  for (const [type, setName] of Object.entries(ANIMATION_SETS)) {
    console.log(`\n━━━ Fetching "${setName}" for [${type}] ━━━`);
    let set;
    try {
      set = await botApi('getStickerSet', { name: setName });
      console.log(`  Found ${set.stickers.length} stickers`);
    } catch (err) {
      console.error(`  ✗ Failed: ${err.message}`);
      continue;
    }

    // For each reaction emoji, find the first matching TGS sticker
    for (const targetEmoji of REACTION_EMOJIS) {
      if (manifest[targetEmoji][type]) continue; // already have it

      const sticker = set.stickers.find((s) => {
        if (!s.emoji) return false;
        if (!s.is_animated) return false; // only TGS
        return matchEmoji(s.emoji, targetEmoji);
      });

      if (!sticker) continue;

      const safeId = emojiToSafeId(targetEmoji);
      const fileName = `${safeId}_${type}.tgs`;

      try {
        const data = await downloadFile(sticker.file_id);
        await writeFile(join(OUTPUT_DIR, fileName), data);
        manifest[targetEmoji][type] = fileName;
        totalDownloaded++;
        console.log(`  ✓ ${targetEmoji} ${type} → ${fileName} (${data.length} bytes)`);
      } catch (err) {
        console.error(`  ✗ ${targetEmoji} ${type}: ${err.message}`);
      }

      // Small delay to avoid rate limiting
      await new Promise((r) => setTimeout(r, 50));
    }
  }

  // Save manifest
  const manifestPath = join(OUTPUT_DIR, 'manifest.json');
  await writeFile(manifestPath, JSON.stringify(manifest, null, 2));

  console.log(`\n═══ Summary ═══`);
  console.log(`Downloaded: ${totalDownloaded} TGS files`);
  console.log(`Manifest: ${manifestPath}`);

  // Report coverage
  for (const type of Object.keys(ANIMATION_SETS)) {
    const missing = REACTION_EMOJIS.filter((e) => !manifest[e][type]);
    if (missing.length > 0) {
      console.log(`Missing ${type}: ${missing.join(', ')}`);
    } else {
      console.log(`${type}: ✓ all 22 emojis covered`);
    }
  }
}

main().catch(console.error);
