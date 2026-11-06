import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';

const script = readFileSync(new URL(
  '../entry/src/main/resources/rawfile/selection-menu.js', import.meta.url), 'utf8');

function fixture(t, native = true) {
  const dom = new JSDOM(`<!doctype html><html><head></head><body>
    <div id="text" class="markdown-body">Selected message</div>
    <div id="toolbar" class="quote-question-bar"><div class="quote-bar-toolbar">Copy Quote Discuss</div></div>
    <div id="composer" class="quote-question-bar"><div class="quote-bar-expanded"><textarea></textarea></div></div>
    <div id="editor" class="cm-editor">Editor selection</div>
    <div id="terminal" class="xterm">Terminal selection</div>
    <div id="input" contenteditable="true">Editable text</div>
  </body></html>`, { runScripts: 'outside-only', url: 'https://fixture.example/' });
  t.after(() => dom.window.close());
  const { window } = dom;
  const { document } = window;
  if (native) window.ClawBenchHarmony = {};
  window.eval(script);
  document.dispatchEvent(new window.Event('DOMContentLoaded'));
  return {
    window, document,
    select(id) {
      const range = document.createRange();
      range.selectNodeContents(document.getElementById(id));
      window.getSelection().removeAllRanges();
      window.getSelection().addRange(range);
      document.dispatchEvent(new window.Event('selectionchange'));
    },
    display(id) { return window.getComputedStyle(document.getElementById(id)).display; }
  };
}

test('DOM selection hides only the collapsed web toolbar while preserving the selection and composer', t => {
  const h = fixture(t);
  h.select('text');
  assert.equal(h.display('toolbar'), 'none');
  assert.notEqual(h.display('composer'), 'none');
  assert.equal(h.window.getSelection().toString(), 'Selected message');
  h.window.getSelection().removeAllRanges();
  h.document.dispatchEvent(new h.window.Event('selectionchange'));
  assert.notEqual(h.display('toolbar'), 'none');
});

test('editor, terminal and editable selections retain their programmatic web toolbar', t => {
  const h = fixture(t);
  for (const id of ['editor', 'terminal', 'input', 'composer']) {
    h.select('text');
    h.select(id);
    assert.notEqual(h.display('toolbar'), 'none', id);
  }
});

test('selection compatibility is inactive without the native host and is disposed on navigation', t => {
  const web = fixture(t, false);
  web.select('text');
  assert.notEqual(web.display('toolbar'), 'none');
  const h = fixture(t);
  h.select('text');
  h.window.dispatchEvent(new h.window.Event('pagehide'));
  assert.notEqual(h.display('toolbar'), 'none');
  h.select('text');
  assert.equal(h.document.documentElement.hasAttribute('data-harmony-dom-selection'), false);
});
