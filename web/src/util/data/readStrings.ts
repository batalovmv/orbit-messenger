import { DEBUG } from '../../config';

export default function readStrings(data: string): Record<string, string> {
  const lines = data.split(/;\r?\n?/);
  const result: Record<string, string> = {};
  for (const rawLine of lines) {
    // Strip blank lines and comment-only lines from the *front* of each chunk.
    // The split above keeps the leading "\n" of any blank line that follows a
    // closed entry, so a chunk for the next key looks like
    //   '\n\n// section header\n"NextKey" = "..."'
    // Without trimming, line.startsWith('"') was false and the entry was
    // silently dropped — that bug ate dozens of RU translations whose keys
    // happened to follow a blank line or a comment in fallback.ru.strings
    // (e.g. Notifications, Language, DataSettings, AiUsageTitle, ...).
    let line = rawLine;
    while (line.length > 0 && (line.startsWith('\r') || line.startsWith('\n'))) {
      line = line.slice(1);
    }
    while (line.startsWith('//')) {
      const eol = line.indexOf('\n');
      if (eol === -1) { line = ''; break; }
      line = line.slice(eol + 1);
      while (line.length > 0 && (line.startsWith('\r') || line.startsWith('\n'))) {
        line = line.slice(1);
      }
    }
    if (!line.startsWith('"')) continue;
    const [key, value] = parseLine(line) || [];
    if (!key || !value) {
      // eslint-disable-next-line no-console
      console.warn('Bad formatting in line:', line);
      continue;
    }
    if (result[key]) {
      // eslint-disable-next-line no-console
      console.warn('Duplicate key:', key);
    }
    result[key] = value;
  }
  return result;
}

function parseLine(line: string) {
  let isEscaped = false;
  let isInsideString = false;

  let separatorIndex;
  for (let i = 0; i < line.length; i++) {
    const char = line[i];
    if (char === '\\') {
      isEscaped = !isEscaped;
      continue;
    }

    if (char === '"' && !isEscaped) {
      isInsideString = !isInsideString;
      continue;
    }

    if (char === '=' && !isInsideString) {
      separatorIndex = i;
      break;
    }

    isEscaped = false;
  }

  if (separatorIndex === undefined || separatorIndex === line.length - 1) return undefined;

  try {
    const key = JSON.parse(line.slice(0, separatorIndex));
    const value = JSON.parse(line.slice(separatorIndex + 1));

    return [key, value];
  } catch (e) {
    if (DEBUG) {
      // eslint-disable-next-line no-console
      console.error('Error parsing line:', line, e);
    }
  }

  return undefined;
}
