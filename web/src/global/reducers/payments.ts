import type {
  ApiReceiptRegular,
} from '../../api/types';
import type {
  PaymentStep,
  ShippingOption,
} from '../../types';
import type {
  GlobalState, TabArgs, TabState,
} from '../types';

import { getCurrentTabId } from '../../util/establishMultitabRole';
import { selectTabState } from '../selectors';
import { updateTabState } from './tabs';

export function updatePayment<T extends GlobalState>(
  global: T, update: Partial<TabState['payment']>,
  ...[tabId = getCurrentTabId()]: TabArgs<T>
): T {
  return updateTabState(global, {
    payment: {
      ...selectTabState(global, tabId).payment,
      ...update,
    },
  }, tabId);
}

export function updateStarsPayment<T extends GlobalState>(
  global: T, _update: Record<string, unknown>,
  ...[_tabId = getCurrentTabId()]: TabArgs<T>
): T {
  return global;
}

export function updateShippingOptions<T extends GlobalState>(
  global: T,
  shippingOptions: ShippingOption[],
  ...[tabId = getCurrentTabId()]: TabArgs<T>

): T {
  return updatePayment(global, { shippingOptions }, tabId);
}

export function setRequestInfoId<T extends GlobalState>(
  global: T, id: string,
  ...[tabId = getCurrentTabId()]: TabArgs<T>
): T {
  return updatePayment(global, { requestId: id }, tabId);
}

export function setPaymentStep<T extends GlobalState>(
  global: T, step: PaymentStep,
  ...[tabId = getCurrentTabId()]: TabArgs<T>
): T {
  return updatePayment(global, { step }, tabId);
}

export function setStripeCardInfo<T extends GlobalState>(
  global: T, cardInfo: { type: string; id: string },
  ...[tabId = getCurrentTabId()]: TabArgs<T>
): T {
  return updatePayment(global, { stripeCredentials: { ...cardInfo } }, tabId);
}

export function setSmartGlocalCardInfo<T extends GlobalState>(
  global: T,
  cardInfo: { type: string; token: string },
  ...[tabId = getCurrentTabId()]: TabArgs<T>
): T {
  return updatePayment(global, { smartGlocalCredentials: { ...cardInfo } }, tabId);
}

export function setConfirmPaymentUrl<T extends GlobalState>(
  global: T, url?: string,
  ...[tabId = getCurrentTabId()]: TabArgs<T>
): T {
  return updatePayment(global, { confirmPaymentUrl: url }, tabId);
}

export function setReceipt<T extends GlobalState>(
  global: T,
  receipt?: ApiReceiptRegular,
  ...[tabId = getCurrentTabId()]: TabArgs<T>
): T {
  if (!receipt) {
    return updatePayment(global, { receipt: undefined }, tabId);
  }

  return updatePayment(global, {
    receipt,
  }, tabId);
}

export function clearPayment<T extends GlobalState>(
  global: T,
  ...[tabId = getCurrentTabId()]: TabArgs<T>
): T {
  return updateTabState(global, {
    payment: {},
  }, tabId);
}

export function clearStarPayment<T extends GlobalState>(
  global: T,
  ...[_tabId = getCurrentTabId()]: TabArgs<T>
): T {
  return global;
}

export function closeInvoice<T extends GlobalState>(
  global: T,
  ...[tabId = getCurrentTabId()]: TabArgs<T>
): T {
  global = updatePayment(global, {
    isPaymentModalOpen: undefined,
    isExtendedMedia: undefined,
  }, tabId);
  return global;
}

export function updateStarsBalance<T extends GlobalState>(
  global: T, _balance: unknown,
): T {
  return global;
}

export function appendStarsTransactions<T extends GlobalState>(
  global: T,
  _type: unknown,
  _transactions: unknown[],
  _nextOffset?: string,
  _isTon?: boolean,
): T {
  return global;
}

export function appendStarsSubscriptions<T extends GlobalState>(
  global: T,
  _subscriptions: unknown[],
  _nextOffset?: string,
): T {
  return global;
}

export function updateStarsSubscriptionLoading<T extends GlobalState>(
  global: T, _isLoading: boolean,
): T {
  return global;
}

export function openStarsTransactionModal<T extends GlobalState>(
  global: T, _transaction: unknown, ...[_tabId = getCurrentTabId()]: TabArgs<T>
): T {
  return global;
}

export function openStarsTransactionFromReceipt<T extends GlobalState>(
  global: T, _receipt: unknown, ...[tabId = getCurrentTabId()]: TabArgs<T>
): T {
  return openStarsTransactionModal(global, undefined, tabId);
}
