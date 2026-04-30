// Per-user override for the AI translation target language. Stored in
// localStorage because this is a client-only preference — there's no
// matching backend column today, and Phase 2 explicitly chose not to
// add one. Falls back to the active UI language when the user hasn't
// picked an override (the common case: "I read messages in my UI lang").

const STORAGE_KEY = 'orbit_translate_default_lang';

export type OrbitTranslateLangOption = {
  code: string;
  nameKey: 'AiTranslateLangEn'
    | 'AiTranslateLangRu'
    | 'AiTranslateLangEs'
    | 'AiTranslateLangDe'
    | 'AiTranslateLangFr';
};

export const ORBIT_TRANSLATE_LANGS: OrbitTranslateLangOption[] = [
  { code: 'en', nameKey: 'AiTranslateLangEn' },
  { code: 'ru', nameKey: 'AiTranslateLangRu' },
  { code: 'es', nameKey: 'AiTranslateLangEs' },
  { code: 'de', nameKey: 'AiTranslateLangDe' },
  { code: 'fr', nameKey: 'AiTranslateLangFr' },
];

export function getStoredTranslateLang(): string | undefined {
  try {
    return localStorage.getItem(STORAGE_KEY) || undefined;
  } catch {
    return undefined;
  }
}

export function setStoredTranslateLang(code: string | undefined): void {
  try {
    if (code) {
      localStorage.setItem(STORAGE_KEY, code);
    } else {
      localStorage.removeItem(STORAGE_KEY);
    }
  } catch {
    // Storage disabled — preference simply won't persist this session.
  }
}

// Resolves the target language for an AI translation request.
// Priority: explicit user override → active UI language → 'en'.
export function resolveTranslateLang(uiLangCode: string | undefined): string {
  const stored = getStoredTranslateLang();
  if (stored) return stored;
  const ui = (uiLangCode || '').slice(0, 2).toLowerCase();
  return ui || 'en';
}
