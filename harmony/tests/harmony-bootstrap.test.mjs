import { test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import { JSDOM } from 'jsdom';
import ts from 'typescript';

const bootstrapUrl = [
  new URL('../harmony/entry/src/main/resources/rawfile/harmony-bootstrap.js', import.meta.url),
  new URL('../entry/src/main/resources/rawfile/harmony-bootstrap.js', import.meta.url)
].find(existsSync);
assert.ok(bootstrapUrl, 'the shipped HarmonyOS bootstrap must exist');
const code = readFileSync(bootstrapUrl, 'utf8');
function createPage({ origin = 'https://example.com', path = '/', subframe = false } = {}) {
  const events = new Map();
  const dispatched = [];
  const timers = new Map();
  const timerDelays = new Map();
  let timerId = 0;
  const window = {
    addEventListener(name, handler) { events.set(name, handler); },
    dispatchEvent(event) { dispatched.push(event); }
  };
  window.top = subframe ? {} : window;
  runInNewContext(code.replace('__HARMONY_CONFIG__', JSON.stringify({
    origin: 'https://example.com', channel: 'random-channel', language: 'zh', theme: 'gruvbox-dark', version: '0.1.2'
  })), {
    window, location: { origin, pathname: path },
    setTimeout(handler, delay) {
      const id = ++timerId;
      timers.set(id, handler);
      timerDelays.set(id, delay);
      return id;
    },
    clearTimeout(id) { timers.delete(id); timerDelays.delete(id); },
    CustomEvent: class { constructor(type, options) { this.type = type; this.detail = options.detail; } }
  });
  return { window, events, timers, timerDelays, dispatched };
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

test('system mail RPC keeps its encoded draft and reports unavailable handlers to the workbench', async () => {
  const page = createPage();
  const { port, messages } = connectPort(page);
  const uri = 'mailto:?subject=' + encodeURIComponent('会议 #1') +
    '&body=' + encodeURIComponent('https://example.com/share/x\n备注');
  const operation = page.window.ClawBenchHarmony.openExternalUrl(uri);
  const request = messages[0];
  assert.equal(request.method, 'openExternalUrl');
  assert.equal(JSON.parse(request.args[0]), uri);
  assert.equal([...page.timerDelays.values()][0], 300000);
  port.onmessage({ data: JSON.stringify({ id: request.id, error: 'Unable to open a mail app.' }) });
  await assert.rejects(operation, /Unable to open a mail app/);
  assert.equal(page.dispatched[0].type, 'clawbench-native-error');
  assert.equal(page.dispatched[0].detail.method, 'openExternalUrl');
  assert.equal(page.dispatched[0].detail.message, 'Unable to open a mail app.');
});

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
  assert.equal(window.ClawBenchHarmony.getCapabilities().tunnel, true);
  assert.equal(window.ClawBenchHarmony.getCapabilities().nativePush, true);
  assert.equal(window.ClawBenchHarmony.getCapabilities().backgroundDelivery, 'while-process-running');
  assert.equal(window.ClawBenchHarmony.getCapabilities().screenCapture, false);
});

