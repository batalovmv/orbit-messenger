import type { FC } from '../../../lib/teact/teact';
import { memo, useEffect, useRef, useState } from '../../../lib/teact/teact';
import { getActions, withGlobal } from '../../../global';

import type { ApiVideo } from '../../../api/types';

import { SLIDE_TRANSITION_DURATION } from '../../../config';
import { selectCurrentMessageList, selectIsChatWithSelf } from '../../../global/selectors';
import { IS_TOUCH_ENV } from '../../../util/browser/windowEnvironment';
import buildClassName from '../../../util/buildClassName';
import { callApi } from '../../../api/saturn';

import { useIntersectionObserver } from '../../../hooks/useIntersectionObserver';
import useLastCallback from '../../../hooks/useLastCallback';
import useOldLang from '../../../hooks/useOldLang';
import useAsyncRendering from '../../right/hooks/useAsyncRendering';

import GifButton from '../../common/GifButton';
import Loading from '../../ui/Loading';
import SearchInput from '../../ui/SearchInput';

import './GifPicker.scss';

type OwnProps = {
  className: string;
  loadAndPlay: boolean;
  canSendGifs?: boolean;
  onGifSelect?: (gif: ApiVideo, isSilent?: boolean, shouldSchedule?: boolean) => void;
};

type StateProps = {
  savedGifs?: ApiVideo[];
  isSavedMessages?: boolean;
};

const INTERSECTION_DEBOUNCE = 300;
const GIF_SEARCH_DEBOUNCE_MS = 250;

