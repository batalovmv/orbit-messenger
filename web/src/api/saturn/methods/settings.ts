// Settings/localization methods for Saturn

import type { LANG_PACKS } from '../../../config';
import type { ApiOldLangPack, ApiOldLangString, LangPackStringValuePlural } from '../../types';

import readFallbackStrings from '../../../util/data/readFallbackStrings';

export async function fetchLangPack() {
  const fallbackData = await readFallbackStrings();

  return {
    version: fallbackData.langPack.version,
    strings: fallbackData.langPack.strings,
    keysToRemove: [],
  };
}

export async function fetchLangDifference() {
  return fetchLangPack();
}

export async function fetchLanguages() {
  const fallbackData = await readFallbackStrings();
  return [fallbackData.language];
}

export async function fetchLanguage() {
  const fallbackData = await readFallbackStrings();
  return fallbackData.language;
}

export function fetchLangStrings() {
  return undefined;
}

// Provide bundled fallback strings as the old langpack format.
// oldLangProvider calls this to populate its translation map.
export async function oldFetchLangPack({ sourceLangPacks, langCode }: {
  sourceLangPacks: typeof LANG_PACKS;
  langCode: string;
}): Promise<{ langPack: ApiOldLangPack } | undefined> {
  const fallbackData = await readFallbackStrings();
  const { strings } = fallbackData.langPack;
  const oldPack: ApiOldLangPack = {};

  for (const [key, value] of Object.entries(strings)) {
    if (typeof value === 'string') {
      oldPack[key] = value;
    } else if (typeof value === 'object' && 'isDeleted' in value) {
      // Skip deleted strings
    } else if (typeof value === 'object') {
      const plural: LangPackStringValuePlural = value;
      const oldString: ApiOldLangString = {
        zeroValue: plural.zero,
        oneValue: plural.one,
        twoValue: plural.two,
        fewValue: plural.few,
        manyValue: plural.many,
        otherValue: plural.other,
      };
      oldPack[key] = oldString;
    }
  }

  return { langPack: oldPack };
}
