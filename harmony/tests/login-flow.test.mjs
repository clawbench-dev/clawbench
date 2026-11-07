import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import jsdom from 'jsdom';
import { isLocalLoginPage, APP_VERSION } from '../entry/src/main/ets/models/AppInfo.ts';

const rawfile = new URL('../entry/src/main/resources/rawfile/', import.meta.url);
const html = readFileSync(new URL('login/index.html', rawfile), 'utf8');
const bootstrap = readFileSync(new URL('login-bootstrap.js', rawfile), 'utf8');
const urlUtils = readFileSync(new URL('login/url-utils.js', rawfile));
const { JSDOM, VirtualConsole } = jsdom;
const fixture = { url: 'https://home.example:20000', name: 'Home', password: 'clawbench-saved:fixture' };
const plain = value => ({ kind: 'plain', value });
const saved = server => ({ kind: 'saved', ref: server.password, sourceUrl: server.url });

function waitForDom(window, condition) {
  if (condition()) return Promise.resolve();
  return new Promise((resolve, reject) => {
    const observer = new window.MutationObserver(() => {
      if (condition()) {
        observer.disconnect();
        window.clearTimeout(timer);
        resolve();
      }
    });
    const timer = window.setTimeout(() => {
      observer.disconnect();
      reject(new Error('Expected login UI state was not reached'));
    }, 3000);
    observer.observe(window.document, { attributes: true, childList: true, subtree: true });
  });
}

async function createLogin(t, {
  servers = [], activeUrl = servers[0]?.url || '', failSave = false, failConnect = false,
  failRemove = false, holdSelections = false, holdRemovals = false, language = 'zh', credentialsIgnored = false
} = {}) {
  const state = {
    servers: structuredClone(servers), activeUrl, credentialsIgnored, language, theme: 'gruvbox-dark', version: APP_VERSION
  };
  const calls = [];
  const errors = [];
  const selections = [];
  const removals = [];
  let nativePort;
  let domReady;
  const ready = new Promise(resolve => { domReady = resolve; });
  const virtualConsole = new VirtualConsole();
  virtualConsole.on('jsdomError', error => errors.push(error.message));
  const dom = new JSDOM(html.replace('<script src="url-utils.js"></script>', `<script>${urlUtils.toString()}</script>`), {
    url: 'resource://rawfile/login/index.html',
    runScripts: 'dangerously',
    virtualConsole,
    beforeParse(window) {
      window.ClawBenchLoginInit = { getConfig: () => JSON.stringify({ channel: 'local-test', state }) };
      window.eval(bootstrap);
      nativePort = {
        start() {},
        close() {},
        postMessage(message) {
          const request = JSON.parse(message);
          const args = request.args.map(value => JSON.parse(value));
          calls.push({ method: request.method, args });
          const respond = () => {
            let error = '';
            let code = '';
            if (request.method === 'setAppearance') {
              state.language = args[0];
              state.theme = args[1];
            } else if (request.method === 'saveServerNamed') {
              if (failSave) {
                error = 'fixture storage failure';
                code = 'credential_save_failed';
              }
              else {
                const hasPassword = args[1].kind === 'saved' || args[1].value;
                state.servers = [{
                  url: args[0], name: args[2], password: hasPassword ? 'clawbench-saved:updated' : '',
                  credentialState: hasPassword ? 'saved' : 'missing'
                }, ...state.servers.filter(server => server.url !== args[0])];
              }
            } else if (request.method === 'removeServer') {
              if (failRemove) {
                error = 'fixture credential removal failure';
                code = 'credential_remove_failed';
              } else {
                state.servers = state.servers.filter(server => server.url !== args[0]);
                if (state.activeUrl === args[0]) state.activeUrl = state.servers[0]?.url || '';
              }
            } else if (request.method === 'selectServer') {
              state.activeUrl = args[0];
            } else if (request.method === 'ignoreSavedCredentials') {
              state.credentialsIgnored = true;
              state.servers = state.servers.map(server => ({ ...server, password: '', credentialState: 'ignored' }));
            } else if (request.method === 'connectToServer' && failConnect) {
              error = '密码不正确';
              code = typeof failConnect === 'string' ? failConnect : 'password';
            }
            const response = error ? { id: request.id, error, code } :
              { id: request.id, result: JSON.stringify({ value: null, snapshot: state }) };
            nativePort.onmessage({ data: JSON.stringify(response) });
          };
          if (holdSelections && request.method === 'selectServer') selections.push(respond);
          else if (holdRemovals && request.method === 'removeServer') removals.push(respond);
          else queueMicrotask(respond);
        }
      };
      window.dispatchEvent(new window.MessageEvent('message', {
        data: '__clawbench_login_port__:local-test', ports: [nativePort]
      }));
      window.addEventListener('DOMContentLoaded', domReady, { once: true });
    }
  });
  t.after(() => dom.window.close());
  await ready;
  return { window: dom.window, document: dom.window.document, calls, errors, selections, removals };
}

