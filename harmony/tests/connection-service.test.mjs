import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { runInNewContext } from 'node:vm';
import { compatibleProtocol, parseServerAddress } from '../entry/src/main/ets/models/ServerAddress.ts';
import { nativeMessage, NativeFailure, formatNativeError } from '../entry/src/main/ets/models/NativeMessages.ts';

const source = readFileSync(new URL(
  '../entry/src/main/ets/services/ConnectionService.ets', import.meta.url
), 'utf8').replace(/^import .*;\r?\n/gm, '').replaceAll('export class ', 'class ');
const production = stripTypeScriptTypes(source, { mode: 'strip' }) +
  '\n({ ConnectionService, ConnectionFailure });';
const healthResponse = { responseCode: 200, result: '{"app":"clawbench","protocol":1}' };

function fixture(steps) {
  const calls = [];
  const requests = [];
  const cookies = [];
  const registrations = [];
  let cookieSaves = 0;
  const http = {
    RequestMethod: { GET: 'GET', POST: 'POST' },
    HttpDataType: { STRING: 'STRING' },
    createHttp() {
      const request = {
        destroys: 0,
        destroy() { this.destroys++; },
        async request(url, options) {
          calls.push({ url, options });
          assert.ok(steps.length > 0, 'Every native HTTP call must have an explicit fixture');
          const step = steps.shift();
          return typeof step === 'function' ? step(url, options) : step;
        }
      };
      requests.push(request);
      return request;
    }
  };
  const webview = {
    WebCookieManager: {
      configCookieSync(url, cookie) { cookies.push({ url, cookie }); },
      saveCookieSync() { cookieSaves++; }
    }
  };
  const { ConnectionService, ConnectionFailure } = runInNewContext(production, {
    http, webview, compatibleProtocol, parseServerAddress, nativeMessage,
    registerServerCookies(url, values) { registrations.push({ url, values: Array.from(values) }); }
  });
  return {
    service: new ConnectionService(), ConnectionFailure, calls, requests, cookies, registrations,
    get cookieSaves() { return cookieSaves; }
  };
}

function rejectedWith(harness, code, language) {
  return error => {
    assert.ok(error instanceof harness.ConnectionFailure);
    assert.equal(error.code, code);
    assert.equal(error.message, nativeMessage(code, language));
    return true;
  };
}

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

for (const language of ['zh', 'en']) {
  test(`invalid health responses do not submit a password (${language})`, async () => {
    const invalid = [
      '<html>Proxy response</html>', '', 'null', '[]', 'true', '"clawbench"', '42',
      '{}', '{"app":"other","protocol":1}', '{"app":{},"protocol":1}',
      '{"app":"clawbench","protocol":"1"}', '{"app":"clawbench","protocol":1.5}',
      '{"app":"clawbench","protocol":null}', '{"app":"clawbench","protocol":1,"version":123}'
    ];
    for (const result of invalid) {
      const h = fixture([{ responseCode: 200, result }]);
      await assert.rejects(h.service.connect('https://fixture.example', 'fixture-only', language),
        rejectedWith(h, 'invalid_server', language));
      assert.equal(h.calls.length, 1);
      assert.equal(h.calls[0].options.method, 'GET');
      assert.equal(h.calls[0].options.extraData, '');
      assert.equal(h.cookies.length, 0);
      assert.equal(h.requests[0].destroys, 1);
    }
  });

  test(`protocol mismatches stay distinct from invalid server responses (${language})`, async () => {
    for (const result of [
      '{"app":"clawbench"}', '{"app":"clawbench","protocol":0}', '{"app":"clawbench","protocol":2}'
    ]) {
      const h = fixture([{ responseCode: 200, result }]);
      await assert.rejects(h.service.connect('https://fixture.example', 'fixture-only', language),
        rejectedWith(h, 'protocol', language));
      assert.equal(h.calls.length, 1);
    }
  });

  test(`unhealthy servers and invalid addresses have localized stable errors (${language})`, async () => {
    const unhealthy = fixture([{ responseCode: 503, result: 'Unavailable' }]);
    await assert.rejects(unhealthy.service.connect('https://fixture.example', 'fixture-only', language),
      rejectedWith(unhealthy, 'health_failed', language));
    assert.equal(unhealthy.calls.length, 1);

    const invalid = fixture([]);
    await assert.rejects(invalid.service.connect('https://fixture.example/private', 'fixture-only', language),
      rejectedWith(invalid, 'invalid_address', language));
    assert.equal(invalid.calls.length, 0);
  });

  test(`authentication errors keep their codes and use the selected language (${language})`, async () => {
    for (const [responseCode, code] of [[401, 'password'], [429, 'rate_limit'], [503, 'auth_failed']]) {
      const h = fixture([healthResponse, { responseCode, header: {} }]);
      await assert.rejects(h.service.connect('https://fixture.example', 'fixture-only', language),
        rejectedWith(h, code, language));
      assert.equal(h.calls.length, 2);
      assert.equal(h.calls[1].url, 'https://fixture.example/login');
      assert.deepEqual(JSON.parse(h.calls[1].options.extraData), { password: 'fixture-only' });
      assert.equal(h.cookies.length, 0);
    }
    const missingCookie = fixture([healthResponse, { responseCode: 200, header: {} }]);
    await assert.rejects(missingCookie.service.connect('https://fixture.example', 'fixture-only', language),
      rejectedWith(missingCookie, 'auth_cookie_missing', language));
    assert.equal(missingCookie.cookieSaves, 0);
  });

  test(`HTTP transport failures distinguish certificates, DNS and timeouts (${language})`, async () => {
    const cases = [
      [2300060, 'certificate'],
      [2300058, 'tls_local_certificate'],
      [2300077, 'tls_ca_unavailable'],
      [2300059, 'tls_protocol'],
      [2300005, 'dns'],
      [2300006, 'dns'],
      [2300028, 'timeout'],
      [2300007, 'network'],
      [2300999, 'network'],
      [201, 'network_permission']
    ];
    for (const [nativeCode, code] of cases) {
      const h = fixture([() => { throw { code: nativeCode, message: 'Native diagnostic detail' }; }]);
      await assert.rejects(h.service.connect('https://fixture.example', 'fixture-only', language),
        rejectedWith(h, code, language));
      assert.equal(h.calls.length, 1);
      assert.equal(h.requests[0].destroys, 1);
      if (code !== 'certificate') {
        assert.doesNotMatch(nativeMessage(code, language), /self-signed|自签名/);
      }
    }
  });
}

