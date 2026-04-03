import type { FC } from '../../../lib/teact/teact';
import type React from '../../../lib/teact/teact';
import {
  memo,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from '../../../lib/teact/teact';
import { getActions, getGlobal } from '../../../global';

import type {
  ApiMessage, ApiPeer, ApiPoll, ApiPollAnswer,
} from '../../../api/types';
import type { ObserveFn } from '../../../hooks/useIntersectionObserver';
import type { OldLangFn } from '../../../hooks/useOldLang';

import { requestMutation } from '../../../lib/fasterdom/fasterdom';
import { selectPeer } from '../../../global/selectors';
import buildClassName from '../../../util/buildClassName';
import { formatMediaDuration } from '../../../util/dates/dateFormat';
import { getMessageKey } from '../../../util/keys/messageKey';
import { getServerTime } from '../../../util/serverTime';
import { renderTextWithEntities } from '../../common/helpers/renderTextWithEntities';

import useLastCallback from '../../../hooks/useLastCallback';
import useOldLang from '../../../hooks/useOldLang';

import AvatarList from '../../common/AvatarList';
import Button from '../../ui/Button';
import PollOption from './PollOption';

import './Poll.scss';

type OwnProps = {
  message: ApiMessage;
  poll: ApiPoll;
  observeIntersectionForLoading?: ObserveFn;
  observeIntersectionForPlaying?: ObserveFn;
  onSendVote: (options: string[]) => void;
};

const SOLUTION_CONTAINER_ID = '#middle-column-portals';
const SOLUTION_DURATION = 5000;
const TIMER_RADIUS = 6;
const TIMER_CIRCUMFERENCE = TIMER_RADIUS * 2 * Math.PI;
const TIMER_UPDATE_INTERVAL = 1000;
const NBSP = '\u00A0';

const Poll: FC<OwnProps> = ({
  message,
  poll,
  observeIntersectionForLoading,
  observeIntersectionForPlaying,
  onSendVote,
}) => {
  const {
    loadMessage, openPollResults, requestConfetti, showNotification,
  } = getActions();

  const { id: messageId, chatId } = message;
  const { summary, results } = poll;
  const summaryQuestion = summary.question || { text: '', entities: [] };
  const summaryAnswers = useMemo(() => summary.answers || [], [summary.answers]);
  const [isSubmitting, setIsSubmitting] = useState<boolean>(false);
  const [chosenOptions, setChosenOptions] = useState<string[]>([]);
  const [wasSubmitted, setWasSubmitted] = useState<boolean>(false);
  const [closePeriod, setClosePeriod] = useState<number>(() => (
    !summary.closed && summary.closeDate && summary.closeDate > 0
      ? Math.min(summary.closeDate - getServerTime(), summary.closePeriod!)
      : 0
  ));
  const countdownRef = useRef<HTMLDivElement>();
  const timerCircleRef = useRef<SVGCircleElement>();
  const { results: voteResults, totalVoters } = results;
  const hasVoted = Boolean(voteResults && voteResults.some((r) => r.isChosen));
  const canVote = !summary.closed && !hasVoted;
  const maxVotersCount = useMemo(() => {
    if (!voteResults) return 0;
    return Math.max(...voteResults.map((r) => r.votersCount), 0);
  }, [voteResults]);
  const canViewResult = !canVote && summary.isPublic && Number(results.totalVoters) > 0;
  const isMultiple = Boolean(summary.multipleChoice);
  const recentVoterIds = results.recentVoterIds;
  const correctResults = useMemo(() => {
    return voteResults?.filter((r) => r.isCorrect).map((r) => r.option) || [];
  }, [voteResults]);

  useEffect(() => {
    const chosen = poll.results.results?.find((result) => result.isChosen);
    if (isSubmitting && chosen) {
      if (chosen.isCorrect) {
        requestConfetti({});
      }
      setIsSubmitting(false);
    }
  }, [isSubmitting, poll.results.results, requestConfetti]);

  useEffect(() => {
    if (!canVote && chosenOptions.length) {
      setChosenOptions([]);
    }
  }, [canVote, chosenOptions.length]);

  useLayoutEffect(() => {
    if (closePeriod > 0) {
      setTimeout(() => setClosePeriod(closePeriod - 1), TIMER_UPDATE_INTERVAL);
    }
    if (!timerCircleRef.current) return;

    const strokeDashOffset = ((summary.closePeriod! - closePeriod) / summary.closePeriod!) * TIMER_CIRCUMFERENCE;
    requestMutation(() => {
      if (closePeriod <= 5) {
        countdownRef.current?.classList.add('hurry-up');
      }

      timerCircleRef.current?.setAttribute('stroke-dashoffset', `-${strokeDashOffset}`);
    });
  }, [closePeriod, summary.closePeriod]);

  useEffect(() => {
    if (summary.quiz && (closePeriod <= 0 || (hasVoted && !summary.closed))) {
      loadMessage({ chatId, messageId });
    }
  }, [chatId, closePeriod, hasVoted, loadMessage, messageId, summary.closed, summary.quiz]);

  // If the client time is not synchronized, the poll must be updated after the closePeriod time has expired.
  useEffect(() => {
    let timer: number | undefined;

    if (summary.quiz && !summary.closed && summary.closePeriod && summary.closePeriod > 0) {
      timer = window.setTimeout(() => {
        loadMessage({ chatId, messageId });
      }, summary.closePeriod * 1000);
    }

    return () => {
      if (timer) {
        window.clearTimeout(timer);
      }
    };
  }, [canVote, chatId, loadMessage, messageId, summary.closePeriod, summary.closed, summary.quiz]);

  const recentVoters = useMemo(() => {
    // No need for expensive global updates on chats or users, so we avoid them
    const global = getGlobal();
    return recentVoterIds ? recentVoterIds.reduce((result: ApiPeer[], id) => {
      const peer = selectPeer(global, id);
      if (peer) {
        result.push(peer);
      }

      return result;
    }, []) : [];
  }, [recentVoterIds]);

  const handleOptionClick = useLastCallback((option: string) => {
    if (!canVote || message.isScheduled || isSubmitting) {
      return;
    }

    if (isMultiple) {
      setChosenOptions((current) => {
        return current.includes(option)
          ? current.filter((currentOption) => currentOption !== option)
          : [...current, option];
      });
    } else {
      // Single-choice: submit immediately on click (Telegram behavior)
      setIsSubmitting(true);
      setWasSubmitted(true);
      onSendVote([option]);
    }
  });

  const handleVoteClick = useLastCallback(() => {
    if (!chosenOptions.length || message.isScheduled || isSubmitting) {
      return;
    }

    setIsSubmitting(true);
    setWasSubmitted(true);
    onSendVote(chosenOptions);
  });

  const handleViewResultsClick = useLastCallback(() => {
    openPollResults({ chatId, messageId });
  });

  const showSolution = useLastCallback(() => {
    showNotification({
      localId: getMessageKey(message),
      message: poll.results.solution!,
      messageEntities: poll.results.solutionEntities,
      duration: SOLUTION_DURATION,
      containerSelector: SOLUTION_CONTAINER_ID,
    });
  });

  // Show the solution to quiz if the answer was incorrect
  useEffect(() => {
    if (wasSubmitted && hasVoted && summary.quiz && results.results && poll.results.solution) {
      const correctResult = results.results.find((r) => r.isChosen && r.isCorrect);
      if (!correctResult) {
        showSolution();
      }
    }
  }, [hasVoted, wasSubmitted, results.results, summary.quiz, poll.results.solution]);

  const lang = useOldLang();

  function renderResultOption(answer: ApiPollAnswer) {
    return (
      <PollOption
        key={answer.option}
        shouldAnimate={wasSubmitted || !canVote}
        answer={answer}
        voteResults={voteResults}
        totalVoters={totalVoters}
        maxVotersCount={maxVotersCount}
        correctResults={correctResults}
      />
    );
  }

  function renderVoteOption(answer: ApiPollAnswer) {
    return (
      <PollOption
        key={answer.option}
        mode="vote"
        answer={answer}
        isMultiple={isMultiple}
        isSelected={chosenOptions.includes(answer.option)}
        disabled={message.isScheduled || isSubmitting}
        onClick={handleOptionClick}
      />
    );
  }

  function renderRecentVoters() {
    return (
      recentVoters.length > 0 && (
        <div className="poll-recent-voters">
          <AvatarList
            size="micro"
            peers={recentVoters}
          />
        </div>
      )
    );
  }

  return (
    <div
      className={buildClassName(
        'Poll',
        canVote ? 'can-vote' : 'has-results',
        summary.closed && 'is-closed',
        summary.quiz && 'is-quiz',
        isMultiple && 'is-multiple',
      )}
      dir={lang.isRtl ? 'auto' : 'ltr'}
    >
      <div className="poll-question">
        {renderTextWithEntities({
          text: summaryQuestion.text,
          entities: summaryQuestion.entities,
          observeIntersectionForLoading,
          observeIntersectionForPlaying,
        })}
      </div>
      <div className="poll-type">
        {lang(getPollTypeString(summary))}
        {renderRecentVoters()}
        {closePeriod > 0 && canVote && (
          <div ref={countdownRef} className="poll-countdown">
            <span>{formatMediaDuration(closePeriod)}</span>
            <svg width="16px" height="16px">
              <circle
                ref={timerCircleRef}
                cx="8"
                cy="8"
                r={TIMER_RADIUS}
                className="poll-countdown-progress"
                transform="rotate(-90, 8, 8)"
                stroke-dasharray={TIMER_CIRCUMFERENCE}
                stroke-dashoffset="0"
              />
            </svg>
          </div>
        )}
        {summary.quiz && poll.results.solution && !canVote && (
          <Button
            round
            size="tiny"
            color="translucent"
            className="poll-quiz-help"
            onClick={showSolution}
            ariaLabel="Show Solution"
            iconName="lamp"
          />
        )}
      </div>
      {canVote && (
        <div
          className="poll-answers"
          onClick={stopPropagation}
        >
          {summaryAnswers
            .filter((answer) => !(summary.quiz && summary.closePeriod && closePeriod <= 0))
            .map(renderVoteOption)}
        </div>
      )}
      {!canVote && (
        <div className="poll-results">
          {summaryAnswers.map(renderResultOption)}
        </div>
      )}
      {canVote && isMultiple && (
        <Button
          className={buildClassName('poll-action-button', chosenOptions.length > 0 && 'active')}
          isText
          disabled={chosenOptions.length === 0 || message.isScheduled || isSubmitting}
          size="tiny"
          onClick={handleVoteClick}
        >
          {lang('PollSubmitVotes')}
        </Button>
      )}
      {!canViewResult && !canVote && (
        <div className="poll-voters-count">{getReadableVotersCount(lang, summary.quiz, results.totalVoters)}</div>
      )}
      {canViewResult && (
        <Button
          className="poll-action-button active"
          isText
          size="tiny"
          onClick={handleViewResultsClick}
        >
          {lang('PollViewResults')}
        </Button>
      )}
    </div>
  );
};

function getPollTypeString(summary: ApiPoll['summary']) {
  if (summary.closed) {
    return 'FinalResults';
  }

  // When we just created the poll, some properties don't exist.
  if (typeof summary.isPublic === 'undefined') {
    return NBSP;
  }

  if (summary.quiz) {
    return summary.isPublic ? 'QuizPoll' : 'AnonymousQuizPoll';
  }

  return summary.isPublic ? 'PublicPoll' : 'AnonymousPoll';
}

function getReadableVotersCount(lang: OldLangFn, isQuiz: boolean | undefined, count?: number) {
  if (!count) {
    return lang(isQuiz ? 'Chat.Quiz.TotalVotesEmpty' : 'Chat.Poll.TotalVotesResultEmpty');
  }

  return lang(isQuiz ? 'Answer' : 'Vote', count, 'i');
}

function stopPropagation(e: React.MouseEvent<HTMLDivElement>) {
  e.stopPropagation();
}

export default memo(Poll);
