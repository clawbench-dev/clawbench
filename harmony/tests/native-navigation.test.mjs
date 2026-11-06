import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { runInNewContext } from 'node:vm';
import { isTrustedAppUrl, parseServerAddress } from '../entry/src/main/ets/models/ServerAddress.ts';
import { APP_VERSION, isLocalLoginPage } from '../entry/src/main/ets/models/AppInfo.ts';
import { portBrowserUrl } from '../entry/src/main/ets/models/NativeActions.ts';

const source = readFileSync(new URL(
  '../entry/src/main/ets/pages/Index.ets', import.meta.url
), 'utf8');
const connectionSource = readFileSync(new URL(
  '../entry/src/main/ets/services/ConnectionService.ets', import.meta.url
), 'utf8');

function between(text, start, end) {
  const from = text.indexOf(start);
  const to = text.indexOf(end, from + start.length);
  assert.ok(from >= 0 && to > from, 'The real method boundaries must still exist');
  return text.slice(from, to);
}

// Execute the production methods while replacing only the native platform dependencies.
const methods = [
  between(source, '  private async notificationChanged(', '  private shareErrorChanged('),
  between(source, '  private async showConnection(', '  private startPageTimeout('),
  between(source, '  private async receiveLoginBridge(', '  private reportLoginError('),
  between(source, '  private async invokeBridge(', '  private async keepScreenOn('),
  between(source, '  onBackPress(): boolean {', '  build() {')
].join('\n');
const failureClass = between(connectionSource,
  'export class ConnectionFailure ', 'export class ConnectionService ').replace('export class', 'class');
const code = stripTypeScriptTypes(failureClass + '\nclass Navigation {\n' + methods + '\n}', { mode: 'strip' }) +
  '\n({ Navigation, ConnectionFailure });';
const tick = () => new Promise(resolve => setImmediate(resolve));

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function request(url, password = '') {
  return { id: 1, method: 'connectToServer', args: [JSON.stringify(url), JSON.stringify(password)] };
}

function host({ local = false, passwordWait, snapshotWait, connections = new Map() } = {}) {
  const passwordCalls = [];
  const networkCalls = [];
  const saved = [];
  const reports = [];
  const replies = [];
  let snapshotCalls = 0;
  let localUrl = 'resource://rawfile/login/index.html';
  let remoteUrl;
  let nextUuid = 0;
  const { Navigation, ConnectionFailure } = runInNewContext(code, {
    isTrustedAppUrl, parseServerAddress, APP_VERSION, isLocalLoginPage, portBrowserUrl, Error,
    clearServerCookies() {}, clearTimeout() {},
    util: {
      generateRandomUUID: () => 'fixture-' + ++nextUuid,
      TextDecoder: class {
        decodeWithStream(bytes) { return new TextDecoder().decode(bytes); }
      }
    }
  });
  const instance = new Navigation();
  Object.assign(instance, {
    connectionId: 1, documentId: 1, loginEpoch: 1,
    showWeb: !local, activeUrl: 'https://current.example', activePassword: '',
    foreground: true, ports: [], loginPorts: [], pendingLoginError: '', pendingLoginCode: '',
    loginInitialization: { configuration: '' },
    controller: { getUrl: () => remoteUrl || instance.activeUrl + '/' },
    loginController: { getUrl: () => localUrl },
    store: {
      servers: [], language: 'en', theme: 'dark',
      async password(url) {
        passwordCalls.push({ source: 'store', value: url });
        return passwordWait || 'fixture-password';
      },
      async save(url, password, name) { saved.push({ url, password, name }); },
      async setActive(url) { instance.selectedServer = url; }
    },
    login: {
      clear() {},
      async password(value) {
        passwordCalls.push({ source: 'login', value });
        return passwordWait || 'fixture-password';
      },
      async snapshot() {
        const sequence = ++snapshotCalls;
        return sequence === 1 && snapshotWait ? snapshotWait : { sequence };
      }
    },
    connection: {
      cancel() {},
      async connect(url, password) {
        networkCalls.push({ url, password });
        return connections.get(url) || url;
      }
    },
    notifications: { stop() {}, async bind() {} },
    context: () => ({
      resourceManager: {
        async getRawFileContent() { return new TextEncoder().encode('__HARMONY_CONFIG__'); }
      }
    }),
    closePorts(ports) { ports.length = 0; },
    keepScreenOn() {},
    disposePageServices() { this.documentId++; },
    startPageTimeout() {},
    reportLoginError() { reports.push({ message: this.pendingLoginError, code: this.pendingLoginCode }); }
  });
  return {
    instance, passwordCalls, networkCalls, saved, reports, replies, ConnectionFailure,
    port: { postMessageEvent: value => replies.push(JSON.parse(value)) },
    snapshots: () => snapshotCalls,
    setLocalUrl: value => { localUrl = value; },
    setRemoteUrl: value => { remoteUrl = value; }
  };
}