test('only the exact packaged login document is trusted', () => {
  assert.equal(isLocalLoginPage('resource://rawfile/login/index.html'), true);
  for (const url of ['https://home.example/login/index.html', 'resource://rawfile/other.html',
    'resource://evil/login/index.html', 'resource://rawfile/login/index.html/extra']) {
    assert.equal(isLocalLoginPage(url), false);
  }
});

test('fresh install renders the Android add-server page with the host version and theme', async t => {
  const { document, errors } = await createLogin(t);
  assert.equal(document.querySelector('.slogan').textContent, '从掌心到桌面');
  assert.equal(document.querySelector('.subtitle').textContent, '多端一体的 AI 工作台');
  assert.equal(document.querySelector('#version').textContent, 'v' + APP_VERSION);
  assert.equal(document.documentElement.dataset.theme, 'gruvbox-dark');
  assert.equal(document.querySelector('#addServerForm').style.display, 'block');
  assert.equal(document.querySelector('#connectForm').style.display, 'none');
  assert.deepEqual(errors, []);
});

test('saving adds a server only after native persistence and returns to the saved list', async t => {
  const { window, document, calls } = await createLogin(t);
  document.querySelector('#addName').value = 'Home';
  document.querySelector('#addAddress').value = fixture.url;
  document.querySelector('#addPassword').value = 'fixture-input-password';
  document.querySelector('#addServerForm').dispatchEvent(new window.Event('submit', { cancelable: true }));
  assert.equal(document.querySelector('#addConnectBtn').disabled, true);
  assert.equal(document.querySelector('#addServerForm').style.display, 'block');
  await waitForDom(window, () => document.querySelector('#addServerForm').style.display === 'none');
  assert.equal(document.querySelector('.server-item-url').textContent, 'Home');
  assert.equal(document.querySelector('#passwordGroup').style.display, 'none');
  assert.deepEqual(calls.find(call => call.method === 'saveServerNamed').args,
    [fixture.url, plain('fixture-input-password'), 'Home']);
  assert.equal(calls.some(call => call.method === 'connectToServer'), false);
});

test('failed persistence leaves the Android form and its inputs available', async t => {
  const { window, document } = await createLogin(t, { failSave: true });
  document.querySelector('#addName').value = 'Home';
  document.querySelector('#addAddress').value = fixture.url;
  document.querySelector('#addPassword').value = 'fixture-input-password';
  document.querySelector('#addServerForm').dispatchEvent(new window.Event('submit', { cancelable: true }));
  await waitForDom(window, () => document.querySelector('#addErrorMsg').classList.contains('visible'));
  assert.equal(document.querySelector('#addServerForm').style.display, 'block');
  assert.equal(document.querySelector('#addAddress').value, fixture.url);
  assert.equal(document.querySelector('#addConnectBtn').disabled, false);
  assert.equal(document.querySelector('#addErrorText').textContent.includes('fixture storage failure'), true);
});

