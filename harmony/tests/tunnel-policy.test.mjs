import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  TunnelPolicy, TunnelFailure, tunnelMapping, validHostFingerprint, validTunnelPort, retryTunnelAutomatically
} from '../entry/src/main/ets/models/TunnelPolicy.ts';

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function fixture(options = {}) {
  const commands = [];
  const saves = [];
  const prompts = [];
  const rebounds = [];
  let cancelled = 0;
  const driver = {
    async execute(command) {
      commands.push(structuredClone(command));
      if (options.execute) return await options.execute(command, commands);
      if (command.method === 'connect' || command.method === 'status') return { ok: true, connected: true };
      if (command.method === 'add') return { ok: true, port: command.port };
      if (command.method === 'probe') return { ok: true, reachable: true };
      return { ok: true };
    },
    cancel() { cancelled++; },
    async prepare(origin, transport, current) {
      if (options.prepare) return await options.prepare(origin, transport, current);
      return { method: 'connect', origin, transport, cookie: 'fixture=token', username: 'clawbench', sshPort: 2222,
        hostKey: options.hostKey ?? '', password: 'fixture-only' };
    },
    async approveHostKey(command, fingerprint, current) {
      prompts.push({ command: structuredClone(command), fingerprint });
      return options.approve ? await options.approve(command, fingerprint, current) : true;
    },
    async persist(origin, settings) {
      saves.push({ origin, settings: structuredClone(settings) });
      if (options.persist) await options.persist(origin, settings, saves);
    }
  };
  const policy = new TunnelPolicy(driver, (requested, actual) => rebounds.push({ requested, actual }));
  policy.setServer('https://one.example', { transport: options.transport ?? 'ssh', mappings: options.mappings ?? [] });
  return { policy, commands, saves, prompts, rebounds, cancelled: () => cancelled };
}

test('mapping validation rejects control characters, invalid ports and URL target hosts', () => {
  for (const port of [0, 65536, -1, 1.2, NaN, Infinity]) assert.equal(validTunnelPort(port), false);
  assert.deepEqual(tunnelMapping(3000, 80, '', 'forward'),
    { port: 3000, targetPort: 80, host: '127.0.0.1', direction: 'forward' });
  for (const host of ['host\nx', 'https://host', 'host/path', 'x&port=80', 'user@host', '[::1]']) {
    assert.throws(() => tunnelMapping(3000, 80, host, 'forward'));
  }
  assert.equal(tunnelMapping(3000, 80, '::1', 'reverse').host, '::1');
  assert.equal(validHostFingerprint('a'.repeat(64)), true);
  for (const pin of ['', 'a'.repeat(63), 'A'.repeat(64), 'z'.repeat(64)]) assert.equal(validHostFingerprint(pin), false);
});

test('concurrent forward binds share one connection and retain both mapping directions', async () => {
  const app = fixture();
  const first = app.policy.addForwardedPort(3000, 80, '');
  const second = app.policy.addReverseForwardedPort(3000, 9000, '127.0.0.1');
  assert.deepEqual(await first, { ok: true, port: 3000 });
  assert.equal(await second, true);
  assert.equal(app.commands.filter(c => c.method === 'connect').length, 1);
  assert.deepEqual(JSON.parse(app.policy.getForwardedPorts()).map(p => p.direction), ['forward', 'reverse']);
  assert.equal(await app.policy.testPortReachable(3000), true);
  assert.equal(await app.policy.testPortReachable(3001), false);
});

test('explicit reconnect is single flight and restores persisted mappings once', async () => {
  const connecting = deferred();
  const started = deferred();
  const app = fixture({
    mappings: [tunnelMapping(3000, 80, '', 'forward')],
    execute: async command => {
      if (command.method === 'connect') { started.resolve(); return await connecting.promise; }
      return { ok: true, port: command.port };
    }
  });
  const first = app.policy.reconnectTunnel();
  await started.promise;
  const second = app.policy.reconnectTunnel();
  assert.equal(first, second);
  connecting.resolve({ ok: true, connected: true });
  assert.deepEqual(await Promise.all([first, second]), [true, true]);
  assert.equal(app.commands.filter(c => c.method === 'connect').length, 1);
  assert.equal(app.commands.filter(c => c.method === 'add').length, 1);
});