test('the shipped HarmonyOS bridge implements every required shared host method', () => {
  // The standalone maintenance checkout and the integrated upstream tree place
  // the shared web sources differently. Neither layout may silently skip this check.
  const contractUrl = [
    new URL('../../web/src/utils/clawbenchNative.ts', import.meta.url),
    new URL('../clawbench-source%20code/web/src/utils/clawbenchNative.ts', import.meta.url)
  ].find(existsSync);
  assert.ok(contractUrl, 'the shared ClawBenchNative declaration must exist');
  const source = ts.createSourceFile('clawbenchNative.ts', readFileSync(contractUrl, 'utf8'),
    ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
  assert.equal(source.parseDiagnostics.length, 0, 'the shared host contract must parse');
  const contract = source.statements.find(statement =>
    ts.isInterfaceDeclaration(statement) && statement.name.text === 'ClawBenchNative');
  assert.ok(contract, 'ClawBenchNative must remain an explicit shared host contract');
  const required = contract.members.filter(member => !member.questionToken);
  assert.ok(required.length > 0, 'the shared host contract must not be empty');
  const bridge = createPage().window.ClawBenchHarmony;
  for (const member of required) {
    const name = member.name.text;
    assert.ok(Object.hasOwn(bridge, name), `HarmonyOS is missing required host member ${name}`);
    if (ts.isMethodSignature(member) ||
        (ts.isPropertySignature(member) && member.type && ts.isFunctionTypeNode(member.type))) {
      assert.equal(typeof bridge[name], 'function', `${name} must be callable`);
    }
  }
});

test('setSSHPassword sends the supplied credential to native and awaits acknowledgement', async () => {
  const page = createPage();
  const { port, messages } = connectPort(page);
  let acknowledged = false;
  const result = page.window.ClawBenchHarmony.setSSHPassword('fixture-password')
    .then(value => { acknowledged = true; return value; });
  assert.equal(messages.length, 1);
  assert.equal(messages[0].method, 'setSSHPassword');
  assert.deepEqual(messages[0].args, [JSON.stringify('fixture-password')]);
  await Promise.resolve();
  assert.equal(acknowledged, false);
  port.onmessage({ data: JSON.stringify({ id: messages[0].id, result: 'null' }) });
  assert.equal(await result, null);
  assert.equal(acknowledged, true);
  assert.equal(await page.window.ClawBenchHarmony.getPassword(), '');
  assert.equal(page.timers.size, 0);
});

test('setSSHPassword rejects a failed native credential write', async () => {
  const page = createPage();
  const { port, messages } = connectPort(page);
  const result = page.window.ClawBenchHarmony.setSSHPassword('');
  const rejected = assert.rejects(result, /Credential storage unavailable/);
  assert.deepEqual(messages[0].args, [JSON.stringify('')]);
  port.onmessage({ data: JSON.stringify({ id: messages[0].id, error: 'Credential storage unavailable' }) });
  await rejected;
  assert.equal(page.timers.size, 0);
  assert.equal(page.dispatched.length, 1);
  assert.equal(page.dispatched[0].detail.method, 'setSSHPassword');
});

test('fire-and-forget credential writes report failure without an unhandled rejection', async () => {
  const page = createPage();
  const { port, messages } = connectPort(page);
  page.window.ClawBenchHarmony.setSSHPassword('fixture-password');
  port.onmessage({ data: JSON.stringify({ id: messages[0].id, error: 'Credential storage unavailable' }) });
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(page.dispatched.length, 1);
  assert.equal(page.dispatched[0].type, 'clawbench-native-error');
  assert.equal(page.dispatched[0].detail.message, 'Credential storage unavailable');
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

test('setNativePushEnabled allows the user time to read and acknowledge guidance', async () => {
  const page = createPage();
  const { port, messages } = connectPort(page);
  page.window.ClawBenchHarmony.setNativePushEnabled(true);
  assert.equal(messages[0].method, 'setNativePushEnabled');
  assert.equal([...page.timerDelays.values()][0], 300000);
  assert.equal(page.timers.size, 1);
  port.onmessage({ data: JSON.stringify({ id: messages[0].id, result: 'null' }) });
  assert.equal(page.timers.size, 0);
  assert.equal(page.dispatched.length, 0);
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

test('restored port changes reach the frontend through the bounded native event channel', () => {
  const page = createPage();
  const { port } = connectPort(page);
  for (const event of ['clawbench-port-rebound', 'unrecognized-native-event']) {
    port.onmessage({ data: JSON.stringify({ event, detail: { requested: 3000, actual: 3001 } }) });
  }
  assert.equal(page.dispatched.length, 1);
  assert.equal(page.dispatched[0].type, 'clawbench-port-rebound');
  assert.deepEqual(JSON.parse(JSON.stringify(page.dispatched[0].detail)), { requested: 3000, actual: 3001 });
});

test('a failed port-browser request reports an error even when its caller swallows rejection', async () => {
  const page = createPage();
  const { port, messages } = connectPort(page);
  const request = page.window.ClawBenchHarmony.openInSandbox(3000, 'http', '', '/').catch(() => {});
  port.onmessage({ data: JSON.stringify({ id: messages[0].id, error: 'Tunnel is not ready' }) });
  await request;
  assert.equal(page.dispatched.length, 1);
  assert.equal(page.dispatched[0].type, 'clawbench-native-error');
  assert.equal(page.dispatched[0].detail.method, 'openInSandbox');
  assert.equal(page.dispatched[0].detail.message, 'Tunnel is not ready');
});

test('rewrites only the About app-version description, never chat or previews', async () => {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    url: 'https://example.com/', runScripts: 'outside-only', pretendToBeVisual: true
  });
  dom.window.__HARMONY_CONFIG__ = {
    origin: 'https://example.com', channel: 'random-channel', language: 'zh',
    theme: 'gruvbox-dark', version: '0.2.10'
  };
  dom.window.eval(code);
  const document = dom.window.document;
  const wait = () => new Promise(resolve => setTimeout(resolve, 0));
  function aboutRow(text = '当前安装的 Android 应用版本号') {
    const row = document.createElement('div');
    row.setAttribute('data-settings-key', 'appVersion');
    row.innerHTML = '<span class="settings-item__label">App版本</span><div class="settings-item__desc"></div>';
    row.querySelector('.settings-item__desc').textContent = text;
    return row;
  }

  const about = aboutRow();
  document.body.appendChild(about);
  await wait();
  assert.equal(about.querySelector('.settings-item__label').textContent, 'App版本');
  assert.equal(about.querySelector('.settings-item__desc').textContent, '当前安装的 HarmonyOS 应用版本号');

  const unchanged = [
    ['chat', '<div class="chat-message__body">当前安装的 Android 应用版本号</div>'],
    ['code preview', '<pre><code>当前安装的 Android 应用版本号</code></pre>'],
    ['editable', '<div contenteditable="true">当前安装的 Android 应用版本号</div>'],
    ['textarea', '<textarea>当前安装的 Android 应用版本号</textarea>'],
    ['json script', '<script type="application/json">{"note":"当前安装的 Android 应用版本号"}</script>'],
    ['download link', '<a href="/api/apk">下载 Android 应用</a>']
  ];
  for (const [label, html] of unchanged) {
    const host = document.createElement('section');
    host.innerHTML = html;
    document.body.appendChild(host);
    await wait();
    assert.match(host.textContent, /Android 应用版本号|下载 Android 应用/, `${label} must keep the original copy`);
  }

  // Vue may patch the text node instead of replacing the row.
  about.querySelector('.settings-item__desc').firstChild.nodeValue = '当前安装的 Android 应用版本号';
  await wait();
  assert.equal(about.querySelector('.settings-item__desc').textContent, '当前安装的 HarmonyOS 应用版本号');

  // Vue also replaces a text-only element by assigning textContent, which
  // removes the old text node and inserts a fresh one under the description.
  about.querySelector('.settings-item__desc').textContent = '当前安装的 Android 应用版本号';
  await wait();
  assert.equal(about.querySelector('.settings-item__desc').textContent, '当前安装的 HarmonyOS 应用版本号');

  // A late about-page render must still be corrected.
  const lateAbout = aboutRow('Current installed Android app version number');
  document.body.appendChild(lateAbout);
  await wait();
  assert.equal(lateAbout.querySelector('.settings-item__desc').textContent,
    'Current installed HarmonyOS app version number');
  dom.window.close();
});

test('chat insertions never scan their added subtrees for the About copy', async () => {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    url: 'https://example.com/', runScripts: 'outside-only', pretendToBeVisual: true
  });
  dom.window.__HARMONY_CONFIG__ = {
    origin: 'https://example.com', channel: 'random-channel', language: 'zh',
    theme: 'gruvbox-dark', version: '0.2.10'
  };
  dom.window.eval(code);
  const document = dom.window.document;
  const chat = document.createElement('article');
  chat.className = 'chat-message__body';
  chat.innerHTML = '<div><pre><code>' + 'large code block '.repeat(1000) + '</code></pre></div>';
  let subtreeQueries = 0;
  const querySelector = chat.querySelector.bind(chat);
  chat.querySelector = (...args) => {
    subtreeQueries++;
    return querySelector(...args);
  };
  document.body.appendChild(chat);
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(subtreeQueries, 0);
  dom.window.close();
});

test('unrelated settings-row insertions do not query the About copy', async () => {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    url: 'https://example.com/', runScripts: 'outside-only', pretendToBeVisual: true
  });
  dom.window.__HARMONY_CONFIG__ = {
    origin: 'https://example.com', channel: 'random-channel', language: 'zh',
    theme: 'gruvbox-dark', version: '0.2.10'
  };
  dom.window.eval(code);
  const document = dom.window.document;
  const settings = document.createElement('section');
  settings.className = 'settings-page';
  document.body.appendChild(settings);
  await new Promise(resolve => setTimeout(resolve, 0));

  let documentQueries = 0;
  const querySelector = document.querySelector.bind(document);
  document.querySelector = (...args) => {
    documentQueries++;
    return querySelector(...args);
  };
  settings.appendChild(document.createElement('div'));
  await new Promise(resolve => setTimeout(resolve, 0));

  assert.equal(documentQueries, 0);
  dom.window.close();
});
