import { IS_TOUCH_ENV } from './browser/windowEnvironment';

export default function focusEditableElement(element: HTMLElement, force?: boolean, forcePlaceCaretAtEnd?: boolean) {
  if (!force && element === document.activeElement) {
    return;
  }

  const lastChild = element.lastChild || element;

  element.focus();

  const selection = window.getSelection();
  if (!selection) {
    return;
  }

  if (!IS_TOUCH_ENV && !forcePlaceCaretAtEnd && (!lastChild || !lastChild.nodeValue)) {
    return;
  }

  const range = document.createRange();
  range.selectNodeContents(forcePlaceCaretAtEnd ? element : lastChild);
  // `false` means collapse to the end rather than the start
  range.collapse(false);
  selection.removeAllRanges();
  selection.addRange(range);
}
