import type { ActionReturnType } from '../../types';

import { formatCurrencyAsString } from '../../../util/formatCurrency';
import * as langProvider from '../../../util/oldLangProvider';
import { addActionHandler, setGlobal } from '../../index';

addActionHandler('apiUpdate', (global, actions, update): ActionReturnType => {
  switch (update['@type']) {
    case 'updatePaymentStateCompleted': {
      const { paymentState, tabId } = update;
      const form = paymentState.form!;
      const { invoice } = form;

      const { totalAmount, currency } = invoice;

      actions.showNotification({
        tabId,
        message: langProvider.oldTranslate('PaymentInfoHint', [
          formatCurrencyAsString(totalAmount, currency, langProvider.getTranslationFn().code),
          form.title,
        ]),
      });

      setGlobal(global);
      break;
    }
  }
});