test('removing a bind in flight closes its actual allocated port and never restores it', async () => {
  const pending = deferred();
  const started = deferred();
  const app = fixture({ execute: async command => {
    if (command.method === 'connect' || command.method === 'status') return { ok: true, connected: true };
    if (command.method === 'add') { started.resolve(); return await pending.promise; }
    return { ok: true };
  } });
  const add = app.policy.addForwardedPort(3000, 80, '');
  await started.promise;
  const remove = app.policy.removeForwardedPort(3000);
  pending.resolve({ ok: true, port: 3001 });
  assert.equal((await add).ok, false);
  await remove;
  assert.equal(app.policy.getForwardedPorts(), '[]');
  assert.ok(app.commands.some(c => c.method === 'remove' && c.port === 3001));
  await app.policy.tick(Date.now() + 60000);
  assert.equal(app.commands.filter(c => c.method === 'add').length, 1);
});

test('switching servers discards late connect completion and cancels it before the next connect', async () => {
  const pending = deferred();
  const started = deferred();
  const app = fixture({ execute: async command => {
    if (command.method === 'connect' && command.origin === 'https://one.example') {
      started.resolve(); return await pending.promise;
    }
    return { ok: true, connected: true, port: command.port };
  } });
  const first = app.policy.addForwardedPort(3000, 80, '');
  await started.promise;
  app.policy.setServer('https://two.example', { transport: 'h2', mappings: [] });
  const second = app.policy.addForwardedPort(4000, 80, '');
  pending.resolve({ ok: true, connected: true });
  assert.equal((await first).ok, false);
  assert.deepEqual(await second, { ok: true, port: 4000 });
  assert.ok(app.cancelled() >= 3);
  assert.deepEqual(app.commands.filter(c => c.method === 'add').map(c => c.port), [4000]);
  assert.deepEqual(JSON.parse(app.policy.getForwardedPorts()).map(p => p.port), [4000]);
  assert.equal(app.policy.getActiveTunnelTransport(), 'h2');
});

test('late credential loading cannot submit native work for a previous server', async () => {
  const pending = deferred();
  const started = deferred();
  const app = fixture({ prepare: async (origin, transport) => {
    if (origin === 'https://one.example') { started.resolve(); return await pending.promise; }
    return { method: 'connect', origin, transport };
  } });
  const first = app.policy.reconnectTunnel();
  await started.promise;
  app.policy.setServer('https://two.example', { transport: 'h2', mappings: [] });
  assert.equal(await first, false);
  assert.equal(await app.policy.reconnectTunnel(), true);
  pending.resolve({ method: 'connect', origin: 'https://one.example', transport: 'ssh', password: 'stale' });
  await Promise.resolve();
  assert.deepEqual(app.commands.filter(c => c.method === 'connect').map(c => c.origin), ['https://two.example']);
});

test('first SSH key requires confirmation, then the exact fingerprint is sent to the retry', async () => {
  const fingerprint = 'b'.repeat(64);
  const app = fixture({ execute: async command => command.method === 'connect' && command.hostKey !== fingerprint ?
    { ok: false, errorType: 'hostkey', error: 'approval required', fingerprint } :
    { ok: true, connected: true, port: command.port } });
  assert.equal(await app.policy.reconnectTunnel(), true);
  assert.equal(app.prompts.length, 1);
  assert.equal(app.prompts[0].command.hostKey, '');
  assert.equal(app.commands.filter(c => c.method === 'connect')[1].hostKey, fingerprint);
});

test('a changed SSH key can be rejected and does not trigger unattended retry prompts', async () => {
  const fingerprint = 'b'.repeat(64);
  const app = fixture({
    hostKey: 'a'.repeat(64),
    approve: async () => false,
    mappings: [tunnelMapping(3000, 80, '', 'forward')],
    execute: async () => ({ ok: false, errorType: 'hostkey', error: 'changed', fingerprint })
  });
  assert.equal(await app.policy.reconnectTunnel(), false);
  assert.equal(app.prompts[0].command.hostKey, 'a'.repeat(64));
  await app.policy.tick(Date.now() + 60000);
  assert.equal(app.prompts.length, 1);
  assert.equal(app.commands.length, 1);
  assert.equal(app.policy.getTunnelErrorType(), 'hostkey');
});

test('navigation cancels an unresolved fingerprint prompt without trusting its late answer', async () => {
  const pending = deferred();
  const shown = deferred();
  const app = fixture({
    approve: async () => { shown.resolve(); return await pending.promise; },
    execute: async () => ({ ok: false, errorType: 'hostkey', fingerprint: 'c'.repeat(64) })
  });
  const work = app.policy.reconnectTunnel();
  await shown.promise;
  app.policy.setServer('', { transport: 'ssh', mappings: [] });
  assert.equal(await work, false);
  pending.resolve(true);
  await Promise.resolve();
  assert.equal(app.commands.length, 1);
});