test('authentication errors reveal the password field without overwriting the saved credential', async t => {
  const { window, document, calls } = await createLogin(t, { servers: [fixture], failConnect: true });
  assert.equal(document.querySelector('#passwordGroup').style.display, 'none');
  document.querySelector('#connectForm').dispatchEvent(new window.Event('submit', { cancelable: true }));
  await waitForDom(window, () => document.querySelector('#errorMsg').classList.contains('visible'));
  assert.equal(document.querySelector('#passwordGroup').style.display, 'block');
  assert.equal(document.querySelector('#connectBtn').disabled, false);
  assert.equal(calls.some(call => call.method === 'saveServer'), false);
  assert.deepEqual(calls.find(call => call.method === 'connectToServer').args, [fixture.url, saved(fixture)]);
});

test('renaming an address preserves the password reference and removes the old entry after saving', async t => {
  const { window, document, calls } = await createLogin(t, { servers: [fixture] });
  window.editServer(fixture.url);
  document.querySelector('#addAddress').value = 'https://work.example:20000';
  document.querySelector('#addName').value = 'Work';
  assert.equal(document.querySelector('#addPassword').value, '');
  assert.equal(document.querySelector('#addServerForm').dataset.passwordRef, fixture.password);
  document.querySelector('#addServerForm').dispatchEvent(new window.Event('submit', { cancelable: true }));
  await waitForDom(window, () => document.querySelector('#addServerForm').style.display === 'none');
  const mutations = calls.filter(call => ['saveServerNamed', 'removeServer'].includes(call.method));
  assert.deepEqual(mutations.map(call => call.method), ['saveServerNamed', 'removeServer']);
  assert.deepEqual(mutations[0].args[1], saved(fixture));
  assert.equal(document.querySelectorAll('.server-item').length, 1);
  assert.equal(document.querySelector('.server-item-url').textContent, 'Work');
});

test('editing a saved default-port server keeps its native identity and credential', async t => {
  const server = { ...fixture, url: 'https://home.example' };
  const { window, document, calls } = await createLogin(t, { servers: [server] });
  window.editServer(server.url);
  document.querySelector('#addName').value = 'Renamed';
  document.querySelector('#addServerForm').dispatchEvent(new window.Event('submit', { cancelable: true }));
  await waitForDom(window, () => document.querySelector('#addServerForm').style.display === 'none');
  assert.deepEqual(calls.find(call => call.method === 'saveServerNamed').args,
    [server.url, saved(server), 'Renamed']);
  assert.equal(calls.some(call => call.method === 'removeServer'), false);

  window.editServer(server.url);
  document.querySelector('#addAddress').value = 'https://HOME.example:443';
  document.querySelector('#addServerForm').dispatchEvent(new window.Event('submit', { cancelable: true }));
  await waitForDom(window, () => document.querySelector('#addServerForm').style.display === 'none');
  assert.equal(calls.filter(call => call.method === 'saveServerNamed').at(-1).args[0], server.url);
  assert.equal(calls.some(call => call.method === 'removeServer'), false);
});

test('a differently formatted duplicate address cannot overwrite another saved credential', async t => {
  const server = { ...fixture, url: 'https://home.example' };
  const { window, document, calls } = await createLogin(t, { servers: [server] });
  window.showAddForm();
  document.querySelector('#addAddress').value = 'https://HOME.example:443';
  document.querySelector('#addServerForm').dispatchEvent(new window.Event('submit', { cancelable: true }));
  assert.equal(document.querySelector('#addErrorMsg').classList.contains('visible'), true);
  assert.equal(calls.some(call => call.method === 'saveServerNamed'), false);
});