function notificationHost(options = {}, launch = {
  origin: 'https://notification.example', navigation: { sessionId: 'fixture-session' }
}) {
  const h = host(options);
  const queued = [];
  let delivered = 0;
  Object.assign(h.instance, {
    nativeReady: true, notificationToken: 'fixture-token', notificationEpoch: 0, rendererReady: true,
    deliverPendingNavigation() { delivered++; }
  });
  Object.assign(h.instance.notifications, {
    async takeLaunch() { return launch; },
    queueNavigation(value) { queued.push(value); }
  });
  return { ...h, queued, delivered: () => delivered };
}

test('a current notification connects its server and retains the requested navigation', async () => {
  const h = notificationHost();
  await h.instance.notificationChanged();
  assert.deepEqual(h.networkCalls, [{
    url: 'https://notification.example', password: 'fixture-password'
  }]);
  assert.equal(h.instance.activeUrl, 'https://notification.example');
  assert.equal(h.instance.showWeb, true);
  assert.equal(h.queued.length, 1);
  assert.equal(h.queued[0].navigation.sessionId, 'fixture-session');
});

test('a notification for the ready current server navigates without reconnecting', async () => {
  const h = notificationHost({}, {
    origin: 'https://current.example', navigation: { sessionId: 'fixture-session' }
  });
  await h.instance.notificationChanged();
  assert.equal(h.networkCalls.length, 0);
  assert.equal(h.snapshots(), 0);
  assert.equal(h.queued.length, 1);
  assert.equal(h.delivered(), 1);
});

test('a late notification password result or failure cannot replace a newer connection', async () => {
  for (const fail of [false, true]) {
    const password = deferred();
    const h = notificationHost({ passwordWait: password.promise });
    const opening = h.instance.notificationChanged();
    await tick();
    assert.equal(h.passwordCalls.length, 1);
    await h.instance.showConnection();
    await h.instance.connect('https://new.example', 'new-fixture-password');
    if (fail) password.reject(new Error('Old password lookup failed'));
    else password.resolve('old-fixture-password');
    await opening;
    assert.deepEqual(h.networkCalls.map(call => call.url), ['https://new.example']);
    assert.equal(h.instance.activeUrl, 'https://new.example');
    assert.equal(h.instance.showWeb, true);
    assert.equal(h.snapshots(), 1);
    assert.equal(h.queued.length, 0);
  }
});

test('a stale notification login snapshot cannot connect or queue the old destination', async () => {
  for (const invalidate of [
    instance => { instance.connectionId++; },
    instance => { instance.loginEpoch++; },
    instance => { instance.notificationEpoch++; },
    instance => { instance.showWeb = true; }
  ]) {
    const snapshot = deferred();
    const h = notificationHost({ snapshotWait: snapshot.promise });
    const opening = h.instance.notificationChanged();
    await tick();
    assert.equal(h.snapshots(), 1);
    invalidate(h.instance);
    snapshot.resolve({ sequence: 1 });
    await opening;
    assert.equal(h.networkCalls.length, 0);
    assert.equal(h.queued.length, 0);
    assert.equal(h.snapshots(), 1);
  }
});

