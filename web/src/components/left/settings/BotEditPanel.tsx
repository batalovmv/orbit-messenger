import {
  memo, useEffect, useMemo, useState,
} from '../../../lib/teact/teact';
import { getActions } from '../../../global';

import type { SaturnBot } from '../../../api/saturn/types';

import buildClassName from '../../../util/buildClassName';
import { copyTextToClipboard } from '../../../util/clipboard';
import { updateBot, type UpdateBotPayload } from '../../../api/saturn/methods/bots';

import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import Icon from '../../common/icons/Icon';
import Button from '../../ui/Button';
import Checkbox from '../../ui/Checkbox';
import FloatingActionButton from '../../ui/FloatingActionButton';
import InputText from '../../ui/InputText';

import styles from './BotEditPanel.module.scss';

type BotWithToken = SaturnBot & { token?: string };

type OwnProps = {
  bot: BotWithToken;
  onClose: NoneToVoidFunction;
  onSaved: (bot: SaturnBot) => void;
  onConfirmDelete: (botId: string) => void;
  onInstallToGroup: NoneToVoidFunction;
  onRotateToken: (botId: string) => void;
};

type EditableFields = {
  display_name: string;
  description: string;
  short_description: string;
  about_text: string;
  webhook_url: string;
  inline_placeholder: string;
  is_privacy_enabled: boolean;
  can_join_groups: boolean;
  can_read_all_group_messages: boolean;
  is_inline: boolean;
};

type FieldErrors = Partial<Record<
  'display_name' | 'description' | 'short_description' | 'about_text' | 'webhook_url' | 'inline_placeholder',
  string
>>;

const DISPLAY_NAME_MAX = 64;
const DESC_MAX = 256;
const SHORT_DESC_MAX = 256;
const ABOUT_MAX = 120;
const INLINE_PLACEHOLDER_MAX = 64;

function toEditable(bot: SaturnBot): EditableFields {
  return {
    display_name: bot.display_name || '',
    description: bot.description || '',
    short_description: bot.short_description || '',
    about_text: bot.about_text || '',
    webhook_url: bot.webhook_url || '',
    inline_placeholder: bot.inline_placeholder || '',
    is_privacy_enabled: bot.is_privacy_enabled,
    can_join_groups: bot.can_join_groups,
    can_read_all_group_messages: bot.can_read_all_group_messages,
    is_inline: bot.is_inline,
  };
}

function isEditableEqual(a: EditableFields, b: EditableFields): boolean {
  return (Object.keys(a) as (keyof EditableFields)[]).every((k) => a[k] === b[k]);
}

function validate(draft: EditableFields, lang: ReturnType<typeof useLang>): FieldErrors {
  const errors: FieldErrors = {};
  const trimmedName = draft.display_name.trim();
  if (!trimmedName) {
    errors.display_name = lang('BotEditDisplayNameRequired');
  } else if (trimmedName.length > DISPLAY_NAME_MAX) {
    errors.display_name = lang('BotEditDisplayNameTooLong');
  }
  if (draft.description.length > DESC_MAX) {
    errors.description = lang('BotEditDescTooLong');
  }
  if (draft.short_description.length > SHORT_DESC_MAX) {
    errors.short_description = lang('BotEditShortDescTooLong');
  }
  if (draft.about_text.length > ABOUT_MAX) {
    errors.about_text = lang('BotEditAboutTooLong');
  }
  if (draft.webhook_url.trim() && !/^https:\/\//i.test(draft.webhook_url.trim())) {
    errors.webhook_url = lang('BotEditWebhookInvalid');
  }
  if (draft.is_inline && draft.inline_placeholder.length > INLINE_PLACEHOLDER_MAX) {
    errors.inline_placeholder = lang('BotEditInlinePlaceholderTooLong');
  }
  return errors;
}

