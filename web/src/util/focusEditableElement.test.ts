import focusEditableElement from './focusEditableElement';

describe('focusEditableElement', () => {
  function createEditableElement(text = '') {
    const element = document.createElement('div');
    element.contentEditable = 'true';
    element.tabIndex = 0;
    element.textContent = text;
    document.body.appendChild(element);
    return element;
  }

  afterEach(() => {
    document.body.innerHTML = '';
    window.getSelection()?.removeAllRanges();
  });

  it('focuses the editable element before restoring the caret', () => {
    const element = createEditableElement('hello');

    focusEditableElement(element, true, true);

    const selection = window.getSelection()!;

    expect(document.activeElement).toBe(element);
    expect(selection.rangeCount).toBe(1);
    expect(selection.anchorNode?.textContent).toBe('hello');
    expect(selection.anchorOffset).toBe(1);
  });

  it('keeps an empty editable element focused', () => {
    const element = createEditableElement();

    focusEditableElement(element, true, true);

    const selection = window.getSelection()!;

    expect(document.activeElement).toBe(element);
    expect(selection.rangeCount).toBe(1);
    expect(selection.anchorNode).toBe(element);
  });
});
