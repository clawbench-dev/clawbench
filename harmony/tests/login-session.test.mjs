import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { runInNewContext } from 'node:vm';
import { parseServerAddress } from '../entry/src/main/ets/models/ServerAddress.ts';
import { NativeFailure } from '../entry/src/main/ets/models/NativeMessages.ts';

const source = readFileSync(new URL(
  '../entry/src/main/ets/services/LoginSession.ets', import.meta.url
), 'utf8').replace(/^import .*;\r?$/gm, '').replace(/\bexport /g, '');
const code = stripTypeScriptTypes(source, { mode: 'strip' }) + '\nLoginSession;';
const first = { url: 'https://first.example', name: 'First' };
const second = { url: 'https://second.example', name: 'Second' };
const plain = value => ({ kind: 'plain', value });
const saved = server => ({ kind: 'saved', ref: server.password, sourceUrl: server.url });

function host({ servers = [first, second], lastUrl = first.url } = {}) {
  let nextId = 0;
  const LoginSession = runInNewContext(code, {
    parseServerAddress, NativeFailure, APP_VERSION: 'fixture',
    util: { generateRandomUUID: () => 'fixture-' + ++nextId }
  });
  const reads = [];
  const writes = [];
  const metadata = [];
  const failures = new Set();
  const passwords = new Map(servers.map(server => [server.url, 'fixture-password-' + server.name]));
  const store = {
    servers: structuredClone(servers), lastUrl, language: 'en', theme: 'gruvbox-dark',
    async password(url) {
      reads.push(url);
      if (failures.has(url)) throw new Error('Fixture asset failure');
      return passwords.get(url) || '';
    },
    async save(url, password, name) { writes.push({ url, password, name }); },
    async saveMetadata(url, name) { metadata.push({ url, name }); }
  };
  return { login: new LoginSession(store), store, reads, writes, metadata, failures, passwords };
}

test('one unreadable credential keeps every server available and revokes only its reference', async () => {
  const h = host();
  const initial = await h.login.snapshot(second.url);
  h.failures.add(first.url);
  const degraded = await h.login.snapshot(second.url);
  assert.equal(degraded.activeUrl, second.url);
  assert.equal(degraded.servers.length, 2);
  assert.equal(degraded.servers[0].password, '');
  assert.equal(degraded.servers[0].credentialState, 'unavailable');
  assert.equal(degraded.servers[1].password, initial.servers[1].password);
  assert.equal(degraded.servers[1].credentialState, 'saved');
  const reads = h.reads.length;
  await assert.rejects(h.login.password(saved(initial.servers[0]), first.url),
    { code: 'credential_ref_invalid' });
  assert.equal(h.reads.length, reads);
  assert.equal(await h.login.password(saved(degraded.servers[1]), second.url), h.passwords.get(second.url));
  assert.deepEqual(h.writes, []);
});

test('all unavailable credentials still return server metadata and an explicit selected target', async () => {
  const h = host();
  h.failures.add(first.url).add(second.url);
  const snapshot = await h.login.snapshot('https://SECOND.example:443');
  assert.equal(snapshot.activeUrl, second.url);
  assert.deepEqual(Array.from(snapshot.servers, server => server.credentialState), ['unavailable', 'unavailable']);
  assert.deepEqual(Array.from(snapshot.servers, server => server.password), ['', '']);
});

test('selection falls back to the last saved server and then the first surviving server', async () => {
  const h = host({ lastUrl: second.url });
  assert.equal((await h.login.snapshot()).activeUrl, second.url);
  h.store.lastUrl = 'https://removed.example';
  assert.equal((await h.login.snapshot('not a server')).activeUrl, first.url);
  h.store.servers = [];
  assert.equal((await h.login.snapshot()).activeUrl, '');
});

test('an explicit unsaved target stays selectable without reading or persisting a credential for it', async () => {
  const h = host();
  const url = 'https://new.example';
  h.passwords.set(url, 'fixture-unlisted-password');
  const snapshot = await h.login.snapshot('https://NEW.example:443');
  const target = snapshot.servers.find(server => server.url === url);
  assert.equal(snapshot.activeUrl, url);
  assert.deepEqual(JSON.parse(JSON.stringify(target)), {
    url, name: '', password: '', credentialState: 'missing'
  });
  assert.deepEqual(h.reads, [first.url, second.url]);
  assert.deepEqual(h.store.servers, [first, second]);
  assert.deepEqual(h.writes, []);
  assert.deepEqual(h.metadata, []);
  assert.equal(h.store.lastUrl, first.url);
});

test('an unsaved target survives an empty list and ignore mode without creating a saved reference', async () => {
  const h = host({ servers: [], lastUrl: '' });
  for (const ignored of [false, true]) {
    const snapshot = await h.login.snapshot(second.url, ignored);
    assert.equal(snapshot.activeUrl, second.url);
    assert.equal(snapshot.servers.length, 1);
    assert.equal(snapshot.servers[0].password, '');
    assert.equal(snapshot.servers[0].credentialState, ignored ? 'ignored' : 'missing');
    assert.deepEqual(h.reads, []);
    assert.deepEqual(h.store.servers, []);
  }
  assert.equal((await h.login.snapshot()).activeUrl, '');
});