function computeDiff(orig: EditableFields, draft: EditableFields): UpdateBotPayload {
  const out: UpdateBotPayload = {};
  if (orig.display_name !== draft.display_name) out.display_name = draft.display_name.trim();
  if (orig.description !== draft.description) out.description = draft.description.trim();
  if (orig.short_description !== draft.short_description) out.short_description = draft.short_description.trim();
  if (orig.about_text !== draft.about_text) out.about_text = draft.about_text.trim();
  if (orig.webhook_url !== draft.webhook_url) out.webhook_url = draft.webhook_url.trim();
  if (orig.is_privacy_enabled !== draft.is_privacy_enabled) out.is_privacy_enabled = draft.is_privacy_enabled;
  if (orig.can_join_groups !== draft.can_join_groups) out.can_join_groups = draft.can_join_groups;
  if (orig.can_read_all_group_messages !== draft.can_read_all_group_messages) {
    out.can_read_all_group_messages = draft.can_read_all_group_messages;
  }
  if (orig.is_inline !== draft.is_inline) out.is_inline = draft.is_inline;
  if (orig.inline_placeholder !== draft.inline_placeholder) {
    out.inline_placeholder = draft.is_inline ? draft.inline_placeholder.trim() : '';
  }
  return out;
}

const BotEditPanel = ({
  bot, onClose, onSaved, onConfirmDelete, onInstallToGroup, onRotateToken,
}: OwnProps) => {
  const { showNotification } = getActions();
  const lang = useLang();

  const baseline = useMemo(() => toEditable(bot), [bot]);
  const [draft, setDraft] = useState<EditableFields>(baseline);
  const [isSaving, setIsSaving] = useState(false);

  // Re-sync draft when the upstream bot identity changes (different bot opened)
  // or when token rotates (we keep field draft as-is, only the bot prop changes).
  useEffect(() => {
    setDraft(toEditable(bot));
  }, [bot]);

  const errors = useMemo(() => validate(draft, lang), [draft, lang]);
  const isValid = Object.keys(errors).length === 0;
  const isDirty = !isEditableEqual(baseline, draft);

  const update = useLastCallback((patch: Partial<EditableFields>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
  });

  const handleCopy = useLastCallback((value: string) => {
    if (!value) return;
    copyTextToClipboard(value);
    showNotification({ message: lang('BotEditCopiedNotification') });
  });

  const handleSave = useLastCallback(async () => {
    if (!isDirty || !isValid || isSaving) return;
    setIsSaving(true);
    try {
      const payload = computeDiff(baseline, draft);
      const updated = await updateBot(bot.id, payload);
      if (updated) {
        onSaved(updated);
        showNotification({ message: lang('SettingsSaved') });
      }
    } catch (e) {
      showNotification({ message: e instanceof Error ? e.message : String(e) });
    } finally {
      setIsSaving(false);
    }
  });

  const deepLink = `${window.location.origin}/?botstart=${bot.username}`;

  function renderCopyRow(value: string, isBlurred = false, ariaKey: 'BotEditCopyAria' = 'BotEditCopyAria') {
    return (
      <div className={styles.copyRow}>
        <span className={buildClassName(styles.copyValue, isBlurred && styles.copyValueBlurred)}>
          {value}
        </span>
        <button
          type="button"
          className={styles.copyButton}
          aria-label={lang(ariaKey)}
          onClick={() => handleCopy(value)}
        >
          <Icon name="copy" />
        </button>
      </div>
    );
  }

  return (
    <div className={styles.root}>
      <div className={buildClassName(styles.scroll, 'custom-scroll')}>
        <div className={styles.section}>
          <h4 className={styles.sectionHeader}>{`@${bot.username}`}</h4>

          <InputText
            label={lang('BotDisplayName')}
            value={draft.display_name}
            error={errors.display_name}
            maxLength={DISPLAY_NAME_MAX + 32}
            onChange={(e) => update({ display_name: (e.target as HTMLInputElement).value })}
          />

          <InputText
            label={lang('BotDescription')}
            value={draft.description}
            error={errors.description}
            maxLength={DESC_MAX + 64}
            onChange={(e) => update({ description: (e.target as HTMLInputElement).value })}
          />
          <span
            className={buildClassName(
              styles.charCounter,
              draft.description.length > DESC_MAX && styles.charCounterError,
            )}
          >
            {`${draft.description.length} / ${DESC_MAX}`}
          </span>

          <InputText
            label={lang('BotShortDescription')}
            value={draft.short_description}
            error={errors.short_description}
            maxLength={SHORT_DESC_MAX + 64}
            onChange={(e) => update({ short_description: (e.target as HTMLInputElement).value })}
          />

          <InputText
            label={lang('BotAboutText')}
            value={draft.about_text}
            error={errors.about_text}
            maxLength={ABOUT_MAX + 32}
            onChange={(e) => update({ about_text: (e.target as HTMLInputElement).value })}
          />

          <InputText
            label={lang('BotWebhookUrl')}
            value={draft.webhook_url}
            error={errors.webhook_url}
            inputMode="url"
            onChange={(e) => update({ webhook_url: (e.target as HTMLInputElement).value })}
          />
          {draft.webhook_url.trim() && !errors.webhook_url
            && renderCopyRow(draft.webhook_url.trim())}
        </div>

        <div className={styles.section}>
          <h4 className={styles.sectionHeader}>{lang('BotPermissions')}</h4>
          <Checkbox
            label={lang('BotPrivacyMode')}
            subLabel={lang('BotPrivacyModeHint')}
            checked={draft.is_privacy_enabled}
            onCheck={(checked) => update({ is_privacy_enabled: checked })}
          />
          <Checkbox
            label={lang('BotCanJoinGroups')}
            subLabel={lang('BotCanJoinGroupsHint')}
            checked={draft.can_join_groups}
            onCheck={(checked) => update({ can_join_groups: checked })}
          />
          <Checkbox
            label={lang('BotCanReadAllMessages')}
            subLabel={lang('BotCanReadAllMessagesHint')}
            checked={draft.can_read_all_group_messages}
            onCheck={(checked) => update({ can_read_all_group_messages: checked })}
          />
          <Checkbox
            label={lang('BotInlineMode')}
            subLabel={lang('BotInlineModeHint')}
            checked={draft.is_inline}
            onCheck={(checked) => update({ is_inline: checked })}
          />
          {draft.is_inline && (
            <InputText
              label={lang('BotInlinePlaceholder')}
              value={draft.inline_placeholder}
              error={errors.inline_placeholder}
              maxLength={INLINE_PLACEHOLDER_MAX + 16}
              onChange={(e) => update({ inline_placeholder: (e.target as HTMLInputElement).value })}
            />
          )}
        </div>

        {bot.token && (
          <div className={styles.section}>
            <h4 className={styles.sectionHeader}>{lang('BotToken')}</h4>
            {renderCopyRow(bot.token, true)}
          </div>
        )}

        <div className={styles.section}>
          <h4 className={styles.sectionHeader}>{lang('BotEditDeepLinkLabel')}</h4>
          {renderCopyRow(deepLink)}
        </div>

        <div className={styles.actions}>
          <Button onClick={onInstallToGroup} color="translucent">
            {lang('BotAddToGroup')}
          </Button>
          <Button onClick={() => onRotateToken(bot.id)} color="translucent">
            {lang('BotRotateToken')}
          </Button>
          <Button onClick={() => onConfirmDelete(bot.id)} color="danger">
            {lang('DeleteBot')}
          </Button>
          <Button onClick={onClose} color="translucent">
            {lang('Back')}
          </Button>
        </div>
      </div>

      <FloatingActionButton
        isShown={isDirty && isValid}
        iconName="check"
        ariaLabel={lang('Save')}
        disabled={isSaving}
        isLoading={isSaving}
        onClick={handleSave}
      />
    </div>
  );
};

export default memo(BotEditPanel);
