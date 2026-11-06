(() => {
  'use strict';
  if (window !== window.top || !window.ClawBenchHarmony) return;
  const attribute = 'data-harmony-dom-selection';
  const editable = 'input,textarea,[contenteditable="true"],.cm-editor,.xterm,.quote-question-bar';
  let style;
  let closed = false;
  function element(node) {
    return node?.nodeType === 1 ? node : node?.parentElement;
  }
  function update() {
    const selection = window.getSelection();
    const readonly = selection && !selection.isCollapsed && selection.toString().trim() &&
      !element(selection.anchorNode)?.closest(editable) && !element(selection.focusNode)?.closest(editable);
    document.documentElement.toggleAttribute(attribute, Boolean(readonly));
  }
  function initialize() {
    if (closed || style) return;
    // Older servers do not recognize the HarmonyOS host and still draw the
    // collapsed web toolbar under ArkWeb's menu. Keep the expanded composer.
    style = document.createElement('style');
    style.textContent = `html[${attribute}] .quote-question-bar:has(> .quote-bar-toolbar) { display: none !important; }`;
    document.head.appendChild(style);
    document.addEventListener('selectionchange', update);
    update();
  }
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initialize, { once: true });
  } else {
    initialize();
  }
  window.addEventListener('pagehide', () => {
    closed = true;
    document.removeEventListener('DOMContentLoaded', initialize);
    document.removeEventListener('selectionchange', update);
    document.documentElement.removeAttribute(attribute);
    style?.remove();
  }, { once: true });
})();
