import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { runInNewContext } from 'node:vm';
import * as model from '../entry/src/main/ets/models/NativeEvents.ts';
import * as cookies from '../entry/src/main/ets/models/NativeCookies.ts';
import { parseServerAddress } from '../entry/src/main/ets/models/ServerAddress.ts';

const event = (kind = 'session_update', status = 'completed', id = 'evt_100_1', fields = {}) => ({
  type: 'event', event: kind, id, data: { session_id: 'session-1', task_id: '42', status, ...fields }
});
const initial = () => ({ cursor: '', recentIds: [] });
const forgeEvent = (type = 'issue', number = 12, runId = 0, change = 'opened') => ({
  type: 'event', event: 'forge_event', id: 'evt_100_50', data: {
    event: { owner: 'example', repo: 'project', item_type: type, number, run_id: runId,
      event_type: change, project_path: '/workspace/project' },
    item: { type, number, title: 'A repository change' }
  }
});

test('notification policy mirrors persisted cursor and notifiable statuses independently', () => {
  assert.equal(model.advancesNativeCursor(event('task_update', 'running')), false);
  assert.equal(model.nativeEventDecision(event('task_update', 'running'), initial(), false).notify, true);
  assert.equal(model.advancesNativeCursor(event('live_event', '')), false);
  assert.equal(model.advancesNativeCursor(event('usage_meter_update', '')), true);
  assert.equal(model.advancesNativeCursor(event('chat_stream', '', 'evt_100_2', { event_type: 'user_message' })), true);
  assert.equal(model.advancesNativeCursor(event('session_update', 'running')), false);
});

test('fresh history, replay, foreground and duplicates do not raise notifications; approvals stay visible', () => {
  const completed = event();
  for (const [value, state, foreground, fresh] of [
    [completed, initial(), true, false],
    [completed, initial(), false, true],
    [{ ...completed, replayed: true }, initial(), false, false],
    [{ ...completed, suppress_notification: true }, initial(), false, false],
    [completed, { cursor: completed.id, recentIds: [completed.id] }, false, false]
  ]) {
    const decision = model.nativeEventDecision(value, state, foreground, fresh);
    assert.equal(decision.notify, false);
    assert.equal(decision.advance, true);
  }
  const approval = { ...event('session_update', 'permission_pending'), replayed: true, suppress_notification: true };
  assert.equal(model.nativeEventDecision(approval, initial(), false).notify, true);
  assert.equal(model.nativeEventDecision(approval, initial(), false).dismiss, false);
  assert.equal(model.nativeEventDecision(approval, initial(), false, true).notify, false);
});

test('read, resolved approvals and suppressed completed subjects retract native notifications', () => {
  for (const value of [event('session_update', 'read'), event('task_update', 'read'),
    event('session_update', 'permission_resolved'), { ...event(), suppress_notification: true }]) {
    assert.equal(model.nativeEventDecision(value, initial(), false).dismiss, true);
  }
  assert.equal(model.nativeEventDecision(event('session_update', 'read'), initial(), false).notify, false);
});

test('late replay cannot regress numeric cursors and bounded dedup survives restarts', () => {
  let state = { cursor: 'evt_1000000000000000001_12', recentIds: [] };
  state = model.recordNativeEvent(state, event('session_update', 'completed', 'evt_1000000000000000001_9'));
  assert.equal(state.cursor, 'evt_1000000000000000001_12');
  assert.equal(model.newestEventId(state.cursor, 'evt_1000000000000000002_1'), 'evt_1000000000000000002_1');
  for (let i = 1; i <= 600; i++) state = model.recordNativeEvent(state, event('task_update', 'running', `evt_200_${i}`));
  assert.equal(state.cursor, 'evt_1000000000000000001_12');
  assert.equal(state.recentIds.length, 512);
  assert.equal(model.nativeEventDecision(event('task_update', 'running', 'evt_200_600'),
    JSON.parse(JSON.stringify(state)), false).duplicate, true);
});

test('server navigation is validated and notification identifiers are separated by server and subject', () => {
  const nav = model.navigationForEvent(event('task_update', 'completed', 'evt_1_1',
    { execution_id: '54', project_path: '/workspace/demo', message_id: 7 }));
  assert.deepEqual(nav, { projectPath: '/workspace/demo', taskId: '42', executionId: '54' });
  assert.equal(model.notificationSubject(nav), 'task:42');
  assert.equal(model.navigationEventName(nav), 'clawbench-open-task');
  assert.equal(model.isAllowedNotificationLaunch({ origin: 'https://one.example', navigation: nav },
    ['https://one.example']), true);
  assert.equal(model.isAllowedNotificationLaunch({ origin: 'https://two.example', navigation: nav },
    ['https://one.example']), false);
  assert.equal(model.isAllowedNotificationLaunch({ origin: 'https://one.example',
    navigation: { taskId: 'javascript:alert(1)' } }, ['https://one.example']), false);
  assert.equal(model.isAllowedNotificationLaunch({ origin: 'https://one.example',
    navigation: { taskId: '42', unexpected: 'value' } }, ['https://one.example']), false);
  assert.notEqual(model.notificationIdentity('https://one.example', 'task:42'),
    model.notificationIdentity('https://two.example', 'task:42'));
  assert.notEqual(model.notificationIdentity('https://one.example', 'task:42'),
    model.notificationIdentity('https://one.example', 'session:42'));
});

