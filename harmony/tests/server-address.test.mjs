import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseServerAddress, isTrustedAppUrl, compatibleProtocol } from '../entry/src/main/ets/models/ServerAddress.ts';

test('normalizes identity and default ports for per-server credentials', () => {
  assert.equal(parseServerAddress(' EXAMPLE.COM:443/ ').url, 'https://example.com');
  assert.equal(parseServerAddress('http://example.com:80').url, 'http://example.com');
  assert.equal(parseServerAddress('http://[::1]:20000/').url, 'http://[::1]:20000');
  assert.notEqual(parseServerAddress('https://example.com:20000').url, parseServerAddress('https://example.com:20001').url);
});

test('rejects credentials, ambiguous paths and invalid ports', () => {
  for (const address of ['', 'https://', 'file:///test', 'https://user:pass@example.com',
    'https://example.com/project', 'https://example.com?password=secret',
    'https://example.com#test', 'https://example.com:0', 'https://example.com:65536',
    'https://example.com\\@evil.example', 'https://example.com:NaN']) {
    assert.throws(() => parseServerAddress(address), address);
  }
});

test('only the exact server application documents are trusted', () => {
  const origin = 'https://example.com:20000';
  assert.equal(isTrustedAppUrl(origin + '/', origin), true);
  assert.equal(isTrustedAppUrl(origin + '/login?from=app', origin), true);
  for (const candidate of ['https://example.com:200001/', origin + '.evil/', origin + '@evil/',
    origin + '/api/fs/raw/example.html', origin + '/share/abc', origin + '/docs/', 'about:blank']) {
    assert.equal(isTrustedAppUrl(candidate, origin), false, candidate);
  }
});

test('health identity and protocol are both required', () => {
  assert.equal(compatibleProtocol('clawbench', 1), true);
  assert.equal(compatibleProtocol('other', 1), false);
  assert.equal(compatibleProtocol('clawbench', 0), false);
  assert.equal(compatibleProtocol('clawbench', 2), false);
});
