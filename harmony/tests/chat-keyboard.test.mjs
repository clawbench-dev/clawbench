import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';

const script = readFileSync(new URL(
  '../entry/src/main/resources/rawfile/chat-keyboard.js', import.meta.url), 'utf8');

function page(t, { host = true } = {}) {
  const dom = new JSDOM('<textarea class="chat-textarea"></textarea><textarea id="editor"></textarea><button>Send</button>', {
    runScripts: 'outside-only'
  });
  t.after(() => dom.window.close());
  const { window } = dom;
  if (host) window.ClawBenchHarmony = {};
  let now = 10;
  window.performance.now = () => now;
  window.eval(script);
  const input = window.document.querySelector('.chat-textarea');
  input.focus();
  input.value = 'first line';
  const sends = [];
  input.addEventListener('keydown', event => {
    if (event.isComposing || event.keyCode === 229) return;
    if (event.key === 'Enter' && !event.shiftKey && !event.ctrlKey && !event.metaKey && !event.altKey) {
      event.preventDefault();
      sends.push(input.value);
    }
  });
  window.document.querySelector('button').onclick = () => sends.push(input.value);
  function enter(options = {}, target = input) {
    const event = new window.KeyboardEvent('keydown', {
      key: 'Enter', code: 'Enter', keyCode: 13, bubbles: true, cancelable: true, ...options
    });
    target.dispatchEvent(event);
    return event;
  }
  return { window, input, sends, enter, advance: () => { now += 1001; } };
}

test('ArkWeb soft Enter bypasses legacy send while keeping the native editing default', t => {
  const h = page(t);
  const event = h.enter();
  assert.deepEqual(h.sends, []);
  assert.equal(event.defaultPrevented, false);
  assert.equal(h.input.value, 'first line');
  // jsdom does not perform textarea default editing; a real ArkWeb input event
  // must still reach Vue's v-model after the browser inserts its newline.
  let model = '';
  h.input.addEventListener('input', () => { model = h.input.value; });
  h.input.value += '\nsecond line';
  h.input.dispatchEvent(new h.window.InputEvent('input', { bubbles: true, inputType: 'insertLineBreak' }));
  assert.equal(model, 'first line\nsecond line');
  h.window.document.querySelector('button').click();
  assert.deepEqual(h.sends, ['first line\nsecond line']);
});

test('native hardware markers preserve Enter send and are consumed by one key only', t => {
  const h = page(t);
  h.window.__clawbenchHardwareEnter();
  assert.equal(h.enter().defaultPrevented, true);
  assert.deepEqual(h.sends, ['first line']);
  h.enter();
  assert.equal(h.sends.length, 1);
});

test('IME candidate selection, modifier shortcuts and non-chat editors keep their handlers', t => {
  const h = page(t);
  let calls = 0;
  h.input.addEventListener('keydown', () => { calls++; });
  for (const options of [{ isComposing: true }, { keyCode: 229 }, { shiftKey: true },
    { ctrlKey: true }, { metaKey: true }, { altKey: true }]) {
    assert.equal(h.enter(options).defaultPrevented, false);
  }
  assert.equal(calls, 6);
  assert.deepEqual(h.sends, []);
  const editor = h.window.document.querySelector('#editor');
  let editorCalls = 0;
  editor.addEventListener('keydown', () => { editorCalls++; });
  h.enter({}, editor);
  assert.equal(editorCalls, 1);
});

test('old, unfocused and interrupted hardware markers cannot turn soft Enter into send', t => {
  for (const reset of [
    h => h.advance(),
    h => h.window.dispatchEvent(new h.window.Event('blur')),
    h => h.enter({ key: 'a', code: 'KeyA', keyCode: 65 }),
    h => { h.window.document.querySelector('#editor').focus(); h.window.__clawbenchHardwareEnter(); h.input.focus(); }
  ]) {
    const h = page(t);
    h.window.__clawbenchHardwareEnter();
    reset(h);
    h.enter();
    assert.deepEqual(h.sends, []);
  }
});

test('page exit removes the compatibility handler and native marker', t => {
  const h = page(t);
  h.window.dispatchEvent(new h.window.Event('pagehide'));
  assert.equal(h.window.__clawbenchHardwareEnter, undefined);
  h.enter();
  assert.equal(h.sends.length, 1);
});

test('ordinary browser pages do not install HarmonyOS keyboard handling', t => {
  const h = page(t, { host: false });
  assert.equal(h.window.__clawbenchHardwareEnter, undefined);
  h.enter();
  assert.equal(h.sends.length, 1);
});