test('backgrounding preserves an established listener for the external browser', async () => {
  const app = fixture();
  await app.policy.addForwardedPort(3000, 80, '');
  const cancels = app.cancelled();
  app.policy.setForeground(false);
  assert.equal(app.cancelled(), cancels);
  assert.equal(await app.policy.isTunnelConnected(), true);
  assert.equal(await app.policy.testPortReachable(3000), true);
  const count = app.commands.length;
  await app.policy.tick(Date.now() + 60000);
  assert.equal(app.commands.length, count);
  assert.equal(await app.policy.reconnectTunnel(), false);
  assert.equal(app.cancelled(), cancels);
  assert.equal((await app.policy.addForwardedPort(4000, 80, '')).ok, false);
  app.policy.setForeground(true);
  await app.policy.tick(Date.now() + 60000);
  assert.equal(app.commands.filter(c => c.method === 'connect').length, 1);
  assert.equal(app.commands.filter(c => c.method === 'add').length, 1);
});

for (const transientError of ['network', 'capacity']) {
test(`${transientError} failures back off and success clears the prior error`, async () => {
  let attempts = 0;
  const app = fixture({
    mappings: [tunnelMapping(3000, 80, '', 'forward')],
    execute: async command => {
      if (command.method === 'connect' && ++attempts === 1) {
        return { ok: false, errorType: transientError, error: 'fixture disconnect' };
      }
      return { ok: true, connected: true, port: command.port };
    }
  });
  assert.equal(await app.policy.reconnectTunnel(), false);
  await app.policy.tick();
  assert.equal(attempts, 1);
  await app.policy.tick(Date.now() + 60000);
  assert.equal(attempts, 2);
  assert.equal(app.policy.getTunnelError(), '');
  assert.equal(app.policy.getTunnelErrorType(), '');
  for (const error of ['auth', 'hostkey', 'certificate', 'disabled', 'invalid']) {
    assert.equal(retryTunnelAutomatically(error), false);
  }
});
}

test('capacity recovery stays suspended in background and resumes once in foreground', async () => {
  let attempts = 0;
  const app = fixture({
    mappings: [tunnelMapping(3000, 80, '', 'forward')],
    execute: async command => {
      if (command.method === 'connect' && ++attempts === 1) {
        return { ok: false, errorType: 'capacity', error: 'Reverse tunnel incoming queue is full' };
      }
      return { ok: true, connected: true, port: command.port };
    }
  });
  assert.equal(await app.policy.reconnectTunnel(), false);
  assert.equal(app.policy.getTunnelErrorType(), 'capacity');
  app.policy.setForeground(false);
  await app.policy.tick(Date.now() + 60000);
  assert.equal(attempts, 1);
  app.policy.setForeground(true);
  await Promise.all([app.policy.tick(Date.now() + 60000), app.policy.tick(Date.now() + 60000)]);
  assert.equal(attempts, 2);
  assert.equal(app.commands.filter(command => command.method === 'add').length, 1);
});

test('preparation errors preserve authentication versus transient-network classification', async () => {
  const app = fixture({ prepare: async () => { throw new TunnelFailure('auth', 'Session expired'); } });
  assert.equal(await app.policy.reconnectTunnel(), false);
  assert.equal(app.policy.getTunnelErrorType(), 'auth');
  assert.equal(app.policy.getTunnelError(), 'Session expired');
  assert.equal(app.commands.length, 0);
});

test('an allocated fallback port is persisted and removal uses the allocated port', async () => {
  const app = fixture({ execute: async command => command.method === 'add' ?
    { ok: true, port: command.port + 1 } : { ok: true, connected: true } });
  assert.deepEqual(await app.policy.addForwardedPort(3000, 80, ''), { ok: true, port: 3001 });
  assert.equal(JSON.parse(app.policy.getForwardedPorts())[0].port, 3001);
  await app.policy.removeForwardedPort(3001);
  assert.equal(app.policy.getForwardedPorts(), '[]');
  assert.ok(app.commands.some(c => c.method === 'remove' && c.port === 3001));
  assert.equal(app.saves.at(-1).settings.mappings.length, 0);
});

