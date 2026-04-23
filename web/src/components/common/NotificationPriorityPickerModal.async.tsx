import type { OwnProps } from './NotificationPriorityPickerModal';

import { Bundles } from '../../util/moduleLoader';

import useModuleLoader from '../../hooks/useModuleLoader';

const NotificationPriorityPickerModalAsync = (props: OwnProps) => {
  const { isOpen } = props;
  const NotificationPriorityPickerModal = useModuleLoader(Bundles.Extra, 'NotificationPriorityPickerModal', !isOpen);

  return NotificationPriorityPickerModal ? <NotificationPriorityPickerModal {...props} /> : undefined;
};

export default NotificationPriorityPickerModalAsync;
