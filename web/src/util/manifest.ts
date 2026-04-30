const MANIFEST_BY_LANG = {
  en: './site.webmanifest',
  ru: './site_ru.webmanifest',
};

const DEV_MANIFEST_BY_LANG = {
  en: './site_dev.webmanifest',
  ru: './site_dev_ru.webmanifest',
};

export function syncManifestWithLanguage(langCode?: string) {
  const link = document.getElementById('the-manifest-placeholder') as HTMLLinkElement | null;
  if (!link) return;

  const normalizedLang = langCode?.toLowerCase().startsWith('ru') ? 'ru' : 'en';
  const isDevManifest = link.getAttribute('href')?.includes('_dev');
  const nextHref = isDevManifest ? DEV_MANIFEST_BY_LANG[normalizedLang] : MANIFEST_BY_LANG[normalizedLang];

  if (link.getAttribute('href') !== nextHref) {
    link.setAttribute('href', nextHref);
  }

  document.documentElement.lang = normalizedLang;
}