test('deletion awaits native storage and the Android confirmation can be dismissed with Back', async t => {
  const { window, document, calls } = await createLogin(t, { servers: [fixture] });
  window.deleteServer(fixture.url);
  assert.equal(window.__clawbenchLoginBack(), true);
  assert.equal(calls.some(call => call.method === 'removeServer'), false);
  await waitForDom(window, () => document.querySelector('#dlgOverlay').style.display === 'none');
  window.deleteServer(fixture.url);
  document.querySelector('#dlgOk').click();
  await waitForDom(window, () => document.querySelector('#addServerForm').style.display === 'block');
  assert.deepEqual(calls.find(call => call.method === 'removeServer').args, [fixture.url]);
  assert.equal(document.querySelector('#serverListSection').style.display, 'none');
});

test('pending deletion locks selection and mutations until its snapshot is ready', async t => {
  const second = { url: 'https://work.example', name: 'Work', password: '' };
  const { window, document, calls, removals } = await createLogin(t, {
    servers: [fixture, second], holdRemovals: true
  });
  window.deleteServer(fixture.url);
  document.querySelector('#dlgOk').click();
  await waitForDom(window, () => document.querySelector('#connectBtn').disabled);
  assert.equal(removals.length, 1);
  window.selectServer(second.url);
  window.editServer(second.url);
  window.showAddForm();
  document.querySelector('#connectForm').dispatchEvent(new window.Event('submit', { cancelable: true }));
  assert.equal(document.querySelector('#connectForm').dataset.url, fixture.url);
  assert.equal(document.querySelector('#addServerForm').style.display, 'none');
  assert.equal(document.querySelector('#addServerBtn').disabled, true);
  assert.equal(document.querySelector('.server-edit-btn').disabled, true);
  assert.equal(calls.some(call => call.method === 'selectServer' || call.method === 'connectToServer'), false);
  removals[0]();
  await waitForDom(window, () => document.querySelectorAll('.server-item').length === 1);
  assert.equal(document.querySelector('#connectForm').dataset.url, second.url);
  assert.equal(document.querySelector('#connectBtn').disabled, false);
  assert.equal(document.querySelector('#addServerBtn').disabled, false);
  window.editServer(second.url);
  assert.equal(document.querySelector('#addServerForm').style.display, 'block');
});

test('a failed switch to the second server keeps the highlight, credential and retry on that server', async t => {
  const second = { url: 'https://work.example', name: 'Work', password: 'clawbench-saved:work' };
  const { window, document, calls } = await createLogin(t, {
    servers: [fixture, second], activeUrl: second.url, failConnect: true
  });
  assert.equal(document.querySelector('.server-item.active').dataset.url, second.url);
  assert.equal(document.querySelector('#connectForm').dataset.url, second.url);
  document.querySelector('#connectForm').dispatchEvent(new window.Event('submit', { cancelable: true }));
  await waitForDom(window, () => document.querySelector('#passwordGroup').style.display === 'block');
  document.querySelector('#password').value = 'fixture-new-work-password';
  document.querySelector('#connectForm').dispatchEvent(new window.Event('submit', { cancelable: true }));
  const attempts = calls.filter(call => call.method === 'connectToServer');
  assert.deepEqual(attempts.map(call => call.args), [
    [second.url, saved(second)],
    [second.url, plain('fixture-new-work-password')]
  ]);
});

test('quick server selections keep the newest target and typed input when old replies arrive', async t => {
  const second = { url: 'https://work.example', name: 'Work', password: '' };
  const { window, document, selections } = await createLogin(t, {
    servers: [fixture, second], holdSelections: true
  });
  window.selectServer(second.url);
  window.selectServer(fixture.url);
  window.selectServer(second.url);
  document.querySelector('#password').value = 'fixture-typed-password';
  assert.equal(selections.length, 3);
  for (const respond of selections) {
    respond();
    assert.equal(window.ClawBenchNative.getActiveServer(), second.url);
    assert.equal(document.querySelector('.server-item.active').dataset.url, second.url);
    assert.equal(document.querySelector('#connectForm').dataset.url, second.url);
    assert.equal(document.querySelector('#password').value, 'fixture-typed-password');
  }
});

