// Settings/localization methods for Saturn

import type { ApiOldLangPack, ApiOldLangString, LangPackStringValuePlural } from '../../types';
import type { LANG_PACKS } from '../../../config';

import readFallbackStrings from '../../../util/data/readFallbackStrings';

export async function fetchLangPack() {
  // Saturn uses built-in localization from the TG Web A bundle
  return undefined;
}

export async function fetchLanguages() {
  return [];
}

export async function fetchLangStrings() {
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
      const plural = value as LangPackStringValuePlural;
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
