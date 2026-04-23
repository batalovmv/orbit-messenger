import type {
  ApiLanguage, CachedLangData, LangPack, LangPackStringValuePlural,
} from '../../api/types';

import enStrings from '../../assets/localization/fallback.strings';
import ruStrings from '../../assets/localization/fallback.ru.strings';
import readStrings from './readStrings';

const DEFAULT_LANG_CODE = 'en';
const FALLBACK_VERSION = 0;
const TRANSLATE_URL_BASE = 'https://translations.telegram.org';

const PLURAL_SUFFIXES = new Set(['zero', 'one', 'two', 'few', 'many', 'other']);

export const SUPPORTED_LANGUAGES: ApiLanguage[] = [
  {
    langCode: 'en',
    name: 'English',
    nativeName: 'English',
    pluralCode: 'en',
    stringsCount: 0,
    translatedCount: 0,
    translationsUrl: `${TRANSLATE_URL_BASE}/en/weba`,
    isOfficial: true,
  },
  {
    langCode: 'ru',
    name: 'Russian',
    nativeName: 'Русский',
    pluralCode: 'ru',
    stringsCount: 0,
    translatedCount: 0,
    translationsUrl: `${TRANSLATE_URL_BASE}/ru/weba`,
    isOfficial: true,
  },
];

const RAW_STRINGS_BY_LANG: Record<string, string | undefined> = {
  en: enStrings,
  ru: ruStrings,
};

function parseStringsFile(fileData: string): LangPack['strings'] {
  const rawStrings = readStrings(fileData);
  const strings: LangPack['strings'] = {};

  Object.entries(rawStrings).forEach(([key, value]) => {
    const lastUnderscore = key.lastIndexOf('_');
    const suffix = lastUnderscore === -1 ? undefined : key.slice(lastUnderscore + 1);
    const isPlural = suffix && PLURAL_SUFFIXES.has(suffix);

    if (!isPlural) {
      strings[key] = value;
      return;
    }

    const clearKey = key.slice(0, lastUnderscore);
    const knownValue = (strings[clearKey] || {}) as LangPackStringValuePlural;
    knownValue[suffix as keyof LangPackStringValuePlural] = value;
    strings[clearKey] = knownValue;
  });

  return strings;
}

async function loadRawStrings(langCode: string, forLocalScript: boolean): Promise<string | undefined> {
  if (forLocalScript) {
    const fs = await import('fs');
    const path = langCode === 'en'
      ? './src/assets/localization/fallback.strings'
      : `./src/assets/localization/fallback.${langCode}.strings`;
    try {
      return fs.readFileSync(path, 'utf8');
    } catch {
      return undefined;
    }
  }

  return RAW_STRINGS_BY_LANG[langCode];
}

export default async function readFallbackStrings(
  forLocalScript?: boolean,
  langCode: string = DEFAULT_LANG_CODE,
): Promise<CachedLangData> {
  const baseData = await loadRawStrings(DEFAULT_LANG_CODE, Boolean(forLocalScript));
  const baseStrings = baseData ? parseStringsFile(baseData) : {};

  let mergedStrings: LangPack['strings'] = baseStrings;

  if (langCode !== DEFAULT_LANG_CODE) {
    const localeData = await loadRawStrings(langCode, Boolean(forLocalScript));
    if (localeData) {
      const localeStrings = parseStringsFile(localeData);
      mergedStrings = { ...baseStrings, ...localeStrings };
    }
  }

  const langPack: LangPack = {
    langCode,
    version: FALLBACK_VERSION,
    strings: mergedStrings,
  };

  const stringsCount = Object.keys(mergedStrings).length;

  const languageMeta = SUPPORTED_LANGUAGES.find((l) => l.langCode === langCode)
    || SUPPORTED_LANGUAGES[0];

  const language: ApiLanguage = {
    ...languageMeta,
    stringsCount,
    translatedCount: stringsCount,
  };

  return {
    langPack,
    language,
  };
}
