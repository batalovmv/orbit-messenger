import type { FC } from '../../../lib/teact/teact';
import {
  memo, useCallback, useMemo, useRef,
} from '../../../lib/teact/teact';
import { getActions, withGlobal } from '../../../global';

import type {
  ApiAvailableReaction,
  ApiReaction,
  ApiSticker,
  ApiStickerSet,
} from '../../../api/types';
import type { AccountSettings } from '../../../types';
import { SettingsScreens } from '../../../types';

import { selectCanPlayAnimatedEmojis } from '../../../global/selectors';
import { pick } from '../../../util/iteratees';
import { REM } from '../../common/helpers/mediaDimensions';
import renderText from '../../common/helpers/renderText';

import useHistoryBack from '../../../hooks/useHistoryBack';
import { useIntersectionObserver } from '../../../hooks/useIntersectionObserver';
import useOldLang from '../../../hooks/useOldLang';

import ReactionStaticEmoji from '../../common/reactions/ReactionStaticEmoji';
import StickerSetCard from '../../common/StickerSetCard';
import Checkbox from '../../ui/Checkbox';
import ListItem from '../../ui/ListItem';

const DEFAULT_REACTION_SIZE = 1.5 * REM;

const STICKERS_SCREEN_FALLBACKS = {
  emojiPacks: 'Emoji',
  dynamicPackOrder: 'Dynamic pack order',
  dynamicPackOrderInfo: 'Frequently used sticker packs will move to the top of the list.',
  myStickerSets: 'My sticker sets',
  stickersBotInfo: 'Get more stickers and emoji packs from @stickers.',
} as const;

type OwnProps = {
  isActive?: boolean;
  onReset: () => void;
};

type StateProps =
  Pick<AccountSettings, (
    'shouldSuggestStickers' | 'shouldUpdateStickerSetOrder'
  )> & {
    addedSetIds?: string[];
    customEmojiSetIds?: string[];
    stickerSetsById: Record<string, ApiStickerSet>;
    defaultReaction?: ApiReaction;
    availableReactions?: ApiAvailableReaction[];
    canPlayAnimatedEmojis: boolean;
  };

const SettingsStickers: FC<OwnProps & StateProps> = ({
  isActive,
  addedSetIds,
  customEmojiSetIds,
  stickerSetsById,
  defaultReaction,
  shouldSuggestStickers,
  shouldUpdateStickerSetOrder,
  availableReactions,
  canPlayAnimatedEmojis,
  onReset,
}) => {
  const {
    setSettingOption,
    openStickerSet,
    openSettingsScreen,
  } = getActions();
  const lang = useOldLang();

  const getStickerScreenText = useCallback((key: string, fallback: string) => {
    const translation = lang(key);

    return translation === key ? fallback : translation;
  }, [lang]);

  const dynamicPackOrderLabel = getStickerScreenText(
    'InstalledStickers.DynamicPackOrder',
    STICKERS_SCREEN_FALLBACKS.dynamicPackOrder,
  );
  const emojiPacksLabel = getStickerScreenText(
    'CustomEmoji',
    STICKERS_SCREEN_FALLBACKS.emojiPacks,
  );
  const dynamicPackOrderInfo = getStickerScreenText(
    'InstalledStickers.DynamicPackOrderInfo',
    STICKERS_SCREEN_FALLBACKS.dynamicPackOrderInfo,
  );
  const myStickerSetsLabel = getStickerScreenText(
    'ChooseStickerMyStickerSets',
    STICKERS_SCREEN_FALLBACKS.myStickerSets,
  );
  const stickersBotInfo = getStickerScreenText(
    'StickersBotInfo',
    STICKERS_SCREEN_FALLBACKS.stickersBotInfo,
  );

  const stickerSettingsRef = useRef<HTMLDivElement>();
  const { observe: observeIntersectionForCovers } = useIntersectionObserver({ rootRef: stickerSettingsRef });

  const handleStickerSetClick = useCallback((sticker: ApiSticker) => {
    openStickerSet({
      stickerSetInfo: sticker.stickerSetInfo,
    });
  }, [openStickerSet]);

  const handleSuggestStickerSetOrderChange = useCallback((newValue: boolean) => {
    setSettingOption({ shouldUpdateStickerSetOrder: newValue });
  }, [setSettingOption]);

  const handleSuggestStickersChange = useCallback((newValue: boolean) => {
    setSettingOption({ shouldSuggestStickers: newValue });
  }, [setSettingOption]);

  const stickerSets = useMemo(() => (
    addedSetIds && Object.values(pick(stickerSetsById, addedSetIds))
  ), [addedSetIds, stickerSetsById]);

  useHistoryBack({
    isActive,
    onBack: onReset,
  });

  return (
    <div className="settings-content custom-scroll">
      <div className="settings-item">
        <Checkbox
          label={lang('SuggestStickers')}
          checked={shouldSuggestStickers}
          onCheck={handleSuggestStickersChange}
        />
        <ListItem
          narrow

          onClick={() => openSettingsScreen({ screen: SettingsScreens.CustomEmoji })}
          icon="smile"
        >
          {emojiPacksLabel}
          {customEmojiSetIds && <span className="settings-item__current-value">{customEmojiSetIds.length}</span>}
        </ListItem>
        {defaultReaction && (
          <ListItem
            className="SettingsDefaultReaction"
            narrow

            onClick={() => openSettingsScreen({ screen: SettingsScreens.QuickReaction })}
          >
            <ReactionStaticEmoji
              reaction={defaultReaction}
              className="current-default-reaction"
              size={DEFAULT_REACTION_SIZE}
              availableReactions={availableReactions}
            />
            <div className="title">{lang('DoubleTapSetting')}</div>
          </ListItem>
        )}
      </div>
      <div className="settings-item">
        <h4 className="settings-item-header" dir={lang.isRtl ? 'rtl' : undefined}>
          {dynamicPackOrderLabel}
        </h4>
        <Checkbox
          label={dynamicPackOrderLabel}
          checked={shouldUpdateStickerSetOrder}
          onCheck={handleSuggestStickerSetOrderChange}
        />
        <p className="settings-item-description mt-3" dir="auto">
          {dynamicPackOrderInfo}
        </p>
      </div>
      {stickerSets && (
        <div className="settings-item">
          <h4 className="settings-item-header" dir={lang.isRtl ? 'rtl' : undefined}>
            {myStickerSetsLabel}
          </h4>
          <div ref={stickerSettingsRef}>
            {stickerSets.map((stickerSet: ApiStickerSet) => (
              <StickerSetCard
                key={stickerSet.id}
                stickerSet={stickerSet}
                observeIntersection={observeIntersectionForCovers}
                onClick={handleStickerSetClick}
                noPlay={!canPlayAnimatedEmojis}
              />
            ))}
          </div>
          <p className="settings-item-description mt-3" dir="auto">
            {renderText(stickersBotInfo, ['links'])}
          </p>
        </div>
      )}
    </div>
  );
};

export default memo(withGlobal<OwnProps>(
  (global): Complete<StateProps> => {
    return {
      ...pick(global.settings.byKey, [
        'shouldSuggestStickers',
        'shouldUpdateStickerSetOrder',
      ]),
      addedSetIds: global.stickers.added.setIds,
      customEmojiSetIds: global.customEmojis.added.setIds,
      stickerSetsById: global.stickers.setsById,
      defaultReaction: global.config?.defaultReaction,
      availableReactions: global.reactions.availableReactions,
      canPlayAnimatedEmojis: selectCanPlayAnimatedEmojis(global),
    };
  },
)(SettingsStickers));