test('ignoring credentials performs no secure reads or writes and invalidates prior references', async () => {
  const h = host();
  const initial = await h.login.snapshot();
  h.reads.length = 0;
  const ignored = await h.login.snapshot(second.url, true);
  assert.equal(ignored.credentialsIgnored, true);
  assert.equal(ignored.activeUrl, second.url);
  assert.deepEqual(Array.from(ignored.servers, server => server.credentialState), ['ignored', 'ignored']);
  await h.login.save(first.url, saved(initial.servers[0]), 'Renamed', true);
  await h.login.save('https://new.example', plain('fixture-secret'), 'New', true);
  await assert.rejects(h.login.password(saved(initial.servers[0]), first.url),
    { code: 'credential_ref_invalid' });
  assert.deepEqual(h.reads, []);
  assert.deepEqual(h.writes, []);
  assert.deepEqual(h.metadata, [
    { url: first.url, name: 'Renamed' },
    { url: 'https://new.example', name: 'New' }
  ]);
});

test('typed literal passwords preserve the reserved prefix without querying secure storage', async () => {
  const h = host();
  const literal = 'clawbench-saved:literal-user-password';
  assert.equal(await h.login.password(plain(literal), first.url), literal);
  await h.login.save(first.url, plain(literal), 'First');
  assert.deepEqual(h.reads, []);
  assert.deepEqual(h.writes, [{ url: first.url, password: literal, name: 'First' }]);
});

test('saved references are target-bound while explicit edit saves can copy from the source server', async () => {
  const h = host();
  const snapshot = await h.login.snapshot();
  const credential = saved(snapshot.servers[0]);
  const reads = h.reads.length;
  await assert.rejects(h.login.password(credential, second.url), { code: 'credential_ref_invalid' });
  await assert.rejects(h.login.password({ ...credential, sourceUrl: second.url }, first.url),
    { code: 'credential_ref_invalid' });
  assert.equal(h.reads.length, reads);
  await h.login.save('https://renamed.example', credential, 'Renamed');
  assert.deepEqual(h.writes, [
    { url: 'https://renamed.example', password: h.passwords.get(first.url), name: 'Renamed' }
  ]);
});

test('unknown, cleared and malformed references never become literal passwords', async () => {
  const h = host();
  const snapshot = await h.login.snapshot();
  h.login.clear();
  const reads = h.reads.length;
  for (const credential of [
    saved(snapshot.servers[0]),
    { kind: 'saved', ref: 'clawbench-saved:unknown', sourceUrl: first.url },
    { kind: 'other', value: 'fixture' }, { kind: 'plain' }, null, 'fixture-string'
  ]) {
    await assert.rejects(h.login.password(credential, first.url), { code: 'credential_ref_invalid' });
  }
  assert.equal(h.reads.length, reads);
});

test('a credential becoming unreadable between display and connect is revoked and classified', async () => {
  const h = host();
  const snapshot = await h.login.snapshot();
  h.failures.add(first.url);
  await assert.rejects(h.login.password(saved(snapshot.servers[0]), first.url),
    { code: 'credential_read_failed' });
  const reads = h.reads.length;
  await assert.rejects(h.login.password(saved(snapshot.servers[0]), first.url),
    { code: 'credential_ref_invalid' });
  assert.equal(h.reads.length, reads);
});

test('a missing credential drops its reference on the next snapshot', async () => {
  const h = host();
  const initial = await h.login.snapshot();
  h.passwords.delete(first.url);
  const current = await h.login.snapshot();
  assert.equal(current.servers[0].credentialState, 'missing');
  await assert.rejects(h.login.password(saved(initial.servers[0]), first.url),
    { code: 'credential_ref_invalid' });
});

test('a late snapshot cannot restore references after the login document is closed', async () => {
  const h = host({ servers: [first] });
  let finish;
  h.store.password = () => new Promise(resolve => { finish = resolve; });
  const pending = h.login.snapshot();
  h.login.clear();
  finish('fixture-password');
  const snapshot = await pending;
  await assert.rejects(h.login.password(saved(snapshot.servers[0]), first.url),
    { code: 'credential_ref_invalid' });
});

test('a late credential read cannot survive a cleared login document', async () => {
  const h = host({ servers: [first] });
  const snapshot = await h.login.snapshot();
  let finish;
  h.store.password = () => new Promise(resolve => { finish = resolve; });
  const pending = h.login.password(saved(snapshot.servers[0]), first.url);
  h.login.clear();
  finish('fixture-password');
  await assert.rejects(pending, { code: 'credential_ref_invalid' });
});
