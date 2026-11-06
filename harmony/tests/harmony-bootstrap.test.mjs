import { test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import ts from 'typescript';

const bootstrapUrl = [
  new URL('../entry/src/main/resources/rawfile/harmony-bootstrap.js', import.meta.url),
  new URL('../entry/src/main/resources/rawfile/harmony-bootstrap.js', import.meta.url)
].find(existsSync);
assert.ok(bootstrapUrl, 'the shipped HarmonyOS bootstrap must exist');
const code = readFileSync(bootstrapUrl, 'utf8');
function createPage({ origin = 'https://example.com', path = '/', subframe = false } = {}) {
  const events = new Map();
  const dispatched = [];
  const timers = new Map();
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
    setTimeout(handler) { timers.set(++timerId, handler); return timerId; },
    clearTimeout(id) { timers.delete(id); },
    CustomEvent: class { constructor(type, options) { this.type = type; this.detail = options.detail; } }
  });
  return { window, events, timers, dispatched };
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
