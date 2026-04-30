import { getPhase, setPhase } from '../../../../lib/fasterdom/fasterdom';
import focusEditableElement from '../../../../util/focusEditableElement';
import { getCaretPosition, setCaretPosition } from '../../../../util/selection';
import { isSelectionInsideInput } from './selection';

type Args = {
  element: HTMLElement;
  html: string;
  inputId: string;
};

// Called from useLayoutEffect (mutate phase) but needs DOM reads and focus().
// Temporarily switch phase to allow mixed read/write operations.
function withMeasureIfNeeded<T>(cb: () => T): T {
  const currentPhase = getPhase();
  if (currentPhase === 'measure') return cb();

  setPhase('measure');
  const result = cb();
  setPhase(currentPhase);
  return result;
}

export default function syncContentEditableHtml({ element, html, inputId }: Args) {
  if (html === element.innerHTML) {
    return;
  }

  const { shouldRestoreCaret, caretPosition } = withMeasureIfNeeded(() => {
    const selection = window.getSelection();
    const shouldRestore = Boolean(
      document.activeElement === element
      && selection?.rangeCount
      && selection.isCollapsed
      && isSelectionInsideInput(selection.getRangeAt(0), inputId),
    );
    const caret = shouldRestore ? getCaretPosition(element) : undefined;
    return { shouldRestoreCaret: shouldRestore, caretPosition: caret };
  });

  element.innerHTML = html;

  if (!shouldRestoreCaret) {
    return;
  }

  withMeasureIfNeeded(() => {
    if (!html) {
      focusEditableElement(element, true, true);
      return;
    }

    if (caretPosition === undefined) {
      focusEditableElement(element, true, true);
      return;
    }

    focusEditableElement(element, true);

    if (setCaretPosition(element, caretPosition) !== -1) {
      focusEditableElement(element, true, true);
    }
  });
}