test('a notification snapshot finishing after a user switch preserves the new server', async () => {
  for (const fail of [false, true]) {
    const snapshot = deferred();
    const h = notificationHost({ snapshotWait: snapshot.promise });
    const opening = h.instance.notificationChanged();
    await tick();
    await h.instance.showConnection();
    await h.instance.connect('https://new.example', 'new-fixture-password');
    if (fail) snapshot.reject(new Error('Old snapshot unavailable'));
    else snapshot.resolve({ sequence: 1 });
    await opening;
    assert.deepEqual(h.networkCalls.map(call => call.url), ['https://new.example']);
    assert.equal(h.instance.activeUrl, 'https://new.example');
    assert.equal(h.instance.showWeb, true);
    assert.equal(h.queued.length, 0);
  }
});

test('a stale notification connection failure cannot replace newer login or notification state', async () => {
  for (const invalidate of [
    instance => { instance.connectionId++; },
    instance => { instance.loginEpoch++; },
    instance => { instance.notificationEpoch++; }
  ]) {
    const network = deferred();
    const h = notificationHost({
      connections: new Map([['https://notification.example', network.promise]])
    });
    const opening = h.instance.notificationChanged();
    await tick();
    assert.equal(h.networkCalls.length, 1);
    invalidate(h.instance);
    h.instance.pendingLoginError = 'Newer login state';
    network.reject(new Error('Old notification connection failed'));
    await opening;
    assert.equal(h.snapshots(), 1);
    assert.equal(h.instance.pendingLoginError, 'Newer login state');
  }
});

test('a failed notification connection cannot send an already opened server back to login', async () => {
  const network = deferred();
  const h = notificationHost({
    connections: new Map([['https://notification.example', network.promise]])
  });
  const opening = h.instance.notificationChanged();
  await tick();
  await h.instance.showConnection();
  await h.instance.connect('https://new.example', 'new-fixture-password');
  network.reject(new Error('Old notification connection failed'));
  await opening;
  assert.equal(h.instance.activeUrl, 'https://new.example');
  assert.equal(h.instance.showWeb, true);
  assert.equal(h.snapshots(), 2);
});

test('a current notification connection failure still prompts login for its destination', async () => {
  const network = deferred();
  const h = notificationHost({
    connections: new Map([['https://notification.example', network.promise]])
  });
  const opening = h.instance.notificationChanged();
  await tick();
  network.reject(new Error('Notification login rejected'));
  await opening;
  assert.equal(h.snapshots(), 2);
  assert.equal(h.instance.showWeb, false);
  assert.equal(h.instance.connecting, false);
  assert.ok(h.instance.pendingLoginError.length > 0);
  assert.equal(h.queued[0].origin, 'https://notification.example');
});

test('a saved password arriving after another server opens cannot reconnect the old server', async () => {
  const password = deferred();
  const h = host({ passwordWait: password.promise });
  const switching = h.instance.invokeBridge(request('https://old.example'));
  assert.equal(h.passwordCalls.length, 1);
  await h.instance.showConnection();
  await h.instance.connect('https://new.example', 'new-fixture-password');
  password.resolve('old-fixture-password');
  await switching;
  assert.deepEqual(h.networkCalls.map(call => call.url), ['https://new.example']);
  assert.equal(h.instance.activeUrl, 'https://new.example');
  assert.equal(h.instance.showWeb, true);
  assert.equal(h.snapshots(), 1);
});

test('a saved password cannot trigger navigation after the trusted document changes', async () => {
  for (const invalidate of [
    h => { h.instance.documentId++; },
    h => { h.instance.showWeb = false; },
    h => { h.setRemoteUrl('https://untrusted.example/'); }
  ]) {
    const password = deferred();
    const h = host({ passwordWait: password.promise });
    const switching = h.instance.invokeBridge(request('https://old.example'));
    invalidate(h);
    password.resolve('fixture-password');
    await switching;
    assert.equal(h.networkCalls.length, 0);
    assert.equal(h.snapshots(), 0);
  }
});

