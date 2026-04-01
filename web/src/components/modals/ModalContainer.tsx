import { type FC, memo } from '../../lib/teact/teact';
import { withGlobal } from '../../global';

import type { TabState } from '../../global/types';

import { selectTabState } from '../../global/selectors';
import { pick } from '../../util/iteratees';

import WebAppsCloseConfirmationModal from '../main/WebAppsCloseConfirmationModal.async';
import AgeVerificationModal from './ageVerification/AgeVerificationModal.async';
import AttachBotInstallModal from './attachBotInstall/AttachBotInstallModal.async';
import BirthdaySetupModal from './birthday/BirthdaySetupModal.async';
import ChatInviteModal from './chatInvite/ChatInviteModal.async';
import ChatlistModal from './chatlist/ChatlistModal.async';
import CocoonModal from './cocoon/CocoonModal.async';
import DeleteAccountModal from './deleteAccount/DeleteAccountModal.async';
import EmojiStatusAccessModal from './emojiStatusAccess/EmojiStatusAccessModal.async';
import FrozenAccountModal from './frozenAccount/FrozenAccountModal.async';
import InviteViaLinkModal from './inviteViaLink/InviteViaLinkModal.async';
import LeaveGroupModal from './leaveGroup/LeaveGroupModal.async';
import LocationAccessModal from './locationAccess/LocationAccessModal.async';
import MapModal from './map/MapModal.async';
import OneTimeMediaModal from './oneTimeMedia/OneTimeMediaModal.async';
import PasskeyModal from './passkey/PasskeyModal.async';
import PreparedMessageModal from './preparedMessage/PreparedMessageModal.async';
import ProfileRatingModal from './profileRating/ProfileRatingModal.async';
import QuickChatPickerModal from './quickChatPicker/QuickChatPickerModal.async';
import QuickPreviewModal from './quickPreview/QuickPreviewModal.async';
import ReportModal from './reportModal/ReportModal.async';
import SharePreparedMessageModal from './sharePreparedMessage/SharePreparedMessageModal.async';
import SuggestedPostApprovalModal from './suggestedPostApproval/SuggestedPostApprovalModal.async';
import SuggestedStatusModal from './suggestedStatus/SuggestedStatusModal.async';
import SuggestMessageModal from './suggestMessage/SuggestMessageModal.async';
import TwoFaCheckModal from './twoFaCheck/TwoFaCheckModal.async';
import UrlAuthModal from './urlAuth/UrlAuthModal.async';
import WebAppModal from './webApp/WebAppModal.async';

// `Pick` used only to provide tab completion
type ModalKey = keyof Pick<TabState,
  'chatlistModal' |
  'urlAuth' |
  'mapModal' |
  'oneTimeMediaModal' |
  'inviteViaLinkModal' |
  'requestedAttachBotInstall' |
  'reportModal' |
  'suggestMessageModal' |
  'suggestedPostApprovalModal' |
  'webApps' |
  'chatInviteModal' |
  'isWebAppsCloseConfirmationModalOpen' |
  'suggestedStatusModal' |
  'emojiStatusAccessModal' |
  'locationAccessModal' |
  'preparedMessageModal' |
  'sharePreparedMessageModal' |
  'isFrozenAccountModalOpen' |
  'deleteAccountModal' |
  'isAgeVerificationModalOpen' |
  'profileRatingModal' |
  'quickPreview' |
  'isPasskeyModalOpen' |
  'birthdaySetupModal' |
  'leaveGroupModal' |
  'isTwoFaCheckModalOpen' |
  'isQuickChatPickerOpen' |
  'isCocoonModalOpen'
>;

type StateProps = {
  [K in ModalKey]?: TabState[K];
};
type ModalRegistry = {
  [K in ModalKey]: FC<{
    modal: TabState[K];
  }>;
};
type Entries<T> = {
  [K in keyof T]: [K, T[K]];
}[keyof T][];

const MODALS: ModalRegistry = {
  chatlistModal: ChatlistModal,
  urlAuth: UrlAuthModal,
  oneTimeMediaModal: OneTimeMediaModal,
  inviteViaLinkModal: InviteViaLinkModal,
  requestedAttachBotInstall: AttachBotInstallModal,
  reportModal: ReportModal,
  webApps: WebAppModal,
  mapModal: MapModal,
  chatInviteModal: ChatInviteModal,
  suggestMessageModal: SuggestMessageModal,
  suggestedPostApprovalModal: SuggestedPostApprovalModal,
  isWebAppsCloseConfirmationModalOpen: WebAppsCloseConfirmationModal,
  suggestedStatusModal: SuggestedStatusModal,
  emojiStatusAccessModal: EmojiStatusAccessModal,
  locationAccessModal: LocationAccessModal,
  preparedMessageModal: PreparedMessageModal,
  sharePreparedMessageModal: SharePreparedMessageModal,
  isFrozenAccountModalOpen: FrozenAccountModal,
  deleteAccountModal: DeleteAccountModal,
  isAgeVerificationModalOpen: AgeVerificationModal,
  profileRatingModal: ProfileRatingModal,
  quickPreview: QuickPreviewModal,
  isPasskeyModalOpen: PasskeyModal,
  birthdaySetupModal: BirthdaySetupModal,
  leaveGroupModal: LeaveGroupModal,
  isTwoFaCheckModalOpen: TwoFaCheckModal,
  isQuickChatPickerOpen: QuickChatPickerModal,
  isCocoonModalOpen: CocoonModal,
};
const MODAL_KEYS = Object.keys(MODALS) as ModalKey[];
const MODAL_ENTRIES = Object.entries(MODALS) as Entries<ModalRegistry>;

const ModalContainer = (modalProps: StateProps) => {
  return MODAL_ENTRIES.map(([key, ModalComponent]) => (
    // @ts-ignore -- TS does not preserve tuple types in `map` callbacks
    <ModalComponent key={key} modal={modalProps[key]} />
  ));
};

export default memo(withGlobal(
  (global): Complete<StateProps> => (
    pick(selectTabState(global), MODAL_KEYS) as Complete<StateProps>
  ),
)(ModalContainer));
