import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import jsdom from 'jsdom';
import { normalizeBarColor, barContentColor } from '../entry/src/main/ets/models/SystemBars.ts';
import { mergeSafeInsets } from '../entry/src/main/ets/models/SafeInsets.ts';

const script = readFileSync(new URL('../entry/src/main/resources/rawfile/system-bars.js', import.meta.url), 'utf8');

test('system, cutout and gesture insets overlap instead of being added twice', () => {
  assert.deepEqual(mergeSafeInsets([
    { top: 48, bottom: 0, left: 0, right: 0 },
    { top: 30, bottom: 0, left: 0, right: 0 },
    { top: 0, bottom: 24, left: 0, right: 0 }
  ], false), { top: 48, bottom: 24, left: 0, right: 0 });
});

test('rotation preserves side cutouts and keyboard resize removes the extra bottom reservation', () => {
  const areas = [{ top: 0, bottom: 24, left: 48, right: 12 }];
  assert.deepEqual(mergeSafeInsets(areas, false), { top: 0, bottom: 24, left: 48, right: 12 });
  assert.deepEqual(mergeSafeInsets(areas, true), { top: 0, bottom: 0, left: 48, right: 12 });
});

test('system bar icons contrast with light and dark themes and reject non-color input', () => {
  assert.equal(normalizeBarColor('#aabbcc'), '#AABBCC');
  assert.equal(barContentColor('#FFFFFF'), '#000000');
  assert.equal(barContentColor('#1d2021'), '#FFFFFF');
  for (const color of ['red', 'transparent', '#00000000', 'url(https://example.com)', '#ffffff;']) {
    assert.throws(() => normalizeBarColor(color), /Invalid/);
  }
});

function createPage(t, { dockVisible = true, hostAvailable = true } = {}) {
  const dom = new jsdom.JSDOM('<body style="background:#1d2021"><header class="header" style="background:#282828"></header><div class="bottom-dock" style="background:#333333"></div></body>', {
    runScripts: 'outside-only'
  });
  t.after(() => dom.window.close());
  const { window } = dom;
  const calls = [];
  const frames = [];
  window.requestAnimationFrame = callback => frames.push(callback);
  const dock = window.document.querySelector('.bottom-dock');
  dock.getBoundingClientRect = () => ({ height: dockVisible ? 50 : 0 });
  if (hostAvailable) {
    window.ClawBenchHarmony = { setSystemBarAppearance: (...colors) => { calls.push(colors); return Promise.resolve(); } };
  }
  window.eval(script);
  function flush() {
    while (frames.length) frames.shift()();
  }
  flush();
  return { window, calls, flush };
}

test('system bars follow the actual header and dock without repeated native writes', async t => {
  const { window, calls, flush } = createPage(t);
  assert.deepEqual(calls, [['#282828', '#333333']]);
  window.dispatchEvent(new window.Event('resize'));
  flush();
  assert.equal(calls.length, 1);
  window.document.querySelector('.header').setAttribute('style', 'background:#fafafa');
  await new Promise(setImmediate);
  flush();
  assert.deepEqual(calls.at(-1), ['#fafafa', '#333333']);
});

test('hidden keyboard dock falls back to the page background', t => {
  const { calls } = createPage(t, { dockVisible: false });
  assert.deepEqual(calls, [['#282828', '#1d2021']]);
});

test('leaving the page removes the theme observer and resize listener', async t => {
  const { window, calls, flush } = createPage(t);
  window.dispatchEvent(new window.Event('pagehide'));
  window.document.querySelector('.header').setAttribute('style', 'background:#ffffff');
  window.dispatchEvent(new window.Event('resize'));
  await new Promise(setImmediate);
  flush();
  assert.equal(calls.length, 1);
});

test('ordinary pages without the trusted host do not get a native appearance bridge', t => {
  assert.deepEqual(createPage(t, { hostAvailable: false }).calls, []);
});