test('a stale login-page snapshot cannot start an older requested connection', async () => {
  const snapshot = deferred();
  const h = host({ snapshotWait: snapshot.promise });
  const switching = h.instance.invokeBridge(request('https://old.example', 'fixture-password'));
  assert.equal(h.snapshots(), 1);
  assert.equal(h.networkCalls.length, 0);
  await h.instance.showConnection('Newer login state', 'CURRENT');
  await h.instance.connect('https://new.example', 'new-fixture-password');
  snapshot.resolve({ sequence: 1 });
  await switching;
  assert.deepEqual(h.networkCalls.map(call => call.url), ['https://new.example']);
  assert.equal(JSON.parse(h.instance.loginInitialization.configuration).state.sequence, 2);
  assert.equal(h.instance.activeUrl, 'https://new.example');
});

test('an old connection failure cannot overwrite a newer login error', async () => {
  const network = deferred();
  const h = host({ connections: new Map([['https://old.example', network.promise]]) });
  await h.instance.invokeBridge(request('https://old.example', 'fixture-password'));
  assert.equal(h.networkCalls.length, 1);
  await h.instance.showConnection('Newer login error', 'CURRENT');
  network.reject(new h.ConnectionFailure('STALE', 'Old connection failed'));
  await tick();
  assert.equal(h.instance.pendingLoginError, 'Newer login error');
  assert.equal(h.instance.pendingLoginCode, 'CURRENT');
  assert.equal(h.reports.length, 0);
});

test('an active connection failure still reports the original typed login error', async () => {
  const network = deferred();
  const h = host({ connections: new Map([['https://failed.example', network.promise]]) });
  await h.instance.invokeBridge(request('https://failed.example', 'fixture-password'));
  network.reject(new h.ConnectionFailure('AUTH_REJECTED', 'Fixture login rejected'));
  await tick();
  assert.deepEqual(h.reports, [{ message: 'Fixture login rejected', code: 'AUTH_REJECTED' }]);
  assert.equal(h.instance.showWeb, false);
  assert.equal(h.instance.connecting, false);
});

test('local login passwords are discarded after an epoch, document or host transition', async () => {
  for (const invalidate of [
    h => { h.instance.loginEpoch++; },
    h => { h.instance.showWeb = true; },
    h => { h.setLocalUrl('https://untrusted.example/login'); }
  ]) {
    const password = deferred();
    const h = host({ local: true, passwordWait: password.promise });
    const operation = h.instance.receiveLoginBridge(
      JSON.stringify(request('https://old.example', 'password-token')), h.port, h.instance.loginEpoch
    );
    assert.equal(h.passwordCalls.length, 1);
    invalidate(h);
    password.resolve('fixture-password');
    await operation;
    assert.equal(h.networkCalls.length, 0);
    assert.equal(h.replies.length, 0);
  }
});

test('a previous local login failure cannot reply into the next login document', async () => {
  const network = deferred();
  const h = host({ local: true, connections: new Map([['https://old.example', network.promise]]) });
  const operation = h.instance.receiveLoginBridge(
    JSON.stringify(request('https://old.example', 'password-token')), h.port, h.instance.loginEpoch
  );
  await tick();
  assert.equal(h.networkCalls.length, 1);
  await h.instance.showConnection('Current login error', 'CURRENT');
  network.reject(new h.ConnectionFailure('OLD', 'Old request failed'));
  await operation;
  assert.equal(h.replies.length, 0);
  assert.equal(h.instance.pendingLoginError, 'Current login error');
  assert.equal(h.instance.pendingLoginCode, 'CURRENT');
});

