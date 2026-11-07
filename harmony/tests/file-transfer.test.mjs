import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  MAX_BLOB_BYTES, projectDownloadUrl, resolveDownloadAddress, safeFileName,
  shareMime, TransferProgressState, validateBase64
} from '../entry/src/main/ets/models/FileTransferPolicy.ts';

test('project downloads preserve nested, Chinese and absolute file names', () => {
  const origin = 'https://example.com';
  assert.equal(projectDownloadUrl(origin, 'src/中文 100%.txt'),
    origin + '/api/fs/raw/src/%E4%B8%AD%E6%96%87%20100%25.txt?download=1');
  assert.equal(projectDownloadUrl(origin, '/home/user/a.txt'),
    origin + '/api/fs/raw/?download=1&target=%2Fhome%2Fuser%2Fa.txt');
  assert.equal(projectDownloadUrl(origin, 'C:\\project\\a.txt'),
    origin + '/api/fs/raw/?download=1&target=C%3A%2Fproject%2Fa.txt');
  assert.equal(projectDownloadUrl(origin, '..notes.txt'), origin + '/api/fs/raw/..notes.txt?download=1');
});

test('project downloads reject traversal, nested encoding, control bytes and URL schemes', () => {
  for (const path of ['', '../secret', 'a/../../secret', 'a\\..\\secret', '%2e%2e/secret',
    'a/%252e%252e/secret', 'a/%25252e%25252e/secret', 'a%2f..%2fsecret', 'a\0.txt',
    'file:///private', 'https://evil.example/private']) {
    assert.throws(() => projectDownloadUrl('https://example.com', path), undefined, path);
  }
});

test('download URLs attach cookies only to the exact canonical server origin', () => {
  const origin = 'https://example.com';
  assert.deepEqual(resolveDownloadAddress(origin, '/api/fs/archive?id=2'),
    { url: origin + '/api/fs/archive?id=2', authenticated: true });
  assert.equal(resolveDownloadAddress(origin, 'https://EXAMPLE.com:443/a').authenticated, true);
  for (const remote of ['http://example.com/a', 'https://example.com:8443/a', 'https://example.com.evil/a']) {
    assert.equal(resolveDownloadAddress(origin, remote).authenticated, false);
  }
  for (const invalid of ['//evil.example/a', 'https://user:pass@example.com/a', 'file:///a',
    'https://example.com\\@evil.example/a', 'https://example.com:0/a', '/a\nCookie: secret']) {
    assert.throws(() => resolveDownloadAddress(origin, invalid), undefined, invalid);
  }
});

test('file names cannot escape a destination, keep Chinese names and fit UTF-8 limits', () => {
  assert.equal(safeFileName('../../a.txt'), 'a.txt');
  assert.equal(safeFileName('目录/报告.txt'), '报告.txt');
  assert.equal(safeFileName('..'), 'download');
  assert.equal(safeFileName(' a:name?.txt '), 'a_name_.txt');
  assert.ok(Buffer.byteLength(safeFileName('报'.repeat(200) + '.txt'), 'utf8') <= 180);
  assert.ok(Buffer.byteLength(safeFileName('😀'.repeat(200) + '.txt'), 'utf8') <= 180);
});

test('blob validation bounds decoded size before allocating and rejects data URIs', () => {
  assert.equal(validateBase64('aGVsbG8='), 5);
  assert.equal(validateBase64(''), 0);
  for (const invalid of ['data:text/plain;base64,aA==', 'aA', 'a===', 'aA==\n', '****']) {
    assert.throws(() => validateBase64(invalid));
  }
  const maximum = Buffer.alloc(MAX_BLOB_BYTES).toString('base64');
  assert.equal(validateBase64(maximum), MAX_BLOB_BYTES);
  assert.throws(() => validateBase64(Buffer.alloc(MAX_BLOB_BYTES + 1).toString('base64')));
});

test('stream progress has a size bound even without Content-Length', () => {
  const state = new TransferProgressState(7, 10);
  state.receive(4);
  state.receive(6);
  assert.equal(state.snapshot().received, 10);
  assert.throws(() => state.receive(1));
  assert.equal(state.snapshot().received, 10);
  assert.equal(state.finish(), true);
  assert.equal(state.snapshot().total, 10);
  assert.equal(state.snapshot().done, true);
});

test('cancelled transfers ignore late bytes and cannot produce a second final event', () => {
  const state = new TransferProgressState(42);
  state.setTotal(100);
  state.receive(12);
  assert.equal(state.finish('', true), true);
  state.receive(88);
  state.setTotal(999);
  assert.equal(state.finish(), false);
  assert.deepEqual(state.snapshot(), {
    id: 42, received: 12, total: 100, done: true, error: '', cancelled: true
  });
});

test('invalid totals and MIME values do not become trusted transfer metadata', () => {
  const state = new TransferProgressState(1, 128);
  state.setTotal(Number.NaN);
  state.setTotal(-1);
  assert.equal(state.total, 0);
  assert.throws(() => state.setTotal(129));
  assert.throws(() => new TransferProgressState(1.1));
  assert.equal(shareMime('IMAGE/PNG'), 'image/png');
  assert.equal(shareMime('text/plain\r\nInjected: value'), 'application/octet-stream');
});
