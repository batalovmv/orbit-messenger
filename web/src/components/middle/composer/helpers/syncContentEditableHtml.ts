import focusEditableElement from '../../../../util/focusEditableElement';
import { getCaretPosition, setCaretPosition } from '../../../../util/selection';
import { isSelectionInsideInput } from './selection';

type Args = {
  element: HTMLElement;
  html: string;
  inputId: string;
};

export default function syncContentEditableHtml({ element, html, inputId }: Args) {
  if (html === element.innerHTML) {
    return;
  }

  const selection = window.getSelection();
  const shouldRestoreCaret = Boolean(
    document.activeElement === element
      && selection?.rangeCount
      && selection.isCollapsed
      && isSelectionInsideInput(selection.getRangeAt(0), inputId),
  );
  const caretPosition = shouldRestoreCaret ? getCaretPosition(element) : undefined;

  element.innerHTML = html;

  if (!shouldRestoreCaret) {
    return;
  }

  if (!html) {
    focusEditableElement(element, true, true);
    return;
  }

  focusEditableElement(element, true, true);

  if (caretPosition === undefined) {
    return;
  }

  if (setCaretPosition(element, caretPosition) !== -1) {
    focusEditableElement(element, true, true);
  }
}
