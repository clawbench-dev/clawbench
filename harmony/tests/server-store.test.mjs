import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { runInNewContext } from 'node:vm';
import { parseServerAddress } from '../entry/src/main/ets/models/ServerAddress.ts';
import { NativeFailure, nativeMessage } from '../entry/src/main/ets/models/NativeMessages.ts';

const source = readFileSync(new URL(
  '../entry/src/main/ets/services/ServerStore.ets', import.meta.url
), 'utf8').replace(/^import .*;\r?\n/gm, '')
  .replace('export interface ', 'interface ').replace('export class ', 'class ');
const production = stripTypeScriptTypes(source, { mode: 'strip' }) + '\nServerStore;';
const first = { url: 'https://first.example', name: 'First server' };
const second = { url: 'https://second.example', name: 'Second server' };
const aliasFor = url => 'clawbench:password:' + parseServerAddress(url).url;

async function fixture({ servers = [first, second], lastUrl = first.url, language = 'zh',
  saved = new Map([[first.url, 'first-fixture'], [second.url, 'second-fixture']]),
  failQuery = new Map(), failAdd, failRemove, unavailable = false, initialize = true } = {}) {
  const persisted = new Map([
    ['servers', JSON.stringify(servers)], ['lastUrl', lastUrl], ['language', language], ['theme', 'fixture-theme']
  ]);
  const secrets = new Map([...saved].map(([url, password]) => [aliasFor(url), password]));
  const calls = [];
  let flushes = 0;
  const Tag = { ALIAS: 'alias', RETURN_TYPE: 'returnType', SECRET: 'secret',
    ACCESSIBILITY: 'accessibility', CONFLICT_RESOLUTION: 'conflictResolution' };
  const asset = {
    Tag, ReturnType: { ALL: 'all' }, Accessibility: { DEVICE_FIRST_UNLOCKED: 'firstUnlocked' },
    ConflictResolution: { OVERWRITE: 'overwrite' },
    async query(query) {
      const alias = Buffer.from(query.get(Tag.ALIAS)).toString();
      calls.push({ operation: 'query', alias, returnType: query.get(Tag.RETURN_TYPE) });
      if (unavailable) throw { code: 24000003 };
      const fault = failQuery.get(alias);
      if (fault) throw fault;
      if (!secrets.has(alias)) return [];
      return [new Map([[Tag.SECRET, new Uint8Array(Buffer.from(secrets.get(alias))) ]])];
    },
    async add(attributes) {
      const alias = Buffer.from(attributes.get(Tag.ALIAS)).toString();
      calls.push({ operation: 'add', alias, accessibility: attributes.get(Tag.ACCESSIBILITY),
        conflictResolution: attributes.get(Tag.CONFLICT_RESOLUTION) });
      if (unavailable || failAdd) throw failAdd || { code: 24000003 };
      secrets.set(alias, Buffer.from(attributes.get(Tag.SECRET)).toString());
    },
    async remove(query) {
      const alias = Buffer.from(query.get(Tag.ALIAS)).toString();
      calls.push({ operation: 'remove', alias });
      if (unavailable || failRemove) throw failRemove || { code: 24000003 };
      secrets.delete(alias);
    }
  };
  const preferences = {
    async getPreferences() {
      return {
        async get(key, fallback) { return persisted.has(key) ? persisted.get(key) : fallback; },
        async put(key, value) { persisted.set(key, value); },
        async flush() { flushes++; }
      };
    }
  };
  const util = {
    TextEncoder: class { encodeInto(value) { return new Uint8Array(Buffer.from(value)); } },
    TextDecoder: class { decodeWithStream(value) { return Buffer.from(value).toString(); } }
  };
  const ServerStore = runInNewContext(production, { preferences, asset, util, parseServerAddress, NativeFailure });
  const store = new ServerStore();
  if (initialize) await store.init({});
  return { store, ServerStore, persisted, secrets, calls, flushes: () => flushes };
}

function expectFailure(code, language) {
  return error => {
    assert.ok(error instanceof NativeFailure);
    assert.equal(error.code, code);
    assert.equal(error.message, nativeMessage(code, language));
    return true;
  };
}

test('store initialization preserves names and appearance without querying secure credentials', async () => {
  const h = await fixture({ unavailable: true, language: 'en' });
  assert.deepEqual(JSON.parse(JSON.stringify(h.store.servers)), [first, second]);
  assert.equal(h.store.lastUrl, first.url);
  assert.equal(h.store.language, 'en');
  assert.equal(h.store.theme, 'fixture-theme');
  assert.equal(h.calls.length, 0);
});