test('connecting locks selection and mutation controls until the attempt fails', async t => {
  const second = { url: 'https://work.example', name: 'Work', password: '' };
  const { window, document, calls } = await createLogin(t, { servers: [fixture, second] });
  document.querySelector('#connectForm').dispatchEvent(new window.Event('submit', { cancelable: true }));
  window.selectServer(second.url);
  window.editServer(second.url);
  window.deleteServer(second.url);
  window.showAddForm();
  assert.equal(document.querySelector('#connectForm').dataset.url, fixture.url);
  assert.equal(document.querySelector('#addServerForm').style.display, 'none');
  assert.equal(document.querySelector('#dlgOverlay').style.display, 'none');
  assert.equal(document.querySelector('#addServerBtn').disabled, true);
  assert.equal(document.querySelector('.server-edit-btn').disabled, true);
  assert.equal(calls.some(call => call.method === 'selectServer'), false);
  window.onConnectError('Fixture failure', 'password', fixture.url);
  window.selectServer(second.url);
  assert.equal(document.querySelector('#connectForm').dataset.url, second.url);
  assert.equal(document.querySelector('#addServerBtn').disabled, false);
});

test('native-started connections lock their selected target and ignore failures for another target', async t => {
  const second = { url: 'https://work.example', name: 'Work', password: '' };
  const { window, document } = await createLogin(t, {
    servers: [fixture, second], activeUrl: second.url
  });
  window.onConnectStart(second.url);
  window.selectServer(fixture.url);
  window.onConnectError('Old server failed', 'password', fixture.url);
  assert.equal(window.connecting, true);
  assert.equal(document.querySelector('#connectForm').dataset.url, second.url);
  assert.equal(document.querySelector('#errorMsg').classList.contains('visible'), false);
  window.onConnectError('Current server failed', 'password', second.url);
  assert.equal(window.connecting, false);
  assert.equal(document.querySelector('#errorText').textContent, 'Current server failed');
});

test('editing a password with the reserved prefix sends literal text while unchanged edits retain the reference', async t => {
  const { window, document, calls } = await createLogin(t, { servers: [fixture] });
  window.editServer(fixture.url);
  const password = document.querySelector('#addPassword');
  password.value = 'clawbench-saved:literal-user-password';
  password.dispatchEvent(new window.Event('input', { bubbles: true }));
  document.querySelector('#addServerForm').dispatchEvent(new window.Event('submit', { cancelable: true }));
  await waitForDom(window, () => document.querySelector('#addServerForm').style.display === 'none');
  assert.deepEqual(calls.find(call => call.method === 'saveServerNamed').args,
    [fixture.url, plain('clawbench-saved:literal-user-password'), fixture.name]);
});

test('an unreadable credential exposes the recovery action and ignored mode still permits typed login', async t => {
  const unavailable = { ...fixture, password: '', credentialState: 'unavailable' };
  const { window, document, calls } = await createLogin(t, { servers: [unavailable], language: 'en' });
  assert.equal(document.querySelector('#ignoreCredentialsBtn').style.display, 'flex');
  assert.equal(document.querySelector('#passwordGroup').style.display, 'block');
  assert.match(document.querySelector('#errorText').textContent, /saved password is unavailable/i);
  document.querySelector('#ignoreCredentialsBtn').click();
  await waitForDom(window, () => document.querySelector('#ignoreCredentialsBtn').style.display === 'none');
  assert.equal(window.ClawBenchNative.areSavedCredentialsIgnored(), true);
  assert.match(document.querySelector('#errorText').textContent, /not be read or saved/i);
  assert.equal(calls.some(call => call.method === 'removeServer'), false);
  document.querySelector('#password').value = 'fixture-ephemeral-password';
  document.querySelector('#connectForm').dispatchEvent(new window.Event('submit', { cancelable: true }));
  assert.deepEqual(calls.find(call => call.method === 'connectToServer').args,
    [fixture.url, plain('fixture-ephemeral-password')]);
});

