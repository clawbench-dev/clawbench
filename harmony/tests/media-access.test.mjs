import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';

// ArkTS resolves extensionless imports; Node's TypeScript loader requires an explicit extension.
const hooks = registerHooks({
  resolve(specifier, context, nextResolve) {
    return nextResolve(specifier === './ServerAddress' ? './ServerAddress.ts' : specifier, context);
  }
});
const { isCurrentMediaOrigin, mediaPermissions, MediaAccessGate, CaptureConsentGate } =
  await import('../entry/src/main/ets/models/MediaAccess.ts');
hooks.deregister();

function request(origin = 'https://example.com', resources = ['TYPE_AUDIO_CAPTURE']) {
  const calls = [];
  return {
    calls,
    getOrigin: () => origin,
    getAccessibleResource: () => resources,
    grant: (items) => calls.push(['grant', items]),
    deny: () => calls.push(['deny'])
  };
}

test('media origin identity requires an exact configured server origin', () => {
  assert.equal(isCurrentMediaOrigin('HTTPS://EXAMPLE.COM:443/', 'https://example.com'), true);
  for (const origin of ['https://example.com.evil', 'https://example.com:444', 'https://example.com/api/fs/raw/x',
    'https://user@example.com', 'data:text/html,hello', 'http://example.com', 'null', '']) {
    assert.equal(isCurrentMediaOrigin(origin, 'https://example.com'), false, origin);
  }
});

test('only camera and microphone resource names map to OS permissions', () => {
  assert.deepEqual(mediaPermissions(['TYPE_AUDIO_CAPTURE', 'TYPE_VIDEO_CAPTURE']),
    ['ohos.permission.MICROPHONE', 'ohos.permission.CAMERA']);
  assert.deepEqual(mediaPermissions(['TYPE_AUDIO_CAPTURE', 'TYPE_AUDIO_CAPTURE']), ['ohos.permission.MICROPHONE']);
  assert.deepEqual(mediaPermissions(['TYPE_VIDEO_CAPTURE', 'TYPE_SENSOR']), []);
  assert.deepEqual(mediaPermissions([]), []);
});

test('media grants only after system permission success for the current page', async () => {
  const gate = new MediaAccessGate();
  const req = request();
  await gate.request(req, 'https://example.com', () => true, async (permissions) => {
    assert.deepEqual(permissions, ['ohos.permission.MICROPHONE']);
    assert.deepEqual(req.calls, []);
    return true;
  });
  assert.deepEqual(req.calls, [['grant', ['TYPE_AUDIO_CAPTURE']]]);
});

test('foreign and unsupported requests never open the system permission prompt', async () => {
  const gate = new MediaAccessGate();
  for (const req of [request('https://other.example'), request('https://example.com', ['TYPE_SENSOR'])]) {
    await gate.request(req, 'https://example.com', () => true, async () => assert.fail('unexpected system prompt'));
    assert.deepEqual(req.calls, [['deny']]);
  }
});

test('media rejection and system errors deny instead of granting partial resources', async () => {
  const gate = new MediaAccessGate();
  const denied = request();
  await gate.request(denied, 'https://example.com', () => true, async () => false);
  assert.deepEqual(denied.calls, [['deny']]);
  const failed = request();
  await gate.request(failed, 'https://example.com', () => true, async () => { throw new Error('OS rejected'); });
  assert.deepEqual(failed.calls, [['deny']]);
});

test('navigation denies a pending prompt once and a late acceptance cannot grant', async () => {
  const gate = new MediaAccessGate();
  const req = request();
  let finish;
  const work = gate.request(req, 'https://example.com', () => true, () => new Promise((resolve) => { finish = resolve; }));
  gate.cancel();
  finish(true);
  await work;
  assert.deepEqual(req.calls, [['deny']]);
});

test('concurrent media prompts are rejected and current page is rechecked after permission', async () => {
  const gate = new MediaAccessGate();
  const first = request();
  const second = request();
  let current = true;
  let finish;
  const work = gate.request(first, 'https://example.com', () => current, () => new Promise((resolve) => { finish = resolve; }));
  await gate.request(second, 'https://example.com', () => true, async () => assert.fail('second prompt'));
  current = false;
  finish(true);
  await work;
  assert.deepEqual(first.calls, [['deny']]);
  assert.deepEqual(second.calls, [['deny']]);
});

test('every screen capture requires a fresh confirmation and no grant follows cancellation', async () => {
  const gate = new CaptureConsentGate();
  const first = request();
  let confirmed = 0;
  let grants = 0;
  await gate.request(first, 'https://example.com', () => true, async (origin) => {
    assert.equal(origin, 'https://example.com');
    confirmed++;
    return true;
  }, () => grants++);
  assert.equal(grants, 1);
  const second = request();
  await gate.request(second, 'https://example.com', () => true, async () => { confirmed++; return false; }, () => grants++);
  assert.equal(confirmed, 2);
  assert.equal(grants, 1);
  assert.deepEqual(second.calls, [['deny']]);
  const late = request();
  let finish;
  const work = gate.request(late, 'https://example.com', () => true,
    () => new Promise((resolve) => { finish = resolve; }), () => grants++);
  gate.cancel();
  finish(true);
  await work;
  assert.equal(grants, 1);
  assert.deepEqual(late.calls, [['deny']]);
});
