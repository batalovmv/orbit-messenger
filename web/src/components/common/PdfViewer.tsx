import { memo, useEffect, useState } from '../../lib/teact/teact';

import captureEscKeyListener from '../../util/captureEscKeyListener';

import useLastCallback from '../../hooks/useLastCallback';
import useLang from '../../hooks/useLang';
import useOldLang from '../../hooks/useOldLang';

import Button from '../ui/Button';
import Portal from '../ui/Portal';

import styles from './PdfViewer.module.scss';

type OwnProps = {
  url: string;
  fileName: string;
  onClose: NoneToVoidFunction;
};

const PdfViewer = ({ url, fileName, onClose }: OwnProps) => {
  const lang = useLang();
  const oldLang = useOldLang();
  const [isFallback, setIsFallback] = useState(false);

  useEffect(() => captureEscKeyListener(onClose), [onClose]);

  const handleIframeError = useLastCallback(() => {
    setIsFallback(true);
  });

  const handleOpenExternal = useLastCallback(() => {
    window.open(url, '_blank', 'noopener');
  });

  const handleDownload = useLastCallback(() => {
    const a = document.createElement('a');
    a.href = url;
    a.download = fileName;
    a.click();
  });

  return (
    <Portal>
      <div className={styles.root}>
        <div className={styles.header}>
          <div className={styles.headerInfo}>
            <div className={styles.pdfIcon}>PDF</div>
            <div className={styles.fileName} title={fileName}>{fileName}</div>
          </div>
          <div className={styles.headerActions}>
            <Button
              round
              color="translucent"
              size="smaller"
              iconName="link"
              ariaLabel={oldLang('OpenInNewTab')}
              onClick={handleOpenExternal}
            />
            <Button
              round
              color="translucent"
              size="smaller"
              iconName="download"
              ariaLabel={oldLang('AccActionDownload')}
              onClick={handleDownload}
            />
            <Button
              round
              color="translucent"
              size="smaller"
              iconName="close"
              ariaLabel={oldLang('Close')}
              onClick={onClose}
            />
          </div>
        </div>
        <div className={styles.content}>
          {isFallback ? (
            <div className={styles.fallback}>
              <p>{lang('PdfCannotPreview')}</p>
              <Button onClick={handleOpenExternal}>
                {lang('OpenInNewTab')}
              </Button>
            </div>
          ) : (
            <iframe
              className={styles.iframe}
              src={url}
              title={fileName}
              onError={handleIframeError}
            />
          )}
        </div>
      </div>
    </Portal>
  );
};

export default memo(PdfViewer);
