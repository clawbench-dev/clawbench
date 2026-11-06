import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { runInNewContext } from 'node:vm';
import * as transferPolicy from '../entry/src/main/ets/models/FileTransferPolicy.ts';
import { parseServerAddress } from '../entry/src/main/ets/models/ServerAddress.ts';
import { NativeFailure, nativeMessage } from '../entry/src/main/ets/models/NativeMessages.ts';

const bootstrap = readFileSync(new URL(
  '../entry/src/main/resources/rawfile/harmony-bootstrap.js', import.meta.url
), 'utf8');
const serviceSource = readFileSync(new URL(
  '../entry/src/main/ets/services/FileTransferService.ets', import.meta.url
), 'utf8').replace(/^import[\s\S]*?;\r?\n/gm, '').replace('export class FileTransferService', 'class FileTransferService');
const serviceCode = stripTypeScriptTypes(serviceSource, { mode: 'strip' }) + '\nglobalThis.Service = FileTransferService;';
const tick = () => new Promise(resolve => setImmediate(resolve));

function createPage(channel = 'first-document') {
  const listeners = new Map();
  const events = [];
  const timers = new Map();
  let nextTimer = 0;
  const window = {
    addEventListener(name, callback) {
      if (!listeners.has(name)) listeners.set(name, []);
      listeners.get(name).push(callback);
    },
    dispatchEvent(event) {
      events.push(event);
      for (const callback of listeners.get(event.type) || []) callback(event);
    }
  };
  window.top = window;
  const context = {
    window, location: { origin: 'https://example.com', pathname: '/' },
    navigator: {},
    CustomEvent: class {
      constructor(type, options) { this.type = type; this.detail = options?.detail; }
    },
    setTimeout(callback) { timers.set(++nextTimer, callback); return nextTimer; },
    clearTimeout(id) { timers.delete(id); }
  };
  runInNewContext(bootstrap.replace('__HARMONY_CONFIG__', JSON.stringify({
    origin: 'https://example.com', channel, language: 'en', theme: 'dark', version: 'fixture'
  })), context);
  const page = {
    bridge: window.ClawBenchHarmony, events, timers,
    close() { window.dispatchEvent({ type: 'pagehide' }); },
    connect(port, offeredChannel = channel) {
      window.dispatchEvent({ type: 'message', data: '__clawbench_harmony_port__:' + offeredChannel, ports: [port] });
    }
  };
  return page;
}

function createPort(onRequest = () => {}) {
  return {
    messages: [], started: false, closed: false,
    start() { this.started = true; },
    close() { this.closed = true; },
    postMessage(message) {
      const request = JSON.parse(message);
      this.messages.push(request);
      onRequest(request);
    },
    receive(response) { this.onmessage?.({ data: JSON.stringify(response) }); },
    raw(message) { this.onmessage?.({ data: message }); }
  };
}

function downloadEvents(page) {
  return page.events.filter(event => event.type === 'clawbench-download-progress');
}

test('download facade returns void: acceptance ACK never prematurely completes the progress UI', async () => {
  const page = createPage();
  const port = createPort();
  page.connect(port);
  assert.equal(page.bridge.downloadFileWithProgress('report.txt', '报告.txt', 71), undefined);
  const request = port.messages[0];
  assert.equal(request.method, 'downloadFileWithProgress');
  assert.deepEqual(request.args.map(value => JSON.parse(value)), ['report.txt', '报告.txt', 71]);
  port.receive({ id: request.id, result: 'null' });
  await tick();
  assert.equal(downloadEvents(page).length, 0);
  assert.equal(page.timers.size, 0);
  port.receive({ event: 'clawbench-download-progress',
    detail: { id: 71, received: 5, total: 10, done: false, error: '', cancelled: false } });
  assert.equal(downloadEvents(page).at(-1).detail.done, false);
  port.receive({ event: 'clawbench-download-progress',
    detail: { id: 71, received: 10, total: 10, done: true, error: '', cancelled: false } });
  assert.equal(downloadEvents(page).filter(event => event.detail.done).length, 1);
  page.close();
});

test('native rejection emits one terminal failure while cancellation preserves its distinct state', async () => {
  const page = createPage();
  const port = createPort();
  page.connect(port);
  page.bridge.downloadFileWithProgress('one.txt', '', 1);
  port.receive({ id: port.messages[0].id, error: 'Storage unavailable' });
  await tick();
  assert.equal(downloadEvents(page).length, 1);
  assert.equal(downloadEvents(page)[0].detail.done, true);
  assert.equal(Boolean(downloadEvents(page)[0].detail.error), true);
  port.receive({ event: 'clawbench-download-progress',
    detail: { id: 2, received: 4, total: 100, done: true, error: '', cancelled: true } });
  assert.equal(downloadEvents(page).at(-1).detail.cancelled, true);
  page.close();
});