test('authentication transport failures are classified without writing cookies', async () => {
  const h = fixture([healthResponse, () => { throw { code: 2300060 }; }]);
  await assert.rejects(h.service.connect('https://fixture.example', 'fixture-only', 'en'),
    rejectedWith(h, 'certificate', 'en'));
  assert.equal(h.calls.length, 2);
  assert.equal(h.cookies.length, 0);
  assert.equal(h.registrations.length, 0);
  assert.equal(h.cookieSaves, 0);
});

test('a successful connection normalizes its address and commits the returned cookies', async () => {
  const h = fixture([healthResponse, {
    responseCode: 200,
    header: { 'Set-Cookie': ['session=fixture; Path=/; Secure', 'other=fixture; Path=/'] }
  }]);
  assert.equal(await h.service.connect('https://FIXTURE.example:443/', 'fixture-only', 'en'),
    'https://fixture.example');
  assert.deepEqual(h.cookies, [
    { url: 'https://fixture.example/', cookie: 'session=fixture; Path=/; Secure' },
    { url: 'https://fixture.example/', cookie: 'other=fixture; Path=/' }
  ]);
  assert.deepEqual(h.registrations, [{
    url: 'https://fixture.example',
    values: ['session=fixture; Path=/; Secure', 'other=fixture; Path=/']
  }]);
  assert.equal(h.cookieSaves, 1);
  assert.ok(h.requests.every(request => request.destroys === 1));
  assert.ok(h.calls.every(call => call.options.maxRedirects === 0));
});

test('cancelled health requests cannot submit credentials after a late success or TLS failure', async () => {
  for (const reject of [false, true]) {
    const pending = deferred();
    const h = fixture([() => pending.promise]);
    const connecting = h.service.connect('https://old.example', 'old-fixture', 'en');
    const rejected = assert.rejects(connecting, rejectedWith(h, 'cancelled', 'en'));
    h.service.cancel();
    if (reject) pending.reject({ code: 2300060 });
    else pending.resolve(healthResponse);
    await rejected;
    assert.equal(h.calls.length, 1);
    assert.equal(h.cookies.length, 0);
  }
});

test('a newer connection prevents a late authentication response from replacing its cookies', async () => {
  const pendingAuth = deferred();
  const authStarted = deferred();
  const h = fixture([
    healthResponse,
    () => { authStarted.resolve(); return pendingAuth.promise; },
    healthResponse,
    { responseCode: 200, header: { 'set-cookie': 'session=new-fixture; Path=/' } }
  ]);
  const old = h.service.connect('https://old.example', 'old-fixture', 'en');
  const oldRejected = assert.rejects(old, rejectedWith(h, 'cancelled', 'en'));
  await authStarted.promise;
  assert.equal(await h.service.connect('https://new.example', 'new-fixture', 'zh'), 'https://new.example');
  pendingAuth.resolve({ responseCode: 200, header: { 'set-cookie': 'session=old-fixture; Path=/' } });
  await oldRejected;
  assert.deepEqual(h.cookies, [{ url: 'https://new.example/', cookie: 'session=new-fixture; Path=/' }]);
  assert.equal(h.registrations.length, 1);
  assert.equal(h.cookieSaves, 1);
});

test('native error localization uses stable codes and retains unknown diagnostics', () => {
  const known = new NativeFailure('credential_read_failed', 'zh');
  assert.equal(known.code, 'credential_read_failed');
  assert.match(known.message, /密码/);
  assert.equal(formatNativeError(known, 'en'), 'The saved password could not be read. Please enter it again.');
  assert.equal(formatNativeError(new Error('Precise diagnostic'), 'en'), 'Precise diagnostic');
  assert.equal(formatNativeError(new Error(''), 'en'), nativeMessage('operation_failed', 'en'));
  assert.equal(nativeMessage('toString', 'en'), nativeMessage('operation_failed', 'en'));

  const h = fixture([]);
  assert.equal(formatNativeError(new h.ConnectionFailure('password', 'stale message'), 'en'), 'Wrong password');
});
