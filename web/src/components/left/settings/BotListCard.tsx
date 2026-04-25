import { memo } from '../../../lib/teact/teact';

import type { SaturnBot } from '../../../api/saturn/types';

import buildClassName from '../../../util/buildClassName';

import useLang from '../../../hooks/useLang';

import styles from './BotListCard.module.scss';

const BOT_AVATAR_COLORS = [
  '#e17076', '#eda86c', '#a695e7', '#7bc862',
  '#65aadd', '#ee7aae', '#6ec9cb', '#7882ec',
];

function hashString(input: string): number {
  let hash = 0;
  for (let i = 0; i < input.length; i++) {
    hash = (hash * 31 + input.charCodeAt(i)) | 0;
  }
  return Math.abs(hash);
}

function pickAvatarColor(seed: string): string {
  return BOT_AVATAR_COLORS[hashString(seed) % BOT_AVATAR_COLORS.length];
}

function getInitial(bot: SaturnBot): string {
  const source = bot.display_name?.trim() || bot.username?.trim() || '?';
  const ch = source.charAt(0);
  return ch ? ch.toUpperCase() : '?';
}

type OwnProps = {
  bot: SaturnBot;
  onClick: (bot: SaturnBot) => void;
};

const BotListCard = ({ bot, onClick }: OwnProps) => {
  const lang = useLang();

  const initial = getInitial(bot);
  const avatarStyle = `background-color: ${pickAvatarColor(bot.username || bot.id)}`;

  const created = bot.created_at ? new Date(bot.created_at) : undefined;
  const createdLabel = created
    ? created.toLocaleDateString(lang.code || 'ru-RU', { day: '2-digit', month: '2-digit', year: 'numeric' })
    : '';

  const isActive = bot.is_active;

  return (
    <button
      type="button"
      className={styles.card}
      onClick={() => onClick(bot)}
    >
      {bot.avatar_url ? (
        <span className={styles.avatar}>
          <img className={styles.avatarImage} src={bot.avatar_url} alt="" />
        </span>
      ) : (
        <span className={styles.avatar} style={avatarStyle}>{initial}</span>
      )}
      <span className={styles.body}>
        <span className={styles.title}>{bot.display_name}</span>
        <span className={styles.handle}>{`@${bot.username}`}</span>
        {createdLabel && (
          <span className={styles.subtitle}>
            {`${lang('BotCardCreated')} ${createdLabel}`}
          </span>
        )}
      </span>
      <span
        className={buildClassName(
          styles.status,
          isActive ? styles.statusActive : styles.statusDisabled,
        )}
      >
        <span className={styles.statusDot} />
        {isActive ? lang('BotStatusActive') : lang('BotStatusDisabled')}
      </span>
    </button>
  );
};

export default memo(BotListCard);
