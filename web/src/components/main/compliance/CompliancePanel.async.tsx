import type { OwnProps } from './CompliancePanel';

import { Bundles } from '../../../util/moduleLoader';

import useModuleLoader from '../../../hooks/useModuleLoader';

const CompliancePanelAsync = (props: OwnProps) => {
  const { isOpen } = props;
  const CompliancePanel = useModuleLoader(Bundles.Extra, 'CompliancePanel', !isOpen);

  return CompliancePanel ? <CompliancePanel {...props} /> : undefined;
};

export default CompliancePanelAsync;
