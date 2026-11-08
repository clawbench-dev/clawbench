import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { runInNewContext } from 'node:vm';
import { redactLog } from '../entry/src/main/ets/models/NativeActions.ts';

const source = readFileSync(new URL('../entry/src/main/ets/services/NativeLogService.ets', import.meta.url), 'utf8');
const code = stripTypeScriptTypes(source.replace(/^import .*;\r?\n/gm, '').replace('export class', 'class')) +
  '\nNativeLogService;';

function service({ cookiesUnavailable = false } = {}) {
  const timers = new Set();
  const requests = [];
  const local = [];
  const NativeLogService = runInNewContext(code, {
    redactLog, nativeCookieHeader: () => 'scoped-session=fixture',
    webview: { WebCookieManager: { fetchCookieSync: () => {
      if (cookiesUnavailable) throw new Error('Cookie store is unavailable');
      return 'unrelated=private';
    } } },
    hilog: { info: (...args) => local.push(args) },
    setInterval: fn => { timers.add(fn); return fn; }, clearInterval: fn => timers.delete(fn),
    http: { RequestMethod: { POST: 'POST' }, createHttp: () => {
      const request = { destroy() { this.destroyed = true; },
        request(url, options) { requests.push({ url, options, request }); return Promise.resolve({ responseCode: 200 }); } };
      return request;
    } }
  });
  return { instance: new NativeLogService('https://example.com', ['fixture-password']), timers, requests, local };
}

test('native diagnostic capture is off by default and disabling discards queued logs', async () => {
  const s = service();
  s.instance.log('I', 'Test', 'off');
  assert.equal(s.local.length, 0);
  assert.equal(s.timers.size, 0);
  s.instance.start();
  s.instance.start();
  assert.equal(s.timers.size, 1);
  s.instance.log('I', 'Test', 'queued');
  s.instance.stop();
  assert.equal(s.timers.size, 0);
  s.instance.start();
  for (const timer of s.timers) await timer();
  assert.equal(s.requests.length, 0);
});

test('logs use bounded batches, scoped authentication and no redirects', async () => {
  const s = service();
  s.instance.start();
  for (let i = 0; i < 120; i++) s.instance.log('I', 'Test', `entry ${i} fixture-password`);
  for (const timer of s.timers) await timer();
  assert.equal(s.requests.length, 1);
  const { url, options } = s.requests[0];
  assert.equal(url, 'https://example.com/api/client-log');
  assert.equal(options.header.Cookie, 'scoped-session=fixture');
  assert.equal(options.maxRedirects, 0);
  const entries = JSON.parse(options.extraData).entries;
  assert.equal(entries.length, 100);
  assert.match(entries[0].msg, /^entry 20/);
  assert.equal(options.extraData.includes('fixture-password'), false);
  assert.equal(entries[0].source, 'harmonyos');
  assert.equal(s.local.some(args => JSON.stringify(args).includes('fixture-password')), false);
  s.instance.dispose();
  assert.equal(s.timers.size, 0);
});

test('cookie store failures discard diagnostics without an unhandled timer rejection', async () => {
  const s = service({ cookiesUnavailable: true });
  s.instance.start();
  s.instance.log('I', 'Test', 'queued');
  for (const timer of s.timers) await assert.doesNotReject(timer);
  assert.equal(s.requests.length, 0);
  assert.equal(s.instance.queue.length, 0);
  s.instance.dispose();
});

test('a rotated password is redacted immediately from subsequent local and uploaded diagnostics', async () => {
  const s = service();
  s.instance.start();
  s.instance.addSecret('rotated-fixture-secret');
  s.instance.addSecret('rotated-fixture-secret');
  s.instance.addSecret('');
  s.instance.log('I', 'rotated-fixture-secret', 'old fixture-password and new rotated-fixture-secret');
  assert.equal(s.local.length, 1);
  assert.doesNotMatch(JSON.stringify(s.local), /fixture-password|rotated-fixture-secret/);
  assert.match(JSON.stringify(s.local), /\[redacted\]/);
  for (const timer of s.timers) await timer();
  assert.equal(s.requests.length, 1);
  assert.doesNotMatch(s.requests[0].options.extraData, /fixture-password|rotated-fixture-secret/);
  s.instance.dispose();
});

test('registering a password also scrubs queued diagnostics before their next upload', async () => {
  const s = service();
  s.instance.start();
  s.instance.log('I', 'new-fixture-secret', 'queued new-fixture-secret');
  assert.equal(s.requests.length, 0);
  s.instance.addSecret('new-fixture-secret');
  for (const timer of s.timers) await timer();
  assert.equal(s.requests.length, 1);
  const entries = JSON.parse(s.requests[0].options.extraData).entries;
  assert.equal(entries[0].tag, '[redacted]');
  assert.equal(entries[0].msg, 'queued [redacted]');
  s.instance.dispose();
});
