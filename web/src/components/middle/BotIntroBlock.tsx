import { memo } from '../../lib/teact/teact';
import { getActions } from '../../global';

import type { ApiUser } from '../../api/types';

import buildClassName from '../../util/buildClassName';
import renderText from '../common/helpers/renderText';

import useLang from '../../hooks/useLang';
import useLastCallback from '../../hooks/useLastCallback';

import Avatar from '../common/Avatar';
import VerifiedIcon from '../common/VerifiedIcon';
import Button from '../ui/Button';

import styles from './BotIntroBlock.module.scss';

type OwnProps = {
  bot: ApiUser;
  description?: string;
  className?: string;
};

const BotIntroBlock = ({ bot, description, className }: OwnProps) => {
  const { sendBotCommand } = getActions();
  const lang = useLang();

  const handleStart = useLastCallback(() => {
    sendBotCommand({ command: '/start' });
  });

  const username = bot.usernames?.[0]?.username;
  const fullName = [bot.firstName, bot.lastName].filter(Boolean).join(' ').trim();

  return (
    <div className={buildClassName(styles.root, className)}>
      <Avatar peer={bot} size="giant" className={styles.avatar} />

      <div className={styles.titleRow}>
        <h2 className={styles.title}>{renderText(fullName || username || 'Bot')}</h2>
        {bot.isVerified && <VerifiedIcon />}
      </div>

      {username && (
        <p className={styles.username}>{`@${username}`}</p>
      )}

      {description && (
        <p className={styles.description}>{renderText(description, ['br', 'links'])}</p>
      )}

      <Button
        className={styles.startButton}
        size="default"
        color="primary"
        onClick={handleStart}
      >
        {lang('BotStart')}
      </Button>
    </div>
  );
};

export default memo(BotIntroBlock);