const GifPicker: FC<OwnProps & StateProps> = ({
  className,
  loadAndPlay,
  canSendGifs,
  savedGifs,
  isSavedMessages,
  onGifSelect,
}) => {
  const { loadSavedGifs, saveGif } = getActions();

  const contentRef = useRef<HTMLDivElement>();
  const searchRequestIdRef = useRef(0);

  const [query, setQuery] = useState('');
  const [trendingGifs, setTrendingGifs] = useState<ApiVideo[]>();
  const [searchResults, setSearchResults] = useState<ApiVideo[]>();
  const [isTrendingLoading, setIsTrendingLoading] = useState(false);
  const [isSearching, setIsSearching] = useState(false);

  const {
    observe: observeIntersection,
  } = useIntersectionObserver({ rootRef: contentRef, debounceMs: INTERSECTION_DEBOUNCE });

  const canRenderContents = useAsyncRendering([], SLIDE_TRANSITION_DURATION);
  const lang = useOldLang();

  const normalizedQuery = query.trim();
  const isSearchMode = Boolean(normalizedQuery);

  useEffect(() => {
    if (!loadAndPlay) {
      return;
    }

    loadSavedGifs();
  }, [loadAndPlay, loadSavedGifs]);

  useEffect(() => {
    if (!loadAndPlay || trendingGifs !== undefined) {
      return undefined;
    }

    let isCanceled = false;
    setIsTrendingLoading(true);

    void callApi('fetchGifs', {})
      .then((result) => {
        if (isCanceled) return;
        setTrendingGifs(result?.gifs || []);
      })
      .catch(() => {
        if (isCanceled) return;
        setTrendingGifs([]);
      })
      .finally(() => {
        if (!isCanceled) {
          setIsTrendingLoading(false);
        }
      });

    return () => {
      isCanceled = true;
    };
  }, [loadAndPlay, trendingGifs]);

  useEffect(() => {
    searchRequestIdRef.current += 1;
    const requestId = searchRequestIdRef.current;

    if (!loadAndPlay || !normalizedQuery) {
      setSearchResults(undefined);
      setIsSearching(false);
      return undefined;
    }

    setIsSearching(true);

    const timeoutId = window.setTimeout(() => {
      void callApi('searchGifs', { query: normalizedQuery })
        .then((result) => {
          if (searchRequestIdRef.current !== requestId) return;
          setSearchResults(result?.gifs || []);
        })
        .catch(() => {
          if (searchRequestIdRef.current !== requestId) return;
          setSearchResults([]);
        })
        .finally(() => {
          if (searchRequestIdRef.current === requestId) {
            setIsSearching(false);
          }
        });
    }, GIF_SEARCH_DEBOUNCE_MS);

    return () => {
      clearTimeout(timeoutId);
    };
  }, [loadAndPlay, normalizedQuery]);

  const handleUnsaveClick = useLastCallback((gif: ApiVideo) => {
    saveGif({ gif, shouldUnsave: true });
  });

  const handleSearchReset = useLastCallback(() => {
    setQuery('');
    setSearchResults(undefined);
    setIsSearching(false);
  });

  function renderGifGrid(gifs: ApiVideo[], keyPrefix: string, shouldAllowUnsave?: boolean) {
    return (
      <div className="GifPicker-grid">
        {gifs.map((gif) => (
          <GifButton
            key={`${keyPrefix}-${gif.id}`}
            className="GifPicker-gridItem"
            gif={gif}
            observeIntersection={observeIntersection}
            isDisabled={!loadAndPlay}
            onClick={canSendGifs ? onGifSelect : undefined}
            onUnsaveClick={shouldAllowUnsave ? handleUnsaveClick : undefined}
            isSavedMessages={isSavedMessages}
          />
        ))}
      </div>
    );
  }

  function renderSearchResults() {
    if (isSearching && !searchResults) {
      return <Loading color="yellow" />;
    }

    if (!searchResults?.length) {
      return (
        <div className="picker-disabled GifPicker-status">
          {lang('NoGIFsFound')}
        </div>
      );
    }

    return renderGifGrid(searchResults, 'search');
  }

  function renderDefaultSections() {
    return (
      <>
        <section className="GifPicker-section">
          <h4 className="GifPicker-sectionTitle">Trending</h4>
          {trendingGifs?.length ? renderGifGrid(trendingGifs, 'trending') : (
            <div className="GifPicker-sectionFallback">
              {isTrendingLoading ? <Loading color="yellow" /> : undefined}
            </div>
          )}
        </section>
        <section className="GifPicker-section">
          <h4 className="GifPicker-sectionTitle">{lang('SavedGifsLimitTitle')}</h4>
          {savedGifs?.length ? renderGifGrid(savedGifs, 'saved', true) : (
            <div className="picker-disabled GifPicker-status">
              {lang('GifPickerEmpty')}
            </div>
          )}
        </section>
      </>
    );
  }

  return (
    <div
      className={buildClassName('GifPicker', className)}
      dir={lang.isRtl ? 'rtl' : undefined}
    >
      {!canSendGifs ? (
        <div className="picker-disabled GifPicker-status">{lang('GifPickerBlocked')}</div>
      ) : !canRenderContents ? (
        <Loading color="yellow" />
      ) : (
        <>
          <div className="GifPicker-search">
            <SearchInput
              className="GifPicker-searchInput"
              value={query}
              isLoading={isSearching}
              placeholder={lang('SearchGifsTitle')}
              onChange={setQuery}
              onReset={handleSearchReset}
            />
          </div>
          <div
            ref={contentRef}
            className={buildClassName('GifPicker-content', IS_TOUCH_ENV ? 'no-scrollbar' : 'custom-scroll')}
          >
            {isSearchMode ? renderSearchResults() : renderDefaultSections()}
          </div>
        </>
      )}
    </div>
  );
};

export default memo(withGlobal<OwnProps>(
  (global): Complete<StateProps> => {
    const { chatId } = selectCurrentMessageList(global) || {};
    const isSavedMessages = Boolean(chatId) && selectIsChatWithSelf(global, chatId);
    return {
      savedGifs: global.gifs.saved.gifs,
      isSavedMessages,
    };
  },
)(GifPicker));
