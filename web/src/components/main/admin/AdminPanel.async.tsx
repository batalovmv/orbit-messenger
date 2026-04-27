import type { OwnProps } from './AdminPanel';

import { Bundles } from '../../../util/moduleLoader';

import useModuleLoader from '../../../hooks/useModuleLoader';

const AdminPanelAsync = (props: OwnProps) => {
  const { isOpen } = props;
  const AdminPanel = useModuleLoader(Bundles.Extra, 'AdminPanel', !isOpen);

  return AdminPanel ? <AdminPanel {...props} /> : undefined;
};

export default AdminPanelAsync;