test('a saved-credential read failure expands manual input and exposes recovery', async t => {
  const { window, document } = await createLogin(t, {
    servers: [fixture], failConnect: 'credential_read_failed'
  });
  document.querySelector('#connectForm').dispatchEvent(new window.Event('submit', { cancelable: true }));
  await waitForDom(window, () => document.querySelector('#ignoreCredentialsBtn').style.display === 'flex');
  assert.equal(document.querySelector('#passwordGroup').style.display, 'block');
  assert.equal(document.querySelector('#connectForm').hasAttribute('data-saved-password'), false);
});

test('credential save errors expose recovery even before the first server is saved', async t => {
  const { window, document, calls } = await createLogin(t, { failSave: true });
  document.querySelector('#addAddress').value = fixture.url;
  document.querySelector('#addPassword').value = 'fixture-password';
  document.querySelector('#addServerForm').dispatchEvent(new window.Event('submit', { cancelable: true }));
  await waitForDom(window, () => document.querySelector('#ignoreCredentialsBtn').style.display === 'flex');
  document.querySelector('#ignoreCredentialsBtn').click();
  await waitForDom(window, () => document.querySelector('#ignoreCredentialsBtn').style.display === 'none');
  assert.equal(document.querySelector('#addServerForm').style.display, 'block');
  assert.equal(document.querySelector('#addPassword').value, '');
  assert.equal(document.querySelector('#addErrorText').textContent, '本次会话不读取或保存密码。');
  assert.equal(calls.filter(call => call.method === 'ignoreSavedCredentials').length, 1);
});

test('credential removal errors expose recovery without dropping the saved server', async t => {
  const { window, document } = await createLogin(t, { servers: [fixture], failRemove: true });
  window.deleteServer(fixture.url);
  document.querySelector('#dlgOk').click();
  await waitForDom(window, () => document.querySelector('#ignoreCredentialsBtn').style.display === 'flex');
  assert.equal(document.querySelectorAll('.server-item').length, 1);
});

test('deleting another server preserves the selected server after rendering the new snapshot', async t => {
  const second = { url: 'https://work.example', name: 'Work', password: '' };
  const { window, document } = await createLogin(t, {
    servers: [fixture, second], activeUrl: second.url
  });
  window.deleteServer(fixture.url);
  document.querySelector('#dlgOk').click();
  await waitForDom(window, () => document.querySelectorAll('.server-item').length === 1);
  assert.equal(document.querySelector('.server-item.active').dataset.url, second.url);
  assert.equal(document.querySelector('#connectForm').dataset.url, second.url);
});

test('renaming an unreadable credential cannot silently turn an unchanged empty field into credential deletion', async t => {
  const server = { ...fixture, password: '', credentialState: 'unavailable' };
  const { window, document, calls } = await createLogin(t, { servers: [server] });
  window.editServer(server.url);
  document.querySelector('#addName').value = 'Renamed';
  document.querySelector('#addServerForm').dispatchEvent(new window.Event('submit', { cancelable: true }));
  assert.equal(calls.some(call => call.method === 'saveServerNamed'), false);
  assert.equal(document.querySelector('#addErrorText').textContent,
    '无法读取已保存密码，请重新输入或忽略已保存凭据。');
  assert.equal(document.querySelector('#ignoreCredentialsBtn').style.display, 'flex');
  document.querySelector('#addPassword').value = 'fixture-replacement-password';
  document.querySelector('#addPassword').dispatchEvent(new window.Event('input', { bubbles: true }));
  document.querySelector('#addServerForm').dispatchEvent(new window.Event('submit', { cancelable: true }));
  await waitForDom(window, () => document.querySelector('#addServerForm').style.display === 'none');
  assert.deepEqual(calls.find(call => call.method === 'saveServerNamed').args,
    [server.url, plain('fixture-replacement-password'), 'Renamed']);
});