test('both current native entry points still complete the real connection flow', async () => {
  const remote = host();
  await remote.instance.invokeBridge(request('https://selected.example'));
  await tick();
  assert.deepEqual(remote.networkCalls, [{ url: 'https://selected.example', password: 'fixture-password' }]);
  assert.equal(remote.instance.showWeb, true);
  assert.equal(remote.instance.selectedServer, 'https://selected.example');
  assert.equal(remote.saved.length, 1);
  assert.equal(remote.passwordCalls[0].source, 'store');
  const local = host({ local: true });
  await local.instance.receiveLoginBridge(
    JSON.stringify(request('https://selected.example', 'password-token')), local.port, local.instance.loginEpoch
  );
  assert.deepEqual(local.networkCalls, [{ url: 'https://selected.example', password: 'fixture-password' }]);
  assert.equal(local.instance.showWeb, true);
  assert.equal(local.instance.selectedServer, 'https://selected.example');
  assert.equal(local.saved.length, 1);
  assert.equal(local.passwordCalls[0].source, 'login');
  assert.equal(local.replies.length, 0);
});

test('a late back-key result or failure cannot navigate a newer document', async () => {
  for (const fail of [false, true]) {
    for (const invalidate of [
      instance => { instance.connectionId++; },
      instance => { instance.documentId++; },
      instance => { instance.loginEpoch++; },
      instance => { instance.showWeb = false; }
    ]) {
      const h = host();
      const back = deferred();
      h.instance.controller.runJavaScript = () => back.promise;
      let navigations = 0;
      h.instance.showConnection = () => { navigations++; };
      h.instance.lastBack = Date.now();
      assert.equal(h.instance.onBackPress(), true);
      invalidate(h.instance);
      if (fail) back.reject(new Error('Old controller disposed'));
      else back.resolve('false');
      await tick();
      assert.equal(navigations, 0);
      assert.equal(h.instance.backInFlight, false);
    }
  }
});

test('a current failed back-key dispatch still returns to the server list', async () => {
  const h = host();
  h.instance.controller.runJavaScript = async () => { throw new Error('Renderer unavailable'); };
  let navigations = 0;
  h.instance.showConnection = () => { navigations++; };
  assert.equal(h.instance.onBackPress(), true);
  await tick();
  assert.equal(navigations, 1);
  assert.equal(h.instance.backInFlight, false);
});

test('port browsers wait for native readiness and use the restored local port', async () => {
  for (const method of ['openInBrowser', 'openInSandbox']) {
    const h = host();
    const ready = deferred();
    const opened = [];
    h.instance.tunnels = { prepareForwardedPort: port => {
      assert.equal(port, 3000);
      return ready.promise;
    } };
    h.instance.openExternal = async url => { opened.push(url); };
    const operation = h.instance.invokeBridge({
      method, args: ['3000', '"http"', '"::1"', '"/example?value=1"']
    });
    assert.equal(opened.length, 0);
    ready.resolve(3001);
    await operation;
    assert.deepEqual(opened, ['http://127.0.0.1:3001/example?value=1']);
  }
});

test('a cancelled or failed tunnel cannot open an external browser', async () => {
  for (const mode of ['failure', 'document', 'background']) {
    const h = host();
    const ready = deferred();
    let opened = false;
    h.instance.tunnels = { prepareForwardedPort: () => ready.promise };
    h.instance.openExternal = async () => { opened = true; };
    const operation = h.instance.invokeBridge({
      method: 'openInSandbox', args: ['3000', '"http"', '""', '"/"']
    });
    const rejected = assert.rejects(operation, /not ready|Page changed/);
    if (mode === 'failure') ready.reject(new Error('Tunnel not ready'));
    else {
      if (mode === 'document') h.instance.documentId++;
      else h.instance.foreground = false;
      ready.resolve(3000);
    }
    await rejected;
    assert.equal(opened, false);
  }
});

test('a non-loopback port browser does not start a local tunnel', async () => {
  const h = host();
  const opened = [];
  h.instance.tunnels = { prepareForwardedPort: () => { throw new Error('Unexpected tunnel'); } };
  h.instance.openExternal = async url => { opened.push(url); };
  await h.instance.invokeBridge({
    method: 'openInBrowser', args: ['8443', '"https"', '"preview.example"', '"/"']
  });
  assert.deepEqual(opened, ['https://preview.example:8443/']);
});
