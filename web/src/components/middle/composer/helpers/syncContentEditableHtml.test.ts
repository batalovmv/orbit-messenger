import { getCaretPosition } from '../../../../util/selection';
import syncContentEditableHtml from './syncContentEditableHtml';

describe('syncContentEditableHtml', () => {
  function createEditableElement(text: string) {
    const element = document.createElement('div');
    element.id = 'editable-message-text';
    element.contentEditable = 'true';
    element.tabIndex = 0;
    element.textContent = text;
    document.body.appendChild(element);
    return element;
  }

  function setCollapsedCaret(element: HTMLElement, position: number) {
    const textNode = element.firstChild as Text;
    const range = document.createRange();
    const selection = window.getSelection()!;

    range.setStart(textNode, position);
    range.collapse(true);
    selection.removeAllRanges();
    selection.addRange(range);
  }

  afterEach(() => {
    document.body.innerHTML = '';
    window.getSelection()?.removeAllRanges();
  });

  it('restores the caret when html changes while the editable is focused', () => {
    const element = createEditableElement('hello world');
    element.focus();
    setCollapsedCaret(element, 5);

    syncContentEditableHtml({
      element,
      html: 'hello brave world',
      inputId: element.id,
    });

    expect(document.activeElement).toBe(element);
    expect(getCaretPosition(element)).toBe(5);
  });

  it('keeps the editable focused after programmatic clearing', () => {
    const element = createEditableElement('hello');
    element.focus();
    setCollapsedCaret(element, 5);

    syncContentEditableHtml({
      element,
      html: '',
      inputId: element.id,
    });

    const selection = window.getSelection()!;

    expect(document.activeElement).toBe(element);
    expect(element.innerHTML).toBe('');
    expect(selection.rangeCount).toBe(1);
  });
});
