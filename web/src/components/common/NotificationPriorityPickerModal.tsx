import type { FC } from '../../lib/teact/teact';
import {
  memo, useEffect, useMemo, useState,
} from '../../lib/teact/teact';

import type { NotificationPriorityOverride } from '../../api/saturn/methods/notifications';

import useLang from '../../hooks/useLang';
import useLastCallback from '../../hooks/useLastCallback';

import Button from '../ui/Button';
import Modal from '../ui/Modal';
import ItemPicker, { type ItemPickerOption } from './pickers/ItemPicker';

import styles from './NotificationPriorityPickerModal.module.scss';

export type OwnProps = {
  isOpen: boolean;
  isLoading?: boolean;
  selectedValue?: NotificationPriorityOverride;
  onClose: NoneToVoidFunction;
  onSubmit: (value: NotificationPriorityOverride) => void;
};

const NotificationPriorityPickerModal: FC<OwnProps> = ({
  isOpen,
  isLoading,
  selectedValue,
  onClose,
  onSubmit,
}) => {
  const lang = useLang();

  const [selectedItemValue, setSelectedItemValue] = useState<string>(selectedValue || 'default');

  const options = useMemo<ItemPickerOption[]>(() => ([
    { value: 'default', label: lang('NotificationPriorityDefault') },
    { value: 'urgent', label: lang('NotificationPriorityUrgent') },
    { value: 'important', label: lang('NotificationPriorityImportant') },
    { value: 'normal', label: lang('NotificationPriorityNormal') },
    { value: 'low', label: lang('NotificationPriorityLow') },
  ]), [lang]);

  useEffect(() => {
    setSelectedItemValue(selectedValue || 'default');
  }, [selectedValue, isOpen]);

  const handleSubmit = useLastCallback(() => {
    // eslint-disable-next-line no-null/no-null
    onSubmit(selectedItemValue === 'default' ? null : selectedItemValue as NotificationPriorityOverride);
  });

  return (
    <Modal
      className={styles.root}
      isOpen={isOpen}
      onClose={onClose}
      onEnter={handleSubmit}
      hasAbsoluteCloseButton
    >
      <div className={styles.container}>
        <h4 className={styles.title}>{lang('NotificationPriorityTitle')}</h4>
        <p className={styles.description}>{lang('NotificationPriorityAbout')}</p>
      </div>
      <div className={styles.main}>
        <ItemPicker
          className={styles.picker}
          items={options}
          selectedValue={selectedItemValue}
          forceRenderAllItems
          onSelectedValueChange={setSelectedItemValue}
          itemInputType="radio"
        />
      </div>
      <div className={styles.footer}>
        <Button
          isLoading={isLoading}
          onClick={handleSubmit}
        >
          {lang('Save')}
        </Button>
      </div>
    </Modal>
  );
};

export default memo(NotificationPriorityPickerModal);
