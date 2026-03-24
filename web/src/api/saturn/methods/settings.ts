// Stubs for settings/localization methods
// TG Web A calls these during init — we provide no-op implementations for Phase 1

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
