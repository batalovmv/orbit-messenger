import type { FC } from '../../../lib/teact/teact';
import {
  memo, useEffect, useMemo, useState,
} from '../../../lib/teact/teact';
import { getActions, withGlobal } from '../../../global';

import type { SharedSettings } from '../../../global/types';
import type { AccountSettings } from '../../../types';
import { SettingsScreens } from '../../../types';

import type { ApiLanguage } from '../../../api/types';

import { selectSharedSettings } from '../../../global/selectors/sharedState';
import { IS_TRANSLATION_SUPPORTED } from '../../../util/browser/windowEnvironment';
import { SUPPORTED_LANGUAGES } from '../../../util/data/readFallbackStrings';
import { loadAndChangeLanguage } from '../../../util/localization';
import { getUserSettings, updateUserSettings } from '../../../api/saturn/methods/settingsApi';

import useFlag from '../../../hooks/useFlag';
import useHistoryBack from '../../../hooks/useHistoryBack';
import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import ItemPicker, { type ItemPickerOption } from '../../common/pickers/ItemPicker';
import Checkbox from '../../ui/Checkbox';
import ListItem from '../../ui/ListItem';
import Loading from '../../ui/Loading';

type OwnProps = {
  isActive?: boolean;
  onReset: () => void;
};

type StateProps = {
  defaultTranslateLang?: string;
  theme: SharedSettings['theme'];
  shouldUseSystemTheme: SharedSettings['shouldUseSystemTheme'];
  messageTextSize: SharedSettings['messageTextSize'];
  messageSendKeyCombo: SharedSettings['messageSendKeyCombo'];
} & Pick<AccountSettings, 'canTranslate' | 'canTranslateChats' | 'doNotTranslate'>
& Pick<SharedSettings, 'language' | 'languages'>;

const ORBIT_TRANSLATE_LANGS = [
  { code: 'en', nameKey: 'AiTranslateLangEn' },
  { code: 'ru', nameKey: 'AiTranslateLangRu' },
  { code: 'es', nameKey: 'AiTranslateLangEs' },
  { code: 'de', nameKey: 'AiTranslateLangDe' },
  { code: 'fr', nameKey: 'AiTranslateLangFr' },
] as const;

