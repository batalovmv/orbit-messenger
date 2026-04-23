#!/usr/bin/env node
// Generate .opencode/agents-vibecode/ from .opencode/agents-semax/.
// Transforms:
//   SEMAX/gpt-5.4   → VIBECODE_GPT/gpt-5.4-xhigh   (check-roles get cross-family via direct GPT provider)
//   SEMAX/<rest>    → VIBECODE_CLAUDE/<rest>       (writer/orchestrator agents stay on Claude)
import { readdirSync, readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const opencodeDir = dirname(here);
const src = join(opencodeDir, 'agents-semax');
const dst = join(opencodeDir, 'agents-vibecode');
mkdirSync(dst, { recursive: true });

for (const f of readdirSync(src)) {
  if (!f.endsWith('.md')) continue;
  let body = readFileSync(join(src, f), 'utf8');
  body = body.replace(/SEMAX\/gpt-5\.4\b/g, 'VIBECODE_GPT/gpt-5.4-xhigh');
  body = body.replace(/SEMAX\//g, 'VIBECODE_CLAUDE/');
  writeFileSync(join(dst, f), body);
  console.log('wrote', f);
}