test('message port events have an allowlist and ignore malformed messages without breaking later replies', async () => {
  const page = createPage();
  const port = createPort();
  page.connect(port);
  const operation = page.bridge.getServerUrl();
  for (const message of ['null', '[]', '7', '"text"', '{broken']) {
    assert.doesNotThrow(() => port.raw(message));
  }
  for (const response of [
    { event: 'clawbench-run-arbitrary-script', detail: { source: 'fixture' } },
    { event: 'clawbench-selection-action', detail: null },
    { event: 'clawbench-download-progress', detail: [] }
  ]) {
    assert.doesNotThrow(() => port.receive(response));
  }
  assert.equal(downloadEvents(page).length, 0);
  assert.equal(page.events.some(event => event.type === 'clawbench-run-arbitrary-script'), false);
  port.receive({ id: port.messages[0].id, result: JSON.stringify('https://example.com') });
  assert.equal(await operation, 'https://example.com');
  port.receive({ event: 'clawbench-share-imported',
    detail: { paths: ['.clawbench/share-in/report.txt'], projectPath: '/fixture', failed: 0 } });
  assert.equal(page.events.at(-1).type, 'clawbench-share-imported');
  page.close();
});

test('pagehide rejects pending work, refuses late ports, and prevents new native submissions', async () => {
  const page = createPage();
  const operation = page.bridge.shareText('fixture');
  const rejection = assert.rejects(operation, /page closed/);
  page.close();
  await rejection;
  const late = createPort();
  page.connect(late);
  assert.equal(late.started, false);
  assert.equal(late.messages.length, 0);
  await assert.rejects(page.bridge.getServerUrl(), /page closed/);
  assert.equal(page.bridge.downloadFileWithProgress('late.txt', '', 9), undefined);
  await tick();
  assert.equal(downloadEvents(page).length, 0);
  assert.equal(page.timers.size, 0);
});

test('changing documents disposes a real file service and late transfer events cannot enter the new page', async () => {
  const first = createPage('old-channel');
  const nativeCallbacks = {};
  const openDescriptors = new Set();
  const temporary = new Set();
  let destroyed = false;
  let requestStarted = false;
  const request = {
    on(name, callback) { nativeCallbacks[name] = callback; },
    destroy() { destroyed = true; },
    requestInStream() { requestStarted = true; return new Promise(() => {}); }
  };
  const context = {
    ...transferPolicy, parseServerAddress, NativeFailure, nativeMessage,
    nativeCookieHeader: (_origin, header) => header,
    webview: { WebCookieManager: { fetchCookieSync: () => 'clawbench_session=fixture; clawbench_project=fixture' } },
    util: { generateRandomUUID: () => 'fixture' },
    fs: {
      OpenMode: { CREATE: 1, WRITE_ONLY: 2, TRUNC: 4 },
      accessSync: () => true, listFileSync: () => [],
      async open(path) { temporary.add(path); openDescriptors.add(7); return { fd: 7 }; },
      async close(file) { openDescriptors.delete(file.fd); },
      unlinkSync(path) { temporary.delete(path); }
    },
    picker: {
      DocumentSaveOptions: class {},
      DocumentViewPicker: class { async save() { return ['file://docs/Downloads/result.txt']; } }
    },
    http: { RequestMethod: { GET: 'GET' }, createHttp: () => request }
  };
  runInNewContext(serviceCode, context);
  let service;
  const oldPort = createPort(request => {
    const args = request.args.map(value => JSON.parse(value));
    service.startDownload(...args);
    oldPort.receive({ id: request.id, result: 'null' });
  });
  first.connect(oldPort);
  service = new context.Service({ cacheDir: '/cache' }, 'https://example.com', progress => {
    oldPort.receive({ event: 'clawbench-download-progress', detail: progress });
  });
  first.bridge.downloadFileWithProgress('result.txt', '', 22);
  await tick();
  assert.equal(requestStarted, true);
  assert.equal(openDescriptors.size, 1);
  const eventsBeforeNavigation = downloadEvents(first).length;
  first.close();
  service.dispose();
  const second = createPage('new-channel');
  second.connect(oldPort, 'old-channel');
  const newPort = createPort();
  second.connect(newPort);
  nativeCallbacks.dataReceive(new ArrayBuffer(3));
  nativeCallbacks.dataEnd();
  oldPort.receive({ event: 'clawbench-download-progress', detail: { id: 22, done: true } });
  await tick();
  assert.equal(destroyed, true);
  assert.equal(openDescriptors.size, 0);
  assert.equal(temporary.size, 0);
  assert.equal(downloadEvents(first).length, eventsBeforeNavigation);
  assert.equal(downloadEvents(second).length, 0);
  assert.equal(oldPort.closed, true);
  assert.equal(newPort.started, true);
  second.close();
});