const SettingsLanguage: FC<OwnProps & StateProps> = ({
  isActive,
  languages,
  language,
  canTranslate,
  canTranslateChats,
  doNotTranslate,
  defaultTranslateLang,
  theme,
  shouldUseSystemTheme,
  messageTextSize,
  messageSendKeyCombo,
  onReset,
}) => {
  const {
    loadLanguages,
    setSettingOption,
    setSharedSettingOption,
    openSettingsScreen,
  } = getActions();

  const [selectedLanguage, setSelectedLanguage] = useState<string>(language);
  const [isLoading, markIsLoading, unmarkIsLoading] = useFlag();
  const [translateDefaultLang, setTranslateDefaultLang] = useState<string>(defaultTranslateLang || 'auto');
  const lang = useLang();

  const canTranslateChatsEnabled = canTranslateChats;

  // Synchronous fallback to the bundled list. Previously this relied on the
  // `loadLanguages` action populating SharedState, but on fresh sessions
  // where the SharedWorker hadn't synced yet the picker stayed stuck on a
  // spinner (audit 2026-04-27). The bundled list is small (en/ru) and
  // always available — render it immediately, let `loadLanguages` enrich
  // string counts later if the action handler does run.
  useEffect(() => {
    if (!languages?.length) {
      loadLanguages();
    }
  }, [languages]);

  const effectiveLanguages = useMemo<ApiLanguage[]>(() => {
    if (languages?.length) return languages;
    return SUPPORTED_LANGUAGES.map((l) => ({ ...l }));
  }, [languages]);

  useEffect(() => {
    setTranslateDefaultLang(defaultTranslateLang || 'auto');
  }, [defaultTranslateLang]);

  useEffect(() => {
    let isUnmounted = false;

    void (async () => {
      const userSettings = await getUserSettings();
      if (isUnmounted || !userSettings) {
        return;
      }

      setSettingOption({
        defaultTranslateLang: userSettings.default_translate_lang || undefined,
        canTranslate: Boolean(userSettings.can_translate),
        canTranslateChats: Boolean(userSettings.can_translate_chats),
      });
      setTranslateDefaultLang(userSettings.default_translate_lang || 'auto');
    })();

    return () => {
      isUnmounted = true;
    };
  }, []);

  const handleChange = useLastCallback(async (langCode: string) => {
    setSelectedLanguage(langCode);
    markIsLoading();

    await loadAndChangeLanguage(langCode);
    setSharedSettingOption({ language: langCode });
    unmarkIsLoading();
  });

  const options = useMemo(() => {
    if (!effectiveLanguages?.length) return undefined;
    const currentLangCode = (window.navigator.language || 'en').toLowerCase();
    const shortLangCode = currentLangCode.substr(0, 2);

    return effectiveLanguages.map(({ langCode, nativeName, name }) => ({
      value: langCode,
      label: nativeName,
      subLabel: name,
      isLoading: langCode === selectedLanguage && isLoading,
    } satisfies ItemPickerOption)).sort((a) => {
      return currentLangCode && (a.value === currentLangCode || a.value === shortLangCode) ? -1 : 0;
    });
  }, [isLoading, effectiveLanguages, selectedLanguage]);

  const persistTranslatePrefs = useLastCallback(async (overrides: {
    canTranslate?: boolean;
    canTranslateChats?: boolean;
  }) => {
    const result = await updateUserSettings({
      theme: shouldUseSystemTheme ? 'auto' : theme,
      language,
      fontSize: messageTextSize,
      sendByEnter: messageSendKeyCombo === 'enter',
      defaultTranslateLang: translateDefaultLang === 'auto' ? undefined : translateDefaultLang,
      canTranslate: overrides.canTranslate,
      canTranslateChats: overrides.canTranslateChats,
    });
    return Boolean(result);
  });

  const handleShouldTranslateChange = useLastCallback(async (newValue: boolean) => {
    setSettingOption({ canTranslate: newValue });
    const ok = await persistTranslatePrefs({ canTranslate: newValue });
    if (!ok) {
      setSettingOption({ canTranslate: !newValue });
    }
  });

  const handleShouldTranslateChatsChange = useLastCallback(async (newValue: boolean) => {
    setSettingOption({ canTranslateChats: newValue });
    const ok = await persistTranslatePrefs({ canTranslateChats: newValue });
    if (!ok) {
      setSettingOption({ canTranslateChats: !newValue });
    }
  });

  const doNotTranslateText = useMemo(() => {
    if (!IS_TRANSLATION_SUPPORTED || !doNotTranslate.length) {
      return undefined;
    }

    if (doNotTranslate.length === 1) {
      const originalNames = new Intl.DisplayNames([language], { type: 'language' });
      return originalNames.of(doNotTranslate[0])!;
    }

    // @ts-expect-error TODO(phase-8D-cleanup): Languages lang signature mismatch
    const languagesLabel = lang('Languages', doNotTranslate.length);

    return languagesLabel === 'Languages'
      ? `${doNotTranslate.length} languages`
      : languagesLabel;
  }, [doNotTranslate, lang, language]);

  const handleDoNotSelectOpen = useLastCallback(() => {
    openSettingsScreen({ screen: SettingsScreens.DoNotTranslate });
  });

  const translateDefaultOptions = useMemo<ItemPickerOption[]>(() => [
    { value: 'auto', label: lang('AiTranslateDefaultAuto') },
    ...ORBIT_TRANSLATE_LANGS.map(({ code, nameKey }) => ({
      value: code,
      label: lang(nameKey),
    } satisfies ItemPickerOption)),
  ], [lang]);

  const handleTranslateDefaultChange = useLastCallback(async (value: string) => {
    setTranslateDefaultLang(value);

    const nextDefaultTranslateLang = value === 'auto' ? undefined : value;
    const result = await updateUserSettings({
      theme: shouldUseSystemTheme ? 'auto' : theme,
      language,
      fontSize: messageTextSize,
      sendByEnter: messageSendKeyCombo === 'enter',
      defaultTranslateLang: nextDefaultTranslateLang,
    });

    if (!result) {
      setTranslateDefaultLang(defaultTranslateLang || 'auto');
      return;
    }

    setSettingOption({
      defaultTranslateLang: result.default_translate_lang || undefined,
    });
    setTranslateDefaultLang(result.default_translate_lang || 'auto');
  });

  useHistoryBack({
    isActive,
    onBack: onReset,
  });

  return (
    <div className="settings-content settings-language custom-scroll">
      {IS_TRANSLATION_SUPPORTED && (
        <div className="settings-item">
          <Checkbox
            label={lang('ShowTranslateButton')}
            checked={canTranslate}
            onCheck={handleShouldTranslateChange}
          />
          <Checkbox
            label={lang('ShowTranslateChatButton')}
            checked={canTranslateChatsEnabled}
            onCheck={handleShouldTranslateChatsChange}
          />
          {(canTranslate || canTranslateChatsEnabled) && (
            <ListItem
              narrow
              onClick={handleDoNotSelectOpen}
            >
              {lang('DoNotTranslate')}
              <span className="settings-item__current-value">{doNotTranslateText}</span>
            </ListItem>
          )}
          <p className="settings-item-description mb-0 mt-1">
            {/* @ts-expect-error TODO(phase-8D-cleanup): missing lang key lng_translate_settings_about */}
            {lang('lng_translate_settings_about')}
          </p>
        </div>
      )}
      <div className="settings-item settings-item-picker">
        <h4 className="settings-item-header">{lang('AiTranslateDefaultTitle')}</h4>
        <ItemPicker
          items={translateDefaultOptions}
          selectedValue={translateDefaultLang}
          forceRenderAllItems
          onSelectedValueChange={handleTranslateDefaultChange}
          itemInputType="radio"
          className="settings-picker"
        />
        <p className="settings-item-description mb-0 mt-1">{lang('AiTranslateDefaultAbout')}</p>
      </div>
      <div className="settings-item settings-item-picker">
        <h4 className="settings-item-header">
          {lang('Localization.InterfaceLanguage')}
        </h4>
        {options ? (
          <ItemPicker
            items={options}
            selectedValue={selectedLanguage}
            forceRenderAllItems
            onSelectedValueChange={handleChange}
            itemInputType="radio"
            className="settings-picker"
          />
        ) : (
          <Loading />
        )}
      </div>
    </div>
  );
};

export default memo(withGlobal<OwnProps>(
  (global): Complete<StateProps> => {
    const {
      canTranslate, canTranslateChats, doNotTranslate, defaultTranslateLang,
    } = global.settings.byKey;
    const {
      language,
      languages,
      theme,
      shouldUseSystemTheme,
      messageTextSize,
      messageSendKeyCombo,
    } = selectSharedSettings(global);

    return {
      languages,
      language,
      canTranslate,
      canTranslateChats,
      doNotTranslate,
      defaultTranslateLang,
      theme,
      shouldUseSystemTheme,
      messageTextSize,
      messageSendKeyCombo,
    };
  },
)(SettingsLanguage));