test('meeting and usage navigation plus plain text are bounded and malformed events are ignored', () => {
  const meeting = event('live_event', '', 'evt_1_3', { token: 'meeting-token', title: 'Title', body: 'Body' });
  assert.equal(model.navigationEventName(model.navigationForEvent(meeting)), 'clawbench-open-meeting');
  assert.deepEqual(model.navigationForEvent(event('usage_meter_update', '')), { usageMeter: true });
  assert.equal(model.notificationText(event('session_update', 'permission_pending', 'evt_1_3',
    { tool_name: 'Terminal\r\n' }), 'en').body, 'Terminal  ');
  assert.equal(model.notificationText(event('session_update', 'completed', 'evt_1_3',
    { response_preview_plain: 'x'.repeat(4000) }), 'en').body.length, 700);
  for (const malformed of ['', '[]', 'null', '{"type":"event","data":{}}', 'x'.repeat(262145)]) {
    assert.equal(model.parseNativeEvent(malformed), undefined);
  }
});

test('forge live events notify without advancing a non-persisted cursor or alerting on replay', () => {
  const state = { cursor: 'evt_100_7', recentIds: [] };
  for (const change of ['opened', 'closed', 'merged', 'reopened', 'commented', 'pipeline_done']) {
    const value = forgeEvent('issue', 12, 0, change);
    assert.equal(model.nativeEventDecision(value, state, false).notify, true);
    assert.equal(model.advancesNativeCursor(value), false);
    assert.equal(model.recordNativeEvent(state, value).cursor, 'evt_100_7');
    assert.equal(model.nativeEventDecision({ ...value, replayed: true }, state, false).notify, false);
    assert.equal(model.nativeEventDecision({ ...value, suppress_notification: true }, state, false).notify, false);
    assert.equal(model.nativeEventDecision(value, state, false, true).notify, false);
  }
  assert.equal(model.nativeEventDecision(forgeEvent('issue', 12, 0, 'unrecognized'), state, false).notify, false);
});

