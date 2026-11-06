import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';

const code = readFileSync(new URL('../entry/src/main/resources/rawfile/harmony-bootstrap.js', import.meta.url), 'utf8');
function createPage({ origin = 'https://example.com', path = '/', subframe = false } = {}) {
  const events = new Map();
  const timers = new Map();
  let timerId = 0;
  const window = {
    addEventListener(name, handler) { events.set(name, handler); }
  };
  window.top = subframe ? {} : window;
  runInNewContext(code.replace('__HARMONY_CONFIG__', JSON.stringify({
    origin: 'https://example.com', channel: 'random-channel', language: 'zh', theme: 'gruvbox-dark', version: '0.1.2'
  })), {
    window, location: { origin, pathname: path },
    setTimeout(handler) { timers.set(++timerId, handler); return timerId; },
    clearTimeout(id) { timers.delete(id); }
  });
  return { window, events, timers };
}

function connectPort(page, channel = 'random-channel') {
  const messages = [];
  const port = {
    start() {},
    close() { this.closed = true; },
    postMessage(message) { messages.push(JSON.parse(message)); }
  };
  page.events.get('message')({ data: '__clawbench_harmony_port__:' + channel, ports: [port] });
  return { port, messages };
}

test('bootstrap exposes no bridge to subframes, external sites or file previews', () => {
  for (const options of [{ subframe: true }, { origin: 'https://evil.example' }, { path: '/api/fs/raw/untrusted.html' }]) {
    assert.equal(createPage(options).window.ClawBenchHarmony, undefined);
  }
});

test('HarmonyOS has its own name; saved passwords never enter the facade', async () => {
  const { window } = createPage();
  assert.equal(window.ClawBenchNative, undefined);
  assert.equal(window.ClawBenchHarmony.getHostPlatform(), 'harmonyos');
  assert.equal(await window.ClawBenchHarmony.getPassword(), '');
  assert.equal(window.ClawBenchHarmony.getCapabilities().tunnel, false);
  assert.throws(() => window.ClawBenchHarmony.setNativePushEnabled(true), /not implemented/);
});

test('queues requests and accepts only the per-connection channel', async () => {
  const page = createPage();
  const result = page.window.ClawBenchHarmony.getServerList();
  const forged = connectPort(page, 'forged-channel');
  assert.equal(forged.messages.length, 0);
  const { port, messages } = connectPort(page);
  assert.equal(messages.length, 1);
  assert.equal(messages[0].method, 'getServerList');
  port.onmessage({ data: JSON.stringify({ id: messages[0].id, result: JSON.stringify('[]') }) });
  assert.equal(await result, '[]');
  assert.equal(page.timers.size, 0);
});

test('expired requests are never replayed when a late port arrives', async () => {
  const page = createPage();
  const result = page.window.ClawBenchHarmony.saveServer('https://example.com', 'fixture-password');
  const rejected = assert.rejects(result, /timed out/);
  for (const timeout of page.timers.values()) timeout();
  await rejected;
  assert.equal(connectPort(page).messages.length, 0);
});

test('navigation rejects pending operations and closes the channel', async () => {
  const page = createPage();
  const { port } = connectPort(page);
  const result = page.window.ClawBenchHarmony.getServerUrl();
  const rejected = assert.rejects(result, /page closed/);
  page.events.get('pagehide')();
  await rejected;
  assert.equal(port.closed, true);
  assert.equal(page.timers.size, 0);
});
