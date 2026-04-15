import {
  memo, useEffect, useState,
} from '../../lib/teact/teact';

import { copyTextToClipboard } from '../../util/clipboard';

import useLastCallback from '../../hooks/useLastCallback';
import useOldLang from '../../hooks/useOldLang';

import Button from '../ui/Button';
import Modal from '../ui/Modal';

type OwnProps = {
  isOpen: boolean;
  peerUserId: string;
  peerDisplayName?: string;
  onClose: NoneToVoidFunction;
};

type Status = 'loading' | 'ready' | 'error';

// Modal body content:
//
//   * Fetches our own identity + peer identity via the crypto worker +
//     Saturn keys API.
//   * Computes the 60-digit safety number and renders it as 5 groups
//     of 12 digits.
//   * Offers Copy + Mark-as-Verified buttons. Verified state lives in
//     the `orbit-crypto` IndexedDB `verified` store.
//
// Crypto modules are loaded lazily on open so this modal can live in
// the bundle without pulling libsignal-equivalent code into the auth
// screen chunk.
const SafetyNumbersModal = ({
  isOpen,
  peerUserId,
  peerDisplayName,
  onClose,
}: OwnProps) => {
  const lang = useOldLang();
  const [status, setStatus] = useState<Status>('loading');
  const [safetyNumber, setSafetyNumber] = useState<string | undefined>(undefined);
  const [isVerified, setIsVerified] = useState(false);
  const [isTofuPinned, setIsTofuPinned] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  useEffect(() => {
    if (!isOpen) return;

    let cancelled = false;
    setStatus('loading');
    setSafetyNumber(undefined);
    setError(undefined);

    (async () => {
      try {
        const [{ getCryptoWorker }, keysApi, keyStore] = await Promise.all([
          import('../../lib/crypto/worker-proxy'),
          import('../../api/saturn/methods/keys'),
          import('../../lib/crypto/key-store'),
        ]);
        const crypto = getCryptoWorker();

        const own = await crypto.getOrCreateIdentity();
        const peerKey = await keysApi.fetchIdentityKey(peerUserId);

        // Own user id is baked into the auth flow; we do not have it
        // here as a prop, so we read it from the crypto worker's
        // identity (the identity key itself acts as the per-device
        // anchor). For the safety number we need the CHAT peer user
        // ids on both sides. Use the crypto worker's device's parent
        // user id via a global-state hook would be ideal, but to keep
        // this modal self-contained we read `currentUserId` from a
        // narrow import.
        const { getCurrentUserIdForSafetyNumbers } = await import('./safetyNumbersHelpers');
        const ownUserId = getCurrentUserIdForSafetyNumbers();
        if (!ownUserId) {
          throw new Error('current user id not available');
        }

        const number = await crypto.computeSafetyNumber(
          ownUserId,
          own.identityKeyPair.publicKey,
          peerUserId,
          peerKey,
        );

        const verified = await keyStore.getVerified(peerUserId);
        const expectedHash = await hashIdentity(peerKey);
        const hashMatches = !!verified && verified.identityHash === expectedHash;
        // User-verified = explicit confirmation via this modal.
        // TOFU-pinned = auto-pinned on first contact by the send/receive
        // pipeline (Phase 7 Step 10 security fix). Both require the hash
        // to match the current server key.
        const isStillUserVerified = hashMatches && verified?.source !== 'tofu' && verified!.verifiedAt > 0;
        const isStillTofuPinned = hashMatches && !isStillUserVerified;

        if (cancelled) return;
        setSafetyNumber(number);
        setIsVerified(isStillUserVerified);
        setIsTofuPinned(isStillTofuPinned);
        setStatus('ready');
      } catch (err) {
        if (cancelled) return;
        setStatus('error');
        setError(err instanceof Error ? err.message : 'Не удалось получить ключи');
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [isOpen, peerUserId]);

  const handleCopy = useLastCallback(() => {
    if (safetyNumber) {
      copyTextToClipboard(safetyNumber);
    }
  });

  const handleToggleVerified = useLastCallback(async () => {
    try {
      const keysApi = await import('../../api/saturn/methods/keys');
      const keyStore = await import('../../lib/crypto/key-store');
      if (isVerified) {
        // Downgrade to a fresh TOFU pin rather than full delete, so we
        // keep catching subsequent identity rotations.
        const peerKey = await keysApi.fetchIdentityKey(peerUserId);
        const identityHash = await hashIdentity(peerKey);
        await keyStore.putVerified({
          peerUserId,
          identityHash,
          verifiedAt: 0,
          source: 'tofu',
        });
        setIsVerified(false);
        setIsTofuPinned(true);
        return;
      }
      const peerKey = await keysApi.fetchIdentityKey(peerUserId);
      const identityHash = await hashIdentity(peerKey);
      await keyStore.putVerified({
        peerUserId,
        identityHash,
        verifiedAt: Date.now(),
        source: 'user',
      });
      setIsVerified(true);
      setIsTofuPinned(false);
    } catch (err) {
      // eslint-disable-next-line no-console
      console.warn('[crypto] failed to toggle verified state', err);
    }
  });

  const title = peerDisplayName
    ? `🔒 Проверка ключей — ${peerDisplayName}`
    : '🔒 Проверка ключей';

  return (
    <Modal isOpen={isOpen} onClose={onClose} title={title} hasCloseButton>
      <div style="padding: 0.75rem 1rem 1rem 1rem; max-width: 24rem;">
        {status === 'loading' && (
          <p style="opacity: 0.7;">Загрузка ключей…</p>
        )}
        {status === 'error' && (
          <p style="color: var(--color-error); white-space: pre-wrap;">{error}</p>
        )}
        {status === 'ready' && safetyNumber && (
          <>
            <p style="margin-bottom: 0.75rem;">
              Сравните эти цифры с собеседником (при личной встрече или по
              голосовому вызову). Если они совпадают, переписка защищена от
              посредника.
            </p>
            <div style="font-family: monospace; font-size: 1.1rem; line-height: 1.6; letter-spacing: 0.05em; text-align: center; padding: 0.75rem; background: var(--color-background-secondary); border-radius: 0.5rem;">
              {safetyNumber.split(' ').map((group) => (
                <div key={group}>{group}</div>
              ))}
            </div>
            <div style="display: flex; gap: 0.5rem; margin-top: 1rem;">
              <Button isText onClick={handleCopy} size="smaller">
                {lang('Copy')}
              </Button>
              <Button
                color={isVerified ? 'danger' : 'primary'}
                onClick={handleToggleVerified}
                size="smaller"
              >
                {isVerified ? 'Снять отметку' : 'Отметить как проверенный'}
              </Button>
            </div>
            {isVerified && (
              <p style="margin-top: 0.75rem; color: var(--color-text-secondary);">
                ✓ Ключи собеседника отмечены как проверенные на этом устройстве.
              </p>
            )}
            {!isVerified && isTofuPinned && (
              <p style="margin-top: 0.75rem; color: var(--color-text-secondary);">
                Ключи зафиксированы автоматически при первом контакте и до сих
                пор совпадают, но вы не подтверждали их личным сравнением. Если
                сравните цифры с собеседником — отметьте как проверенные.
              </p>
            )}
          </>
        )}
      </div>
    </Modal>
  );
};

// Tiny wrapper around WebCrypto so we don't need to import from the
// crypto layer's primitives module (which would drag the ESM-only
// @noble bundle into this file).
async function hashIdentity(key: Uint8Array): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', key as BufferSource);
  const bytes = new Uint8Array(digest);
  let hex = '';
  for (let i = 0; i < bytes.length; i++) hex += bytes[i].toString(16).padStart(2, '0');
  return hex;
}

export default memo(SafetyNumbersModal);