test('transport preference affects the next reconnect while the live transport stays accurate', async () => {
  const app = fixture();
  await app.policy.addForwardedPort(3000, 80, '');
  await app.policy.setTunnelTransportH2Enabled(true);
  assert.equal(app.policy.getTunnelTransport(), 'h2');
  assert.equal(app.policy.getActiveTunnelTransport(), 'ssh');
  assert.equal(app.commands.filter(c => c.method === 'connect').length, 1);
  await app.policy.reconnectTunnel();
  assert.equal(app.policy.getActiveTunnelTransport(), 'h2');
  assert.equal(app.commands.filter(c => c.method === 'connect').at(-1).transport, 'h2');
});

test('forgetting ports cancels a pending bind and persists an empty desired set', async () => {
  const pending = deferred();
  const started = deferred();
  const app = fixture({ execute: async command => {
    if (command.method === 'add') { started.resolve(); return await pending.promise; }
    return { ok: true, connected: true };
  } });
  const add = app.policy.addForwardedPort(3000, 80, '');
  await started.promise;
  await app.policy.forgetForwardedPorts();
  pending.resolve({ ok: true, port: 3000 });
  assert.equal((await add).ok, false);
  assert.equal(app.policy.getForwardedPorts(), '[]');
  assert.equal(app.saves.at(-1).settings.mappings.length, 0);
  await app.policy.tick(Date.now() + 60000);
  assert.equal(app.commands.filter(c => c.method === 'connect').length, 1);
});

test('persisted configuration contains only mapping metadata, never credentials', async () => {
  const app = fixture();
  await app.policy.addForwardedPort(3000, 80, '');
  const serialized = JSON.stringify(app.saves);
  assert.equal(serialized.includes('fixture-only'), false);
  assert.equal(serialized.includes('fixture=token'), false);
  assert.equal(serialized.includes('"password"'), false);
  assert.equal(serialized.includes('"cookie"'), false);
});

test('removing a reassigned port during persistence closes it before reporting success', async () => {
  const pending = deferred();
  const saving = deferred();
  const app = fixture({
    execute: async command => command.method === 'add' ?
      { ok: true, port: 3001 } : { ok: true, connected: true },
    persist: async (_origin, settings) => {
      if (settings.mappings[0]?.port === 3001) { saving.resolve(); await pending.promise; }
    }
  });
  const add = app.policy.addForwardedPort(3000, 80, '');
  await saving.promise;
  const remove = app.policy.removeForwardedPort(3001);
  pending.resolve();
  assert.equal((await add).ok, false);
  await remove;
  assert.equal(await app.policy.testPortReachable(3001), false);
  assert.equal(app.policy.getForwardedPorts(), '[]');
  assert.ok(app.commands.some(c => c.method === 'remove' && c.port === 3001));
});

test('background restore never opens an SSH fingerprint dialog without a foreground action', async () => {
  const app = fixture({
    mappings: [tunnelMapping(3000, 80, '', 'forward')],
    execute: async () => ({ ok: false, errorType: 'hostkey', fingerprint: 'd'.repeat(64) })
  });
  await app.policy.tick(Date.now() + 60000);
  assert.equal(app.prompts.length, 0);
  assert.equal(app.policy.getTunnelErrorType(), 'hostkey');
});

test('idle stop does not reload a forgotten mapping from an unfinished persistence write', async () => {
  const pending = deferred();
  const saving = deferred();
  const app = fixture({
    mappings: [tunnelMapping(3000, 80, '', 'forward')],
    persist: async (_origin, settings) => {
      if (settings.mappings.length === 0) { saving.resolve(); await pending.promise; }
    }
  });
  const forgetting = app.policy.forgetForwardedPorts();
  await saving.promise;
  app.policy.stopWhenIdle();
  pending.resolve();
  await forgetting;
  await app.policy.tick(Date.now() + 60000);
  assert.equal(app.policy.getForwardedPorts(), '[]');
  assert.equal(app.commands.length, 0);
});

test('a reverse-only server port is never treated as a local reachable listener', async () => {
  const app = fixture();
  assert.equal(await app.policy.addReverseForwardedPort(3000, 80, ''), true);
  assert.equal(await app.policy.testPortReachable(3000), false);
  assert.equal(app.commands.some(c => c.method === 'probe'), false);
});

