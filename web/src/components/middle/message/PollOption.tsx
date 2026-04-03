import type { FC } from '../../../lib/teact/teact';
import {
  useEffect,
  useState,
} from '../../../lib/teact/teact';

import type { ApiPollAnswer, ApiPollResult } from '../../../api/types';

import buildClassName from '../../../util/buildClassName';
import { renderTextWithEntities } from '../../common/helpers/renderTextWithEntities';

import useLang from '../../../hooks/useLang';

import Icon from '../../common/icons/Icon';

import './PollOption.scss';

type OwnProps = {
  mode?: 'vote' | 'result';
  answer: ApiPollAnswer;
  voteResults?: ApiPollResult[];
  totalVoters?: number;
  correctResults?: string[];
  shouldAnimate?: boolean;
  isMultiple?: boolean;
  isSelected?: boolean;
  disabled?: boolean;
  onClick?: (option: string) => void;
};

const PollOption: FC<OwnProps> = ({
  mode = 'result',
  answer,
  voteResults,
  totalVoters,
  correctResults = [],
  shouldAnimate = false,
  isMultiple,
  isSelected,
  disabled,
  onClick,
}) => {
  const lang = useLang();

  if (mode === 'vote') {
    return (
      <button
        type="button"
        className={buildClassName(
          'PollOption',
          'is-vote',
          isMultiple && 'is-multiple',
          isSelected && 'is-selected',
          disabled && 'is-disabled',
        )}
        dir={lang.isRtl ? 'rtl' : undefined}
        disabled={disabled}
        onClick={() => onClick?.(answer.option)}
      >
        <span className="poll-option-selector" aria-hidden>
          <Icon name="check" className="poll-option-selector-icon" />
        </span>
        <span className="poll-option-text" dir="auto">
          {renderTextWithEntities({
            text: answer.text.text,
            entities: answer.text.entities,
          })}
        </span>
      </button>
    );
  }

  const result = voteResults && voteResults.find((r) => r.option === answer.option);
  const isChosen = Boolean(result?.isChosen);
  const isCorrect = Boolean(result?.isCorrect) || correctResults.includes(answer.option);
  const isWrong = Boolean(correctResults.length && isChosen && !isCorrect);
  const isSelectedBar = Boolean(!isWrong && (isCorrect || (isChosen && correctResults.length === 0)));
  const answerPercent = result ? getPercentage(result.votersCount, totalVoters || 0) : 0;
  const [animatedPercent, setAnimatedPercent] = useState(shouldAnimate ? 0 : answerPercent);

  useEffect(() => {
    if (!shouldAnimate) {
      setAnimatedPercent(answerPercent);
      return undefined;
    }

    const frame = window.requestAnimationFrame(() => {
      setAnimatedPercent(answerPercent);
    });

    return () => {
      window.cancelAnimationFrame(frame);
    };
  }, [shouldAnimate, answerPercent]);

  if (!voteResults || !result) {
    return undefined;
  }

  const showStatusIcon = isWrong || isCorrect || (correctResults.length === 0 && isChosen);

  return (
    <div
      className={buildClassName(
        'PollOption',
        'is-result',
        isSelectedBar && 'is-selected',
        isWrong && 'is-wrong',
      )}
      dir={lang.isRtl ? 'rtl' : undefined}
    >
      <div className="poll-option-result-bg" aria-hidden>
        <div
          className={buildClassName(
            'poll-option-bar',
            isWrong ? 'wrong' : isSelectedBar ? 'selected' : 'unselected',
          )}
          style={`width: ${animatedPercent}%`}
        />
      </div>
      <div className="poll-option-result-content">
        <div className="poll-option-share">
          {answerPercent}
          %
        </div>
        <div className="poll-option-text" dir="auto">
          {renderTextWithEntities({
            text: answer.text.text,
            entities: answer.text.entities,
          })}
        </div>
        {showStatusIcon && (
          <span className={buildClassName('poll-option-status', isWrong && 'wrong')}>
            <Icon name={isWrong ? 'close' : 'check'} className="poll-option-icon" />
          </span>
        )}
      </div>
    </div>
  );
};

function getPercentage(value: number, total: number) {
  return total > 0 ? Math.round((value / total) * 100) : 0;
}

export default PollOption;
