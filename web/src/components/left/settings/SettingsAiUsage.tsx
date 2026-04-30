import { memo, useEffect, useState } from '../../../lib/teact/teact';

import type { AiUsageStats } from '../../../api/saturn/methods/ai';
import type { LangPair } from '../../../types/language';

import { formatDateToString } from '../../../util/dates/dateFormat';
import { fetchAiUsage } from '../../../api/saturn/methods/ai';

import useHistoryBack from '../../../hooks/useHistoryBack';
import useLang from '../../../hooks/useLang';

import ListItem from '../../ui/ListItem';
import Loading from '../../ui/Loading';

type OwnProps = {
  isActive?: boolean;
  onReset: NoneToVoidFunction;
};

const ENDPOINT_LABEL_KEYS: Record<string, keyof LangPair> = {
  summarize: 'AiUsageEndpointSummarize',
  translate: 'AiUsageEndpointTranslate',
  transcribe: 'AiUsageEndpointTranscribe',
  'reply-suggest': 'AiUsageEndpointSuggest',
  ask: 'AiUsageEndpointAsk',
  'semantic-search': 'AiUsageEndpointSearch',
};

const SettingsAiUsage = ({ isActive, onReset }: OwnProps) => {
  const lang = useLang();

  const [stats, setStats] = useState<AiUsageStats | undefined>();
  const [isLoading, setIsLoading] = useState(true);
  const [hasError, setHasError] = useState(false);

  useHistoryBack({ isActive, onBack: onReset });

  useEffect(() => {
    let isCancelled = false;

    (async () => {
      try {
        const result = await fetchAiUsage();
        if (isCancelled) return;
        if (!result) {
          setHasError(true);
        } else {
          setStats(result);
        }
      } catch {
        if (!isCancelled) setHasError(true);
      } finally {
        if (!isCancelled) setIsLoading(false);
      }
    })();

    return () => {
      isCancelled = true;
    };
  }, []);

  if (isLoading) {
    return (
      <div className="settings-content custom-scroll">
        <div className="settings-item">
          <Loading />
        </div>
      </div>
    );
  }

  if (hasError || !stats) {
    return (
      <div className="settings-content custom-scroll">
        <div className="settings-item">
          <h4 className="settings-item-header">{lang('AiUsageTitle')}</h4>
          <p className="settings-item-description">{lang('AiUsageUnavailable')}</p>
        </div>
      </div>
    );
  }

  const periodStart = stats.period_start ? new Date(stats.period_start) : undefined;
  const periodLabel = periodStart
    ? lang('AiUsagePeriodSince', { date: formatDateToString(periodStart, lang.code) })
    : lang('AiUsagePeriodFallback');

  const endpointEntries = Object.entries(stats.by_endpoint || {})
    .sort(([, a], [, b]) => b - a);

  const totalCostCents = Object.values(stats.cost_cents || {}).reduce((sum, c) => sum + c, 0);

  const hasActivity = stats.total_requests > 0 || endpointEntries.length > 0;

  return (
    <div className="settings-content custom-scroll">
      <div className="settings-item">
        <h4 className="settings-item-header">{lang('AiUsageTitle')}</h4>
        <p className="settings-item-description">{periodLabel}</p>
      </div>

      {!hasActivity && (
        <div className="settings-item">
          <p className="settings-item-description">{lang('AiUsageEmpty')}</p>
        </div>
      )}

      {hasActivity && (
        <div className="settings-item">
          <h4 className="settings-item-header">{lang('AiUsageTotalsHeader')}</h4>
          <ListItem narrow inactive multiline>
            <span className="title">{lang('AiUsageTotalRequests')}</span>
            <span className="settings-item__current-value">{lang.number(stats.total_requests)}</span>
          </ListItem>
          <ListItem narrow inactive multiline>
            <span className="title">{lang('AiUsageInputTokens')}</span>
            <span className="settings-item__current-value">{lang.number(stats.input_tokens)}</span>
          </ListItem>
          <ListItem narrow inactive multiline>
            <span className="title">{lang('AiUsageOutputTokens')}</span>
            <span className="settings-item__current-value">{lang.number(stats.output_tokens)}</span>
          </ListItem>
          {totalCostCents > 0 && (
            <ListItem narrow inactive multiline>
              <span className="title">{lang('AiUsageCost')}</span>
              <span className="settings-item__current-value">
                {`$${(totalCostCents / 100).toFixed(2)}`}
              </span>
            </ListItem>
          )}
        </div>
      )}

      {endpointEntries.length > 0 && (
        <div className="settings-item">
          <h4 className="settings-item-header">{lang('AiUsageByFeatureHeader')}</h4>
          {endpointEntries.map(([endpoint, count]) => {
            const labelKey = ENDPOINT_LABEL_KEYS[endpoint];
            return (
              <ListItem key={endpoint} narrow inactive multiline>
                <span className="title">{labelKey ? lang(labelKey) : endpoint}</span>
                <span className="settings-item__current-value">{lang.number(count)}</span>
              </ListItem>
            );
          })}
        </div>
      )}
    </div>
  );
};

export default memo(SettingsAiUsage);