for (const language of ['zh', 'en']) {
  test(`one unreadable credential is classified without corrupting other server records (${language})`, async () => {
    const h = await fixture({
      language, failQuery: new Map([[aliasFor(first.url), { code: 24000003, message: 'Platform detail' }]])
    });
    await assert.rejects(h.store.password(first.url), expectFailure('credential_read_failed', language));
    assert.equal(await h.store.password(second.url), 'second-fixture');
    assert.equal(await h.store.password('https://missing.example'), '');
    assert.deepEqual(JSON.parse(JSON.stringify(h.store.servers)), [first, second]);
    assert.equal(h.flushes(), 0);
  });

  test(`missing secure credentials are empty but other native failures remain actionable (${language})`, async () => {
    const missing = await fixture({ language,
      failQuery: new Map([[aliasFor(first.url), { code: 24000002 }]]) });
    assert.equal(await missing.store.password(first.url), '');
    for (const failure of [{ code: 24000003 }, new Error('Unavailable secure store')]) {
      const h = await fixture({ language, failQuery: new Map([[aliasFor(first.url), failure]]) });
      await assert.rejects(h.store.password(first.url), expectFailure('credential_read_failed', language));
    }
  });

  test(`credential write and delete failures preserve server metadata (${language})`, async () => {
    const add = await fixture({ language, failAdd: { code: 24000003 } });
    await assert.rejects(add.store.save(first.url, 'rotated-fixture', 'Changed'),
      expectFailure('credential_save_failed', language));
    assert.deepEqual(JSON.parse(JSON.stringify(add.store.servers)), [first, second]);
    assert.equal(add.secrets.get(aliasFor(first.url)), 'first-fixture');
    assert.equal(add.flushes(), 0);

    const remove = await fixture({ language, failRemove: { code: 24000003 } });
    await assert.rejects(remove.store.remove(first.url), expectFailure('credential_remove_failed', language));
    await assert.rejects(remove.store.save(first.url, '', 'Changed'),
      expectFailure('credential_remove_failed', language));
    assert.deepEqual(JSON.parse(JSON.stringify(remove.store.servers)), [first, second]);
    assert.equal(remove.store.lastUrl, first.url);
    assert.equal(remove.flushes(), 0);
  });
}

test('metadata-only saves preserve credentials and other server names when AssetStore is unavailable', async () => {
  const h = await fixture({ unavailable: true });
  await h.store.saveMetadata('https://SECOND.example:443/', '  Second server  ');
  assert.equal(h.calls.length, 0);
  assert.deepEqual(JSON.parse(h.persisted.get('servers')), [second, first]);
  assert.equal(h.secrets.get(aliasFor(second.url)), 'second-fixture');
  assert.equal(h.persisted.get('lastUrl'), first.url);
  const restored = new h.ServerStore();
  await restored.init({});
  assert.deepEqual(JSON.parse(JSON.stringify(restored.servers)), [second, first]);
});

test('ignoring credentials removes only selected metadata and never touches the secure store', async () => {
  const h = await fixture({ unavailable: true });
  await h.store.remove(first.url, true);
  assert.equal(h.calls.length, 0);
  assert.deepEqual(JSON.parse(h.persisted.get('servers')), [second]);
  assert.equal(h.persisted.get('lastUrl'), '');
  assert.equal(h.secrets.get(aliasFor(first.url)), 'first-fixture');
  assert.equal(h.secrets.get(aliasFor(second.url)), 'second-fixture');
});

test('a normal save uses one canonical alias, preserves names and never serializes passwords into preferences', async () => {
  const h = await fixture();
  await h.store.save('https://SECOND.example:443/', 'rotated-fixture', '  Second server  ');
  assert.equal(h.calls.length, 1);
  assert.deepEqual(h.calls[0], {
    operation: 'add', alias: aliasFor(second.url), accessibility: 'firstUnlocked', conflictResolution: 'overwrite'
  });
  assert.equal(h.secrets.get(aliasFor(second.url)), 'rotated-fixture');
  assert.equal(h.secrets.get(aliasFor(first.url)), 'first-fixture');
  assert.deepEqual(JSON.parse(h.persisted.get('servers')), [second, first]);
  assert.doesNotMatch(JSON.stringify([...h.persisted]), /rotated-fixture|first-fixture|second-fixture/);
  assert.equal(h.flushes(), 1);
});

test('a missing credential does not prevent normal metadata removal or an explicit empty-password save', async () => {
  const h = await fixture({ saved: new Map(), failRemove: { code: 24000002 } });
  await h.store.remove(first.url);
  assert.deepEqual(JSON.parse(h.persisted.get('servers')), [second]);
  assert.equal(h.persisted.get('lastUrl'), '');
  await h.store.save(second.url, '', second.name);
  assert.deepEqual(JSON.parse(h.persisted.get('servers')), [second]);
  assert.equal(h.calls.filter(call => call.operation === 'add').length, 0);
});

test('metadata writes report a classified error when preferences have not been initialized', async () => {
  const h = await fixture({ initialize: false, unavailable: true });
  await assert.rejects(h.store.saveMetadata(first.url, first.name), expectFailure('store_not_ready', 'zh'));
  assert.equal(h.calls.length, 0);
  assert.equal(h.flushes(), 0);
});
