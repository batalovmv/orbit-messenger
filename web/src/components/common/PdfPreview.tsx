import {
  memo, useMemo,
} from '../../lib/teact/teact';

import type { IconName } from '../../types/icons';

import buildClassName from '../../util/buildClassName';
import { formatMediaDateTime, formatPastTimeShort } from '../../util/dates/dateFormat';
import renderText from './helpers/renderText';

import useLang from '../../hooks/useLang';
import useOldLang from '../../hooks/useOldLang';

import Link from '../ui/Link';
import ProgressSpinner from '../ui/ProgressSpinner';
import AnimatedFileSize from './AnimatedFileSize';
import Icon from './icons/Icon';

import styles from './PdfPreview.module.scss';

type OwnProps = {
  id?: string;
  name: string;
  extension?: string;
  size: number;
  timestamp?: number;
  sender?: string;
  thumbnailDataUri?: string;
  previewData?: string;
  className?: string;
  smaller?: boolean;
  isTransferring?: boolean;
  isUploading?: boolean;
  isSelectable?: boolean;
  isSelected?: boolean;
  transferProgress?: number;
  actionIcon?: IconName;
  onClick?: NoneToVoidFunction;
  onDateClick?: (e: React.MouseEvent<HTMLAnchorElement>) => void;
};

const PdfPreview = ({
  id,
  name,
  extension = 'pdf',
  size,
  timestamp,
  sender,
  thumbnailDataUri,
  previewData,
  className,
  smaller,
  isTransferring,
  isUploading,
  isSelectable,
  isSelected,
  transferProgress,
  actionIcon,
  onClick,
  onDateClick,
}: OwnProps) => {
  const oldLang = useOldLang();
  const lang = useLang();

  const previewSource = previewData || thumbnailDataUri;
  const extensionLabel = useMemo(() => (
    extension ? extension.slice(0, 4).toUpperCase() : 'PDF'
  ), [extension]);

  const rootClassName = buildClassName(
    styles.root,
    className,
    smaller && styles.smaller,
    onClick && !isUploading && styles.interactive,
    isSelected && styles.selected,
  );

  const actionIconClassName = buildClassName(
    styles.actionIcon,
    isTransferring && styles.hidden,
  );

  return (
    <div id={id} className={rootClassName} dir={lang.isRtl ? 'rtl' : undefined}>
      {isSelectable && (
        <div className="message-select-control no-selection">
          {isSelected && <Icon name="check" className="message-select-control-icon" />}
        </div>
      )}

      <div className={styles.previewColumn}>
        <div className={styles.preview} onClick={isUploading ? undefined : onClick}>
          {previewSource ? (
            <img
              src={previewSource}
              className={styles.previewImage}
              draggable={false}
              alt=""
            />
          ) : (
            <div className={styles.paperStack}>
              <div className={styles.paperShadow} />
              <div className={styles.paper}>
                <div className={styles.paperHeader}>
                  <span className={styles.paperTitle}>Orbit PDF</span>
                  <span className={styles.paperExtension}>{extensionLabel}</span>
                </div>
                <div className={styles.paperLines}>
                  <span className={styles.paperLine} />
                  <span className={styles.paperLine} />
                  <span className={styles.paperLine} />
                  <span className={styles.paperLine} />
                  <span className={styles.paperLineShort} />
                </div>
              </div>
            </div>
          )}

          <span className={styles.pdfBadge}>PDF</span>

          {isTransferring && (
            <div className={styles.progress}>
              <ProgressSpinner
                progress={transferProgress}
                size={smaller ? 's' : 'm'}
                onClick={isUploading ? onClick : undefined}
              />
            </div>
          )}

          {onClick && (
            <Icon
              name={actionIcon || 'eye'}
              className={actionIconClassName}
            />
          )}
        </div>
      </div>

      <div className={styles.info}>
        <div className={styles.fileTitle} dir="auto" title={name}>{renderText(name)}</div>
        <div className={styles.fileSubtitle} dir="auto">
          <span className={styles.fileType}>PDF</span>
          <span className={styles.bullet}>&bull;</span>
          <AnimatedFileSize size={size} progress={isTransferring ? transferProgress : undefined} />
          {sender && (
            <>
              <span className={styles.bullet}>&bull;</span>
              <span className={styles.fileSender}>{renderText(sender)}</span>
            </>
          )}
          {!sender && Boolean(timestamp) && (
            <>
              <span className={styles.bullet}>&bull;</span>
              <Link onClick={onDateClick}>{formatMediaDateTime(oldLang, timestamp * 1000, true)}</Link>
            </>
          )}
        </div>
      </div>

      {sender && Boolean(timestamp) && (
        <Link onClick={onDateClick}>{formatPastTimeShort(oldLang, timestamp * 1000)}</Link>
      )}
    </div>
  );
};

export default memo(PdfPreview);
