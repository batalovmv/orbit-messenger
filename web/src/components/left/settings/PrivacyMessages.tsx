import {
  memo, useMemo, useState,
} from '../../../lib/teact/teact';
import { getActions, withGlobal } from '../../../global';

import type { GlobalState } from '../../../global/types';

import {
  selectIsCurrentUserPremium,
  selectNewNoncontactPeersRequirePremium,
} from '../../../global/selectors';

import useEffectWithPrevDeps from '../../../hooks/useEffectWithPrevDeps';
import useHistoryBack from '../../../hooks/useHistoryBack';
import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';
import useOldLang from '../../../hooks/useOldLang';

import RadioGroup from '../../ui/RadioGroup';
import PrivacyLockedOption from './PrivacyLockedOption';

type OwnProps = {
  isActive?: boolean;
  onReset: VoidFunction;
};

type StateProps = {
  shouldNewNonContactPeersRequirePremium?: boolean;
  canLimitNewMessagesWithoutPremium?: boolean;
  isCurrentUserPremium?: boolean;
  privacy: GlobalState['settings']['privacy'];
};

function PrivacyMessages({
  isActive,
  canLimitNewMessagesWithoutPremium,
  shouldNewNonContactPeersRequirePremium,
  isCurrentUserPremium,
  onReset,
  privacy,
}: OwnProps & StateProps) {
  const { updateGlobalPrivacySettings, showNotification } = getActions();
  const oldLang = useOldLang();
  const lang = useLang();

  const canChangeForContactsAndPremium = isCurrentUserPremium || canLimitNewMessagesWithoutPremium;
  const [hasShownNotification, setHasShownNotification] = useState(false);

  const selectedValue = useMemo(() => {
    if (shouldNewNonContactPeersRequirePremium) return 'contacts_and_premium';
    return 'everybody';
  }, [shouldNewNonContactPeersRequirePremium]);

  useEffectWithPrevDeps(([prevSelectedValue]) => {
    if (
      !hasShownNotification && prevSelectedValue !== undefined
      && selectedValue !== 'everybody'
      && selectedValue !== prevSelectedValue
    ) {
      if (privacy.chatInvite?.visibility === 'everybody') {
        showNotification({
          message: lang('CheckPrivacyInviteText'),
          actionText: { key: 'Review' },
          duration: 8000,
        });
      }
      if (privacy.phoneCall?.visibility === 'everybody') {
        showNotification({
          message: lang('CheckPrivacyCallsText'),
          actionText: { key: 'Review' },
          duration: 8000,
        });
      }
      setHasShownNotification(true);
    }
  }, [selectedValue, privacy, lang, hasShownNotification]);

  const options = useMemo(() => {
    return [
      { value: 'everybody', label: oldLang('P2PEverybody') },
      {
        value: 'contacts_and_premium',
        label: canChangeForContactsAndPremium ? (
          oldLang('PrivacyMessagesContactsAndPremium')
        ) : (
          <PrivacyLockedOption
            label={oldLang('PrivacyMessagesContactsAndPremium')}
            isChecked={selectedValue === 'contacts_and_premium'}
          />
        ),
        hidden: !canChangeForContactsAndPremium,
        isCanCheckedInDisabled: true,
      },
    ];
  }, [oldLang, canChangeForContactsAndPremium, selectedValue]);

  const handleChange = useLastCallback((privacyValue: string) => {
    updateGlobalPrivacySettings({
      shouldNewNonContactPeersRequirePremium: privacyValue === 'contacts_and_premium',
      // eslint-disable-next-line no-null/no-null
      nonContactPeersPaidStars: null,
    });
  });

  useHistoryBack({
    isActive,
    onBack: onReset,
  });

  return (
    <div className="settings-item">
      <h4 className="settings-item-header" dir={lang.isRtl ? 'rtl' : undefined}>
        {oldLang('PrivacyMessagesTitle')}
      </h4>
      <RadioGroup
        name="privacy-messages"
        options={options}
        onChange={handleChange}
        selected={selectedValue}
      />
      <p className="settings-item-description-larger" dir={lang.isRtl ? 'rtl' : undefined}>
        {lang('PrivacyDescriptionMessagesContactsAndPremium')}
      </p>
    </div>
  );
}

export default memo(withGlobal<OwnProps>((global): Complete<StateProps> => {
  const {
    settings: {
      privacy,
    },
  } = global;

  return {
    shouldNewNonContactPeersRequirePremium: selectNewNoncontactPeersRequirePremium(global),
    isCurrentUserPremium: selectIsCurrentUserPremium(global),
    canLimitNewMessagesWithoutPremium: global.appConfig.canLimitNewMessagesWithoutPremium,
    privacy,
  };
})(PrivacyMessages));