test('background disconnection is rebuilt once after returning to the foreground', async () => {
  let disconnected = false;
  const app = fixture({ execute: async command => {
    if (command.method === 'status' && disconnected) {
      return { ok: true, connected: false, errorType: 'network', error: 'fixture disconnected' };
    }
    if (command.method === 'connect') disconnected = false;
    return { ok: true, connected: true, port: command.port };
  } });
  await app.policy.addForwardedPort(3000, 80, '');
  app.policy.setForeground(false);
  disconnected = true;
  await app.policy.tick(Date.now() + 60000);
  assert.equal(app.commands.filter(c => c.method === 'connect').length, 1);
  app.policy.setForeground(true);
  await Promise.all([app.policy.tick(Date.now() + 60000), app.policy.tick(Date.now() + 60000)]);
  assert.equal(app.commands.filter(c => c.method === 'connect').length, 2);
  assert.equal(app.commands.filter(c => c.method === 'add').length, 2);
});

test('backgrounding cancels an uncompleted connection and refuses its late success', async () => {
  const pending = deferred();
  const started = deferred();
  const app = fixture({ execute: async command => {
    if (command.method === 'connect') { started.resolve(); return await pending.promise; }
    return { ok: true, port: command.port };
  } });
  const add = app.policy.addForwardedPort(3000, 80, '');
  await started.promise;
  app.policy.setForeground(false);
  pending.resolve({ ok: true, connected: true });
  assert.equal((await add).ok, false);
  assert.equal(await app.policy.isTunnelConnected(), false);
  assert.equal(app.commands.some(c => c.method === 'add'), false);
});

test('restored port drift emits one rebound event while initial binding uses only its result', async () => {
  const app = fixture({ execute: async command => command.method === 'add' ?
    { ok: true, port: command.port + 1 } : { ok: true, connected: true } });
  assert.deepEqual(await app.policy.addForwardedPort(3000, 80, ''), { ok: true, port: 3001 });
  assert.deepEqual(app.rebounds, []);
  assert.equal(await app.policy.reconnectTunnel(), true);
  assert.deepEqual(app.rebounds, [{ requested: 3001, actual: 3002 }]);
});

test('a fallback bind cannot overwrite another desired mapping with a different target', async () => {
  const app = fixture({
    mappings: [tunnelMapping(3001, 9000, '', 'forward')],
    execute: async command => command.method === 'add' ?
      { ok: true, port: 3001 } : { ok: true, connected: true }
  });
  assert.deepEqual(await app.policy.addForwardedPort(3000, 80, ''), { ok: false, reason: 'conflict' });
  assert.equal(JSON.parse(app.policy.getForwardedPorts()).find(p => p.port === 3001).targetPort, 9000);
  assert.ok(app.commands.some(c => c.method === 'remove' && c.port === 3001));
});

test('a status poll from the previous server cannot start work on the next server', async () => {
  const pending = deferred();
  const checking = deferred();
  const app = fixture({ execute: async command => {
    if (command.method === 'status') { checking.resolve(); return await pending.promise; }
    return { ok: true, connected: true, port: command.port };
  } });
  await app.policy.addForwardedPort(3000, 80, '');
  const poll = app.policy.tick(Date.now() + 60000);
  await checking.promise;
  app.policy.setServer('https://two.example', { transport: 'h2', mappings: [tunnelMapping(4000, 80, '', 'forward')] });
  pending.resolve({ ok: true, connected: false, errorType: 'network' });
  await poll;
  assert.equal(app.commands.filter(c => c.method === 'connect').length, 1);
  assert.equal(app.policy.getTunnelError(), '');
});

test('a previous server fallback persistence failure cannot remove the next server mapping', async () => {
  const pending = deferred();
  const saving = deferred();
  const app = fixture({
    execute: async command => command.method === 'add' ?
      { ok: true, port: 3001 } : { ok: true, connected: true },
    persist: async (origin, settings) => {
      if (origin === 'https://one.example' && settings.mappings[0]?.port === 3001) {
        saving.resolve();
        await pending.promise;
      }
    }
  });
  const add = app.policy.addForwardedPort(3000, 80, '');
  await saving.promise;
  app.policy.setServer('https://two.example', {
    transport: 'h2', mappings: [tunnelMapping(3001, 9000, '', 'forward')]
  });
  pending.reject(new Error('fixture write failed'));
  assert.equal((await add).ok, false);
  assert.equal(JSON.parse(app.policy.getForwardedPorts())[0].targetPort, 9000);
  assert.equal(app.commands.some(c => c.method === 'remove'), false);
});

