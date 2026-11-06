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

async function createLogin(t, { servers = [], failSave = false, failConnect = false } = {}) {
  const state = { servers: structuredClone(servers), language: 'zh', theme: 'gruvbox-dark', version: APP_VERSION };
  const calls = [];
  const errors = [];
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
          queueMicrotask(() => {
            let error = '';
            let code = '';
            if (request.method === 'setAppearance') {
              state.language = args[0];
              state.theme = args[1];
            } else if (request.method === 'saveServerNamed') {
              if (failSave) error = 'fixture storage failure';
              else {
                state.servers = [{
                  url: args[0], name: args[2], password: args[1] ? 'clawbench-saved:updated' : ''
                }, ...state.servers.filter(server => server.url !== args[0])];
              }
            } else if (request.method === 'removeServer') {
              state.servers = state.servers.filter(server => server.url !== args[0]);
            } else if (request.method === 'connectToServer' && failConnect) {
              error = '密码不正确';
              code = 'password';
            }
            const response = error ? { id: request.id, error, code } :
              { id: request.id, result: JSON.stringify({ value: null, snapshot: state }) };
            nativePort.onmessage({ data: JSON.stringify(response) });
          });
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
  return { window: dom.window, document: dom.window.document, calls, errors };
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
    [fixture.url, 'fixture-input-password', 'Home']);
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
  assert.deepEqual(calls.find(call => call.method === 'connectToServer').args, [fixture.url, fixture.password]);
});

test('renaming an address preserves the password reference and removes the old entry after saving', async t => {
  const { window, document, calls } = await createLogin(t, { servers: [fixture] });
  window.editServer(fixture.url);
  document.querySelector('#addAddress').value = 'https://work.example:20000';
  document.querySelector('#addName').value = 'Work';
  assert.equal(document.querySelector('#addPassword').value, fixture.password);
  document.querySelector('#addServerForm').dispatchEvent(new window.Event('submit', { cancelable: true }));
  await waitForDom(window, () => document.querySelector('#addServerForm').style.display === 'none');
  const mutations = calls.filter(call => ['saveServerNamed', 'removeServer'].includes(call.method));
  assert.deepEqual(mutations.map(call => call.method), ['saveServerNamed', 'removeServer']);
  assert.equal(mutations[0].args[1], fixture.password);
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
    [server.url, fixture.password, 'Renamed']);
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