test('forge targets retain issue, PR and pipeline identities and reject altered cold navigation', () => {
  for (const value of [forgeEvent(), forgeEvent('pr', 23), forgeEvent('pipeline', 0, 555, 'pipeline_done')]) {
    const nav = model.navigationForEvent(value);
    const { item_type: type, number, run_id: runId } = value.data.event;
    assert.equal(nav.projectPath, '/workspace/project');
    assert.deepEqual(nav.forgeTarget, {
      type, number, runId, projectPath: '/workspace/project',
      itemKey: type === 'pipeline' ? 'pipeline/run:555' : `${type}/${number}`
    });
    assert.equal(model.navigationEventName(nav), 'clawbench-open-forge');
    assert.equal(model.isAllowedNotificationLaunch({ origin: 'https://one.example', navigation: nav },
      ['https://one.example']), true);
    for (const changed of [
      { ...nav, forgeTarget: { ...nav.forgeTarget, itemKey: 'pipeline/0' } },
      { ...nav, forgeTarget: { ...nav.forgeTarget, extra: 'injected' } },
      { ...nav, projectPath: '/workspace/different' },
      { ...nav, forgeTarget: { ...nav.forgeTarget, runId: -1 } },
      { ...nav, sessionId: 'unrelated' }
    ]) {
      assert.equal(model.isAllowedNotificationLaunch({ origin: 'https://one.example', navigation: changed },
        ['https://one.example']), false);
    }
  }
  for (const value of [forgeEvent('pipeline', 0, 0), forgeEvent('issue', -1),
    forgeEvent('pr', Number.MAX_SAFE_INTEGER + 1), forgeEvent('unknown', 4)]) {
    assert.equal(model.navigationForEvent(value), undefined);
  }
  assert.doesNotMatch(model.notificationText(forgeEvent('pipeline', 0, 555, 'pipeline_done'), 'en').title, /#0/);
  assert.match(model.notificationText(forgeEvent('pr', 23, 0, 'merged'), 'en').title, /Pull request #23.*Merged/);
});

test('native cookies use the actual login scope, not public ports, and exclude other servers', () => {
  cookies.registerServerCookies('https://proxy.example', ['cb20300_clawbench_session=one; HttpOnly; Path=/']);
  const header = cookies.nativeCookieHeader('https://proxy.example',
    'clawbench_session=wrong; cb20300_clawbench_session=one; cb20300_clawbench_project=%2Fdemo; cb20400_clawbench_project=private; other=secret');
  assert.equal(header, 'cb20300_clawbench_session=one; cb20300_clawbench_project=%2Fdemo');
  assert.equal(cookies.nativeCookieHeader('https://proxy.example',
    'cb20300_clawbench_session=overwritten; cb20300_clawbench_project=other-server'), 'cb20300_clawbench_session=one');
  cookies.registerServerCookies('https://proxy.example:8443', ['clawbench_session=two; HttpOnly']);
  assert.equal(cookies.nativeCookieHeader('https://proxy.example:8443', 'cb20300_clawbench_session=one'), 'clawbench_session=two');
  assert.throws(() => cookies.nativeCookieHeader('https://unregistered.example', ''), /unavailable/);
  cookies.clearServerCookies('https://proxy.example');
  assert.throws(() => cookies.nativeCookieHeader('https://proxy.example', ''), /unavailable/);
  assert.throws(() => cookies.registerServerCookies('https://ambiguous.example',
    ['clawbench_session=one', 'cb20001_clawbench_session=two']), /exactly one/);
});

const serviceSource = stripTypeScriptTypes(readFileSync(new URL(
  '../entry/src/main/ets/services/NativeEventService.ets', import.meta.url), 'utf8')
  .replace(/^import[\s\S]*?;\r?\n/gm, '').replace('export class NativeEventService', 'class NativeEventService')) +
  '\nglobalThis.Service = NativeEventService;';

function harness() {
  const storage = new Map();
  const app = new Map();
  const sockets = [];
  const published = [];
  const cancelled = [];
  const requests = [];
  const responses = [];
  const reports = [];
  const timers = new Map();
  const permissionReads = [];
  const configRequests = [];
  const configResponses = [];
  let permissions = false;
  let grant = true;
  let permissionWait;
  let permissionRequests = 0;
  let nextUuid = 0;
  const prefs = {
    async get(key, fallback) { return storage.get(key) ?? fallback; },
    async put(key, value) { storage.set(key, value); },
    async flush() {}
  };
  const sandbox = {
    ...model, ...cookies, parseServerAddress,
    AppStorage: { get: key => app.get(key), setOrCreate: (key, value) => app.set(key, value) },
    preferences: { async getPreferences() { return prefs; } },
    notificationManager: {
      async isNotificationEnabled() {
        return permissionReads.length ? await permissionReads.shift()() : permissions;
      },
      async requestEnableNotification() {
        permissionRequests++;
        if (permissionWait) await permissionWait;
        permissions = grant;
      },
      async cancel(...args) { cancelled.push(args); },
      async cancelGroup(group) { cancelled.push(['group', group]); },
      async publish(request) { published.push(request); },
      SlotType: { SERVICE_INFORMATION: 2 }, ContentType: { NOTIFICATION_CONTENT_BASIC_TEXT: 0 }
    },
    util: { generateRandomUUID() { return (++nextUuid).toString(16).padStart(32, '0'); } },
    wantAgent: {
      async getWantAgent(info) { return info; },
      OperationType: { START_ABILITY: 1 }, WantAgentFlags: { UPDATE_PRESENT_FLAG: 1, CONSTANT_FLAG: 2 }
    },
    webview: { WebCookieManager: { fetchCookieSync() { return 'clawbench_session=fixture'; } } },
    webSocket: { createWebSocket() {
      const handlers = new Map();
      const socket = { handlers, sends: [], closed: false,
        on(name, callback) { handlers.set(name, callback); }, off(name) { handlers.delete(name); },
        async connect(url, options) { this.url = url; this.options = options; },
        async close() { this.closed = true; }, async send(value) { this.sends.push(value); }
      };
      sockets.push(socket); return socket;
    } },
    http: {
      createHttp() {
        const request = { destroyed: false, async request(url, options) {
          this.url = url; this.options = options;
          if (url.endsWith('/api/config')) {
            configRequests.push(request);
            return configResponses.length ? await configResponses.shift()() :
              { responseCode: 200, result: '{"push_mode":"native"}' };
          }
          requests.push(request);
          return responses.length ? await responses.shift()() : { responseCode: 200, result: '{"events":[]}' };
        }, destroy() { this.destroyed = true; } };
        return request;
      },
      RequestMethod: { GET: 'GET' }, HttpDataType: { STRING: 1 }
    },
    setTimeout(fn) { const id = timers.size + 1; timers.set(id, fn); return id; },
    clearTimeout(id) { timers.delete(id); }
  };
  runInNewContext(serviceSource, sandbox);
  const service = new sandbox.Service((name, detail) => reports.push({ name, detail: JSON.parse(detail) }));
  const context = { abilityInfo: { bundleName: 'com.clawbench.fixture', name: 'EntryAbility' } };
  return {
    service, context, sockets, requests, published, cancelled, storage, app, reports, timers, responses, permissionReads,
    configRequests, configResponses,
    Service: sandbox.Service,
    permissionRequests: () => permissionRequests,
    setGrant(value) { grant = value; },
    setPermission(value) { permissions = value; },
    waitPermission(promise) { permissionWait = promise; },
    async ready(origin = 'https://fixture.example', foreground = false) {
      cookies.registerServerCookies(origin, ['clawbench_session=fixture; HttpOnly']);
      await service.init(context); await service.bind(origin, 'en', foreground); await service.setEnabled(true);
    },
    async message(value) {
      const socket = sockets.at(-1); socket.handlers.get('message')?.(null, JSON.stringify(value));
      await service.queue;
    }
  };
}

test('startup asks for notification permission without a server and never enables a delivery mode', async () => {
  const h = harness();
  await h.service.init(h.context);
  await h.service.requestStartupPermission();
  assert.equal(h.permissionRequests(), 1);
  assert.equal(JSON.parse(h.service.getStatus()).permission, 'granted');
  assert.equal(JSON.parse(h.service.getStatus()).enabled, false);
  assert.equal(h.sockets.length, 0);
  assert.equal(h.storage.has('servers'), false);
  await h.service.requestStartupPermission();
  h.service.setForeground(false);
  h.service.setForeground(true);
  await h.service.requestStartupPermission();
  assert.equal(h.permissionRequests(), 1);
  h.service.destroy();
});

test('startup skips an existing grant and defers permission requests while backgrounded', async () => {
  const h = harness();
  await h.service.init(h.context);
  h.service.setForeground(false);
  await h.service.requestStartupPermission();
  assert.equal(h.permissionRequests(), 0);
  h.setPermission(true);
  h.service.setForeground(true);
  await h.service.requestStartupPermission();
  assert.equal(h.permissionRequests(), 0);
  assert.equal(JSON.parse(h.service.getStatus()).permission, 'granted');
  h.service.destroy();
});

test('denying startup permission leaves login usable and does not repeatedly prompt', async () => {
  const h = harness();
  h.setGrant(false);
  await h.service.init(h.context);
  await h.service.requestStartupPermission();
  await h.service.requestStartupPermission();
  assert.equal(h.permissionRequests(), 1);
  assert.equal(JSON.parse(h.service.getStatus()).permission, 'denied');
  assert.equal(h.sockets.length, 0);
  await h.service.bind('https://denied.example', 'en', true);
  assert.equal(h.service.active.origin, 'https://denied.example');
  assert.equal(h.service.active.enabled, false);
  h.service.destroy();
});

test('startup and a concurrent push enable share one system authorization dialog', async () => {
  const h = harness();
  await h.service.init(h.context);
  await h.service.bind('https://fixture.example', 'en', true);
  let grant;
  h.waitPermission(new Promise(resolve => { grant = resolve; }));
  const startup = h.service.requestStartupPermission();
  const enabling = h.service.setEnabled(true);
  await new Promise(setImmediate);
  assert.equal(h.permissionRequests(), 1);
  grant();
  await Promise.all([startup, enabling]);
  assert.equal(JSON.parse(h.service.getStatus()).permission, 'granted');
  assert.equal(JSON.parse(h.service.getStatus()).enabled, true);
  assert.equal(h.permissionRequests(), 1);
  h.service.destroy();
});

test('a startup authorization cannot undo a newer disabled setting', async () => {
  const h = harness();
  await h.service.init(h.context);
  await h.service.bind('https://fixture.example', 'en', true);
  let grant;
  h.waitPermission(new Promise(resolve => { grant = resolve; }));
  const startup = h.service.requestStartupPermission();
  const enabling = h.service.setEnabled(true);
  await new Promise(setImmediate);
  await h.service.setEnabled(false);
  grant();
  await Promise.all([startup, enabling]);
  assert.equal(JSON.parse(h.service.getStatus()).enabled, false);
  assert.equal(h.service.active.enabled, false);
  assert.equal(h.sockets.length, 0);
  h.service.destroy();
});

test('startup permission service failure does not reject application startup', async () => {
  const h = harness();
  await h.service.init(h.context);
  h.permissionReads.push(
    () => Promise.reject(new Error('OS service unavailable')),
    () => Promise.reject(new Error('OS service unavailable'))
  );
  await h.service.requestStartupPermission();
  assert.equal(JSON.parse(h.service.getStatus()).permission, 'unknown');
  assert.equal(h.permissionRequests(), 0);
  h.service.destroy();
});

test('disposing the page before a permission query completes prevents a late dialog', async () => {
  const h = harness();
  await h.service.init(h.context);
  let finish;
  h.permissionReads.push(() => new Promise(resolve => { finish = resolve; }));
  const startup = h.service.requestStartupPermission();
  h.service.destroy();
  finish(false);
  await startup;
  assert.equal(h.permissionRequests(), 0);
  assert.equal(h.sockets.length, 0);
});

test('native server mode starts background delivery without any frontend enable call', async () => {
  const h = harness();
  h.setPermission(true);
  cookies.registerServerCookies('https://automatic.example', ['clawbench_session=automatic; HttpOnly']);
  await h.service.init(h.context);
  await h.service.bind('https://automatic.example', 'en', false);
  assert.equal(h.sockets.length, 1);
  assert.equal(h.permissionRequests(), 0);
  assert.equal(JSON.parse(h.service.getStatus()).enabled, true);
  assert.equal(h.configRequests[0].options.header.Cookie, 'clawbench_session=automatic');
  assert.equal(h.configRequests[0].options.maxRedirects, 0);
  assert.equal(h.configRequests[0].options.usingCache, false);
  await h.message(event('session_update', 'completed'));
  assert.equal(h.published.length, 1);
  await h.message(event('session_update', 'permission_pending', 'evt_100_2'));
  assert.equal(h.published.length, 2);
  h.service.destroy();
});

test('server disabled and IM modes never activate native delivery', async () => {
  for (const mode of ['disabled', 'dingtalk', 'feishu', 'wechat']) {
    const h = harness();
    h.setPermission(true);
    h.configResponses.push(() => ({ responseCode: 200, result: JSON.stringify({ push_mode: mode }) }));
    cookies.registerServerCookies('https://mode.example', ['clawbench_session=fixture; HttpOnly']);
    await h.service.init(h.context);
    await h.service.bind('https://mode.example', 'en', false);
    assert.equal(h.service.active.enabled, false);
    assert.equal(h.sockets.length, 0);
    assert.equal(h.cancelled.at(-1)[0], 'group');
    assert.equal(h.permissionRequests(), 0);
    h.service.destroy();
  }
});

test('background transition rereads server settings before opening a native socket', async () => {
  const h = harness();
  await h.ready('https://fixture.example', true);
  await h.service.queue;
  let finishSettings;
  h.configResponses.push(() => new Promise(resolve => { finishSettings = resolve; }));
  h.service.setForeground(false);
  await new Promise(setImmediate);
  assert.equal(h.sockets.length, 0);
  finishSettings({ responseCode: 200, result: '{"push_mode":"disabled"}' });
  await new Promise(setImmediate);
  assert.equal(h.sockets.length, 0);
  assert.equal(h.service.active.enabled, false);
  assert.equal(h.configRequests.length, 2);
  h.service.destroy();
});

test('native mode selected on an unadapted frontend takes effect on the next background transition', async () => {
  const h = harness();
  h.setPermission(true);
  h.configResponses.push(() => ({ responseCode: 200, result: '{"push_mode":"disabled"}' }));
  cookies.registerServerCookies('https://mode.example', ['clawbench_session=fixture; HttpOnly']);
  await h.service.init(h.context);
  await h.service.bind('https://mode.example', 'en', true);
  assert.equal(h.sockets.length, 0);
  h.service.setForeground(false);
  await new Promise(setImmediate);
  assert.equal(h.sockets.length, 1);
  await h.message(event('session_update', 'permission_pending'));
  assert.equal(h.published.length, 1);
  h.service.destroy();
});

test('a late server config read cannot undo an explicit notification disable', async () => {
  const h = harness();
  await h.ready();
  let finishSettings;
  h.configResponses.push(() => new Promise(resolve => { finishSettings = resolve; }));
  const syncing = h.service.syncServerSettings();
  await h.service.setEnabled(false);
  finishSettings({ responseCode: 200, result: '{"push_mode":"native"}' });
  await syncing;
  assert.equal(h.service.active.enabled, false);
  assert.equal(JSON.parse(h.service.getStatus()).enabled, false);
  assert.equal(h.sockets.length, 1);
  assert.equal(h.sockets[0].closed, true);
  h.service.destroy();
});

test('server switches abort old configuration reads and isolate their late modes', async () => {
  const h = harness();
  h.setPermission(true);
  cookies.registerServerCookies('https://old.example', ['clawbench_session=old; HttpOnly']);
  cookies.registerServerCookies('https://new.example', ['clawbench_session=new; HttpOnly']);
  await h.service.init(h.context);
  let finishSettings;
  h.configResponses.push(() => new Promise(resolve => { finishSettings = resolve; }));
  const oldBind = h.service.bind('https://old.example', 'en', false);
  h.configResponses.push(() => ({ responseCode: 200, result: '{"push_mode":"disabled"}' }));
  await h.service.bind('https://new.example', 'en', false);
  assert.equal(h.configRequests[0].destroyed, true);
  finishSettings({ responseCode: 200, result: '{"push_mode":"native"}' });
  await oldBind;
  assert.equal(h.service.active.origin, 'https://new.example');
  assert.equal(h.service.active.enabled, false);
  assert.equal(h.sockets.length, 0);
  h.service.destroy();
});

test('invalid or failed server config reads do not invent an enabled native preference', async () => {
  for (const response of [
    { responseCode: 302, result: '' },
    { responseCode: 401, result: '{}' },
    { responseCode: 200, result: 'broken' },
    { responseCode: 200, result: '[]' },
    { responseCode: 200, result: '{"push_mode":true}' },
    { responseCode: 200, result: '{"push_mode":"unknown"}' }
  ]) {
    const h = harness();
    h.setPermission(true);
    cookies.registerServerCookies('https://invalid.example', ['clawbench_session=fixture; HttpOnly']);
    h.configResponses.push(() => response);
    await h.service.init(h.context);
    await h.service.bind('https://invalid.example', 'en', false);
    assert.equal(h.service.active.enabled, false);
    assert.equal(h.sockets.length, 0);
    assert.match(JSON.parse(h.service.getStatus()).error, /settings unavailable/);
    h.service.destroy();
  }
});

test('server native preference is retained under OS denial and resumes after a permission grant', async () => {
  const h = harness();
  cookies.registerServerCookies('https://permission.example', ['clawbench_session=fixture; HttpOnly']);
  await h.service.init(h.context);
  await h.service.bind('https://permission.example', 'en', true);
  assert.equal(h.service.active.enabled, true);
  assert.equal(JSON.parse(h.service.getStatus()).enabled, false);
  assert.equal(h.permissionRequests(), 0);
  h.setPermission(true);
  h.service.setForeground(false);
  await new Promise(setImmediate);
  assert.equal(h.sockets.length, 1);
  assert.equal(JSON.parse(h.service.getStatus()).permission, 'granted');
  h.service.destroy();
});

test('notification denial is reported and no background connection is started', async () => {
  const h = harness();
  h.setGrant(false);
  await h.service.init(h.context);
  await h.service.bind('https://denied.example', 'en', false);
  await assert.rejects(h.service.setEnabled(true), /permission/);
  assert.equal(h.sockets.length, 0);
  assert.equal(JSON.parse(h.service.getStatus()).enabled, false);
  assert.match(h.reports.at(-1).detail.error, /permission/);
});

test('native live events publish once, read retracts, and cold launch consumes only an issued token', async () => {
  const h = harness();
  await h.ready();
  await h.message(event());
  await h.message(event());
  assert.equal(h.published.length, 1);
  const notification = h.published[0];
  const token = notification.wantAgent.wants[0].parameters.clawbenchNotificationToken;
  assert.equal(Object.keys(notification.wantAgent.wants[0].parameters).length, 1);
  assert.equal(notification.content.normal.title, 'Session completed');
  h.Service.receiveWant({ parameters: { clawbenchNotificationToken: token } });
  const launch = await h.service.takeLaunch(['https://fixture.example']);
  assert.equal(launch.origin, 'https://fixture.example');
  h.service.queueNavigation(launch);
  assert.equal(JSON.parse(h.service.takePendingNavigation()).sessionId, 'session-1');
  assert.equal(h.service.takePendingNavigation(), '');
  h.Service.receiveWant({ parameters: { clawbenchNotificationToken: token } });
  assert.equal(await h.service.takeLaunch(['https://fixture.example']), undefined);
  await h.message(event('session_update', 'read', 'evt_100_2'));
  assert.equal(h.cancelled.length, 1);
  assert.equal(h.cancelled[0][0], notification.id);
  h.Service.receiveWant({ parameters: { clawbenchNotificationToken: 'f'.repeat(32), origin: 'https://evil.example' } });
  assert.equal(await h.service.takeLaunch(['https://fixture.example']), undefined);
  h.service.destroy();
});

test('forge system notification opens the precise project and pipeline after a cold launch', async () => {
  const h = harness();
  await h.ready();
  await h.message(forgeEvent('pipeline', 0, 555, 'pipeline_done'));
  await h.message(forgeEvent('pipeline', 0, 555, 'pipeline_done'));
  assert.equal(h.published.length, 1);
  const notification = h.published[0];
  assert.match(notification.content.normal.title, /example\/project.*Pipeline.*Finished/);
  assert.equal(notification.content.normal.text, 'A repository change');
  const token = notification.wantAgent.wants[0].parameters.clawbenchNotificationToken;
  h.Service.receiveWant({ parameters: { clawbenchNotificationToken: token } });
  const launch = await h.service.takeLaunch(['https://fixture.example']);
  assert.equal(launch.navigation.projectPath, '/workspace/project');
  assert.equal(launch.navigation.forgeTarget.itemKey, 'pipeline/run:555');
  assert.equal(model.navigationEventName(launch.navigation), 'clawbench-open-forge');
  assert.equal(h.service.active.state.cursor, '');
  h.service.destroy();
});

test('server switches drop late messages, keep cursors isolated, and gate pending navigation', async () => {
  const h = harness();
  await h.ready();
  const oldCallback = h.sockets[0].handlers.get('message');
  await h.message(event());
  h.service.queueNavigation({ origin: 'https://fixture.example', navigation: { sessionId: 'session-1' } });
  cookies.registerServerCookies('https://second.example', ['cb20300_clawbench_session=second; HttpOnly']);
  await h.service.bind('https://second.example', 'en', false);
  assert.equal(h.service.takePendingNavigation(), '');
  oldCallback(null, JSON.stringify(event('session_update', 'completed', 'evt_100_3')));
  await h.service.queue;
  assert.equal(h.published.length, 1);
  await h.service.setEnabled(true);
  assert.equal(h.sockets.at(-1).options.header.Cookie, 'cb20300_clawbench_session=second');
  await h.message(event('session_update', 'completed', 'evt_100_3'));
  assert.equal(h.published.length, 2);
  const states = JSON.parse(h.storage.get('servers'));
  assert.equal(states.find(value => value.origin === 'https://fixture.example').state.cursor, 'evt_100_1');
  assert.equal(states.find(value => value.origin === 'https://second.example').state.cursor, 'evt_100_3');
  h.service.destroy();
});

test('foreground suppresses publication and closes background socket; disable cancels group', async () => {
  const h = harness();
  await h.ready();
  const oldSocket = h.sockets.at(-1);
  h.service.setForeground(true);
  await new Promise(setImmediate);
  await h.service.queue;
  assert.equal(oldSocket.closed, true);
  assert.equal(h.published.length, 0);
  assert.equal(h.requests[0].options.maxRedirects, 0);
  await h.service.setEnabled(false);
  assert.equal(h.cancelled.at(-1)[0], 'group');
  assert.equal(JSON.parse(h.service.getStatus()).backgroundDelivery, 'while-process-running');
  h.service.destroy();
});

function backlog(...events) {
  return () => ({ responseCode: 200, result: JSON.stringify({ events: events.map(value => ({
    event_id: value.id, payload: JSON.stringify(value), suppress_notification: value.suppress_notification
  })) }) });
}

test('HTTP recovery silently baselines fresh history, then delivers unread gaps once and advances suppressed rows', async () => {
  const h = harness();
  await h.ready();
  h.responses.push(backlog(event('session_update', 'completed', 'evt_100_1')));
  h.sockets[0].handlers.get('open')();
  await h.service.queue;
  assert.equal(h.published.length, 0);
  assert.equal(h.service.active.state.cursor, 'evt_100_1');
  h.responses.push(backlog(
    { ...event('session_update', 'completed', 'evt_100_2'), suppress_notification: true },
    event('session_update', 'completed', 'evt_100_3')));
  h.service.enqueueRecovery();
  await h.service.queue;
  assert.match(h.requests.at(-1).url, /after=evt_100_1$/);
  assert.equal(h.published.length, 1);
  assert.equal(h.cancelled.length, 1);
  assert.equal(h.service.active.state.cursor, 'evt_100_3');
  await h.message(event('session_update', 'completed', 'evt_100_3'));
  assert.equal(h.published.length, 1);
  h.service.destroy();
});

test('expired pending cursor is rebased without a storm of historical alerts', async () => {
  const h = harness();
  await h.ready();
  await h.service.updateLastSeenEventId('evt_99_10');
  h.responses.push(backlog(), backlog(event('session_update', 'completed', 'evt_100_1')));
  h.sockets[0].handlers.get('open')();
  await h.service.queue;
  assert.equal(h.requests.length, 2);
  assert.equal(h.service.active.state.cursor, 'evt_100_1');
  assert.equal(h.published.length, 0);
  h.service.destroy();
});

test('switching servers aborts a pending HTTP request and ignores its late response', async () => {
  const h = harness();
  await h.ready();
  await h.service.updateLastSeenEventId('evt_100_1');
  let resolve;
  h.responses.push(() => new Promise(done => { resolve = done; }));
  h.sockets[0].handlers.get('open')();
  const recovery = h.service.queue;
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(h.requests.length, 1);
  await h.service.bind('https://new.example', 'en', false);
  assert.equal(h.requests[0].destroyed, true);
  resolve(backlog(event('session_update', 'completed', 'evt_100_2'))());
  await recovery;
  assert.equal(h.published.length, 0);
  assert.equal(h.service.records.find(value => value.origin === 'https://fixture.example').state.cursor, 'evt_100_1');
  assert.equal(h.service.active.state.cursor, '');
  h.service.destroy();
});

test('turning notifications off wins over a late permission grant', async () => {
  const h = harness();
  await h.service.init(h.context);
  await h.service.bind('https://fixture.example', 'en', false);
  let grant;
  h.waitPermission(new Promise(resolve => { grant = resolve; }));
  const enabling = h.service.setEnabled(true);
  await Promise.resolve();
  await h.service.setEnabled(false);
  grant();
  await enabling;
  assert.equal(JSON.parse(h.service.getStatus()).enabled, false);
  assert.equal(h.sockets.length, 0);
  h.service.destroy();
});

test('OS permission revocation stops native delivery and a later grant restores the requested mode', async () => {
  const h = harness();
  await h.ready();
  const socket = h.sockets.at(-1);
  h.setPermission(false);
  const denied = JSON.parse(await h.service.refreshStatus());
  assert.equal(denied.permission, 'denied');
  assert.equal(denied.enabled, false);
  assert.equal(socket.closed, true);
  assert.equal(h.service.active.enabled, true);
  h.setPermission(true);
  const restored = JSON.parse(await h.service.refreshStatus());
  assert.equal(restored.permission, 'granted');
  assert.equal(restored.enabled, true);
  assert.equal(restored.error, '');
  assert.equal(h.sockets.length, 2);
  h.service.destroy();
});

test('a denied first authorization can be granted in system settings without changing push mode', async () => {
  const h = harness();
  h.setGrant(false);
  cookies.registerServerCookies('https://denied.example', ['clawbench_session=fixture; HttpOnly']);
  await h.service.init(h.context);
  await h.service.bind('https://denied.example', 'en', false);
  await assert.rejects(h.service.setEnabled(true), /permission/);
  assert.equal(JSON.parse(h.service.getStatus()).permission, 'denied');
  assert.equal(JSON.parse(h.storage.get('servers'))[0].enabled, true);
  h.setPermission(true);
  assert.equal(JSON.parse(await h.service.refreshStatus()).enabled, true);
  assert.equal(h.sockets.length, 1);
  h.service.destroy();
});

test('permission service failure is unknown, not a fabricated user denial', async () => {
  const h = harness();
  await h.ready();
  h.permissionReads.push(() => Promise.reject(new Error('OS service unavailable')));
  const status = JSON.parse(await h.service.refreshStatus());
  assert.equal(status.permission, 'unknown');
  assert.equal(status.enabled, false);
  assert.match(status.error, /unavailable/);
  assert.equal(h.sockets.at(-1).closed, true);
  assert.equal(JSON.parse(await h.service.refreshStatus()).permission, 'granted');
  h.service.destroy();
});

test('late permission reads cannot overwrite a new server or a newer permission read', async () => {
  const h = harness();
  await h.ready();
  let finishOld;
  h.permissionReads.push(() => new Promise(resolve => { finishOld = resolve; }));
  const oldRead = h.service.refreshStatus();
  cookies.registerServerCookies('https://new.example', ['clawbench_session=new; HttpOnly']);
  h.configResponses.push(() => ({ responseCode: 200, result: '{"push_mode":"disabled"}' }));
  await h.service.bind('https://new.example', 'en', false);
  finishOld(false);
  await oldRead;
  assert.equal(JSON.parse(h.service.getStatus()).permission, 'granted');
  assert.equal(JSON.parse(h.service.getStatus()).enabled, false);
  let finishFirst;
  h.permissionReads.push(() => new Promise(resolve => { finishFirst = resolve; }));
  const first = h.service.refreshStatus();
  await h.service.refreshStatus();
  finishFirst(false);
  await first;
  assert.equal(JSON.parse(h.service.getStatus()).permission, 'granted');
  h.service.destroy();
});

test('an in-flight read cannot undo a completed enable operation', async () => {
  const h = harness();
  await h.ready();
  let finishOld;
  h.permissionReads.push(() => new Promise(resolve => { finishOld = resolve; }));
  const oldRead = h.service.refreshStatus();
  await h.service.setEnabled(true);
  finishOld(false);
  await oldRead;
  assert.equal(JSON.parse(h.service.getStatus()).permission, 'granted');
  assert.equal(JSON.parse(h.service.getStatus()).enabled, true);
  h.service.destroy();
});