test('browser preparation connects a cold saved forward and awaits a successful native probe', async () => {
  const probe = deferred();
  const probing = deferred();
  const app = fixture({
    mappings: [tunnelMapping(3000, 80, '', 'forward')],
    execute: async command => {
      if (command.method === 'probe') { probing.resolve(); return await probe.promise; }
      return { ok: true, connected: true, port: command.port };
    }
  });
  let ready = false;
  const preparing = app.policy.prepareForwardedPort(3000);
  void preparing.then(() => { ready = true; });
  await probing.promise;
  assert.equal(ready, false);
  probe.resolve({ ok: true, reachable: true });
  assert.equal(await preparing, 3000);
  assert.deepEqual(app.commands.map(c => c.method), ['connect', 'add', 'probe']);
});

test('browser preparation reuses a healthy tunnel without reconnecting or rebinding', async () => {
  const app = fixture();
  await app.policy.addForwardedPort(3000, 80, '');
  await app.policy.addForwardedPort(3001, 81, '');
  const before = app.commands.length;
  assert.equal(await app.policy.prepareForwardedPort(3000), 3000);
  assert.deepEqual(app.commands.slice(before).map(c => c.method), ['status', 'probe']);
});

test('browser preparation returns the restored actual port and shares concurrent requests', async () => {
  const connected = deferred();
  const starting = deferred();
  const app = fixture({
    mappings: [tunnelMapping(3000, 80, '', 'forward')],
    execute: async command => {
      if (command.method === 'connect') { starting.resolve(); return await connected.promise; }
      if (command.method === 'add') return { ok: true, port: 3001 };
      return { ok: true, reachable: true };
    }
  });
  const first = app.policy.prepareForwardedPort(3000);
  await starting.promise;
  const second = app.policy.prepareForwardedPort(3000);
  assert.equal(first, second);
  connected.resolve({ ok: true, connected: true });
  assert.deepEqual(await Promise.all([first, second]), [3001, 3001]);
  assert.deepEqual(app.rebounds, [{ requested: 3000, actual: 3001 }]);
  assert.equal(app.commands.filter(c => c.method === 'probe')[0].port, 3001);
});

test('browser preparation rejects unknown and background ports without native work', async () => {
  const app = fixture({ mappings: [tunnelMapping(3000, 80, '', 'forward')] });
  await assert.rejects(app.policy.prepareForwardedPort(4000), /not configured/);
  app.policy.setForeground(false);
  await assert.rejects(app.policy.prepareForwardedPort(3000), /Return to the app/);
  assert.equal(app.commands.length, 0);
});

test('browser preparation never opens after its server changes during a successful probe', async () => {
  const probe = deferred();
  const probing = deferred();
  const app = fixture({
    mappings: [tunnelMapping(3000, 80, '', 'forward')],
    execute: async command => {
      if (command.method === 'probe') { probing.resolve(); return await probe.promise; }
      return { ok: true, connected: true, port: command.port };
    }
  });
  const preparation = app.policy.prepareForwardedPort(3000);
  const rejected = assert.rejects(preparation, /cancelled/);
  await probing.promise;
  app.policy.setServer('https://two.example', { transport: 'h2', mappings: [] });
  probe.resolve({ ok: true, reachable: true });
  await rejected;
});

test('browser preparation refuses a late probe when the app backgrounds, preserving established tunnels', async () => {
  const probe = deferred();
  const probing = deferred();
  const app = fixture({
    mappings: [tunnelMapping(3000, 80, '', 'forward')],
    execute: async command => {
      if (command.method === 'probe') { probing.resolve(); return await probe.promise; }
      return { ok: true, connected: true, port: command.port };
    }
  });
  const preparation = app.policy.prepareForwardedPort(3000);
  const rejected = assert.rejects(preparation, /cancelled/);
  await probing.promise;
  const cancels = app.cancelled();
  app.policy.setForeground(false);
  probe.resolve({ ok: true, reachable: true });
  await rejected;
  assert.equal(app.cancelled(), cancels);
});

test('browser preparation reports a failed probe instead of returning a configured but unready port', async () => {
  const app = fixture({
    mappings: [tunnelMapping(3000, 80, '', 'forward')],
    execute: async command => command.method === 'probe' ?
      { ok: true, reachable: false } : { ok: true, connected: true, port: command.port }
  });
  await assert.rejects(app.policy.prepareForwardedPort(3000), /not ready/);
});
