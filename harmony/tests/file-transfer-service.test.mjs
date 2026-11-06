import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { runInNewContext } from 'node:vm';
import * as policy from '../entry/src/main/ets/models/FileTransferPolicy.ts';
import { parseServerAddress } from '../entry/src/main/ets/models/ServerAddress.ts';
import { NativeFailure, nativeMessage } from '../entry/src/main/ets/models/NativeMessages.ts';

const source = readFileSync(new URL(
  '../entry/src/main/ets/services/FileTransferService.ets', import.meta.url
), 'utf8').replace(/^import[\s\S]*?;\r?\n/gm, '').replace('export class FileTransferService', 'class FileTransferService');
const executable = stripTypeScriptTypes(source, { mode: 'strip' }) + '\nglobalThis.Service = FileTransferService;';

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function createHost({ select, status = 200, payload = 'fixture file', waitForData, cachedBytes = 0,
  failCreateRequest = false, language = 'zh' } = {}) {
  const files = new Map([['file://selected/report.txt', Buffer.from('existing file')]]);
  const descriptors = new Map();
  const requests = [];
  const events = [];
  const shares = [];
  const completed = deferred();
  let nextFd = 0;
  let nextUuid = 0;
  let legacyFailures = 0;
  const fs = {
    OpenMode: { READ_ONLY: 0, WRITE_ONLY: 1, CREATE: 64, TRUNC: 512 },
    accessSync: () => true,
    mkdirSync() {},
    listFileSync: () => cachedBytes ? ['123_cached.txt'] : [],
    statSync: () => ({ isFile: () => true, size: cachedBytes, mtime: Date.now() / 1000 }),
    unlinkSync: path => files.delete(path),
    async open(path, mode) {
      if (mode & 512 || !files.has(path)) files.set(path, Buffer.alloc(0));
      const fd = ++nextFd;
      descriptors.set(fd, { path, position: 0 });
      return { fd };
    },
    async write(fd, data) {
      const descriptor = descriptors.get(fd);
      const incoming = Buffer.from(data);
      const count = Math.min(3, incoming.length);
      const previous = files.get(descriptor.path);
      const contents = Buffer.alloc(Math.max(previous.length, descriptor.position + count));
      previous.copy(contents);
      incoming.copy(contents, descriptor.position, 0, count);
      files.set(descriptor.path, contents);
      descriptor.position += count;
      return count;
    },
    async read(fd, buffer) {
      const descriptor = descriptors.get(fd);
      const contents = files.get(descriptor.path);
      const count = Math.min(buffer.byteLength, contents.length - descriptor.position);
      new Uint8Array(buffer).set(contents.subarray(descriptor.position, descriptor.position + count));
      descriptor.position += count;
      return count;
    },
    async fsync() {},
    async close(file) { descriptors.delete(file.fd); }
  };
  const http = {
    RequestMethod: { GET: 'GET' },
    createHttp() {
      if (failCreateRequest) throw new Error('Network unavailable');
      const callbacks = {};
      const request = {
        destroyed: false,
        on(name, callback) { callbacks[name] = callback; },
        destroy() { request.destroyed = true; },
        async requestInStream(url, options) {
          requests.push({ url, options, request });
          const send = async () => {
            if (waitForData) await waitForData;
            if (request.destroyed) return;
            const contents = Buffer.from(payload);
            callbacks.headersReceive?.({ 'content-length': String(contents.length) });
            callbacks.dataReceive?.(contents.buffer.slice(contents.byteOffset, contents.byteOffset + contents.byteLength));
            callbacks.dataEnd?.();
          };
          void send();
          return status;
        }
      };
      return request;
    }
  };
  const systemShare = {
    SharePreviewMode: { DEFAULT: 0 }, SelectionMode: { SINGLE: 0, BATCH: 1 },
    SharedData: class {
      constructor(record) { this.records = [record]; }
      addRecord(record) { this.records.push(record); }
    },
    ShareController: class {
      constructor(data) { this.data = data; }
      async show(context, options) { shares.push({ records: this.data.records, options }); }
    }
  };
  const context = {
    ...policy, parseServerAddress, NativeFailure, nativeMessage, fs, http, systemShare,
    // The cookie-scope policy is independently covered by native event tests.
    nativeCookieHeader: (_origin, header) => header,
    webview: { WebCookieManager: { fetchCookieSync: () => 'clawbench_session=fixture; clawbench_project=fixture-project' } },
    fileUri: { getUriFromPath: path => 'file://' + path },
    uniformTypeDescriptor: {
      UniformDataType: { FILE: 'general.file', PLAIN_TEXT: 'general.plain-text' },
      getUniformDataTypeByMIMEType: mime => mime
    },
    util: {
      generateRandomUUID: () => String(++nextUuid),
      Base64Helper: class { decodeSync(value) { return new Uint8Array(Buffer.from(value, 'base64')); } }
    },
    picker: {
      DocumentSaveOptions: class {},
      DocumentViewPicker: class {
        async save(options) { return select ? select(options) : ['file://selected/report.txt']; }
      }
    }
  };
  runInNewContext(executable, context);
  const service = new context.Service({ cacheDir: '/cache' }, 'https://example.com', event => {
    events.push(JSON.parse(JSON.stringify(event)));
    if (event.done) completed.resolve(event);
  }, () => legacyFailures++, language);
  return { service, files, requests, events, shares, completed, descriptors,
    legacyFailures: () => legacyFailures };
}

test('native download streams partial writes, keeps project cookies and reports completion only after save', async () => {
  const host = createHost({ payload: '中文 fixture document' });
  host.service.startDownload('project/report.txt', '报告.txt', 9);
  const end = await host.completed.promise;
  assert.equal(end.error, '');
  assert.equal(end.cancelled, false);
  assert.equal(host.files.get('file://selected/report.txt').toString(), '中文 fixture document');
  assert.equal(host.requests[0].options.maxRedirects, 0);
  assert.equal(host.requests[0].options.header.Cookie, 'clawbench_session=fixture; clawbench_project=fixture-project');
  assert.equal(host.events.filter(event => event.done).length, 1);
  await new Promise(resolve => setImmediate(resolve));
  assert.equal([...host.files.keys()].filter(path => path.startsWith('/cache/')).length, 0);
  assert.equal(host.descriptors.size, 0);
});

test('cancel while the system picker is open never starts the network or overwrites the selected file', async () => {
  const selection = deferred();
  const host = createHost({ select: () => selection.promise });
  host.service.startDownload('report.txt', '', 10);
  host.service.cancelDownload(10);
  selection.resolve(['file://selected/report.txt']);
  const end = await host.completed.promise;
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(end.cancelled, true);
  assert.equal(host.requests.length, 0);
  assert.equal(host.files.get('file://selected/report.txt').toString(), 'existing file');
  assert.equal(host.events.filter(event => event.done).length, 1);
});

test('redirect responses are rejected before exporting the temporary file', async () => {
  const host = createHost({ status: 302, payload: 'redirect body' });
  host.service.startDownload('report.txt', '', 11);
  const end = await host.completed.promise;
  await new Promise(resolve => setImmediate(resolve));
  assert.notEqual(end.error, '');
  assert.equal(host.files.get('file://selected/report.txt').toString(), 'existing file');
  assert.equal([...host.files.keys()].filter(path => path.startsWith('/cache/')).length, 0);
});

test('external downloads receive no authenticated cookies', async () => {
  const host = createHost();
  host.service.downloadUrl('https://downloads.example.net/file.txt', 'file.txt');
  await host.completed.promise;
  assert.equal(host.requests[0].options.header.Cookie, undefined);
});

test('only legacy downloads report asynchronous failures to the native error surface', async () => {
  for (const download of [
    service => service.downloadFile('report.txt'),
    service => service.downloadUrl('https://downloads.example.net/report.txt', 'report.txt')
  ]) {
    const host = createHost({ status: 403 });
    download(host.service);
    await host.completed.promise;
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(host.legacyFailures(), 1);
  }
  const progress = createHost({ status: 403 });
  progress.service.startDownload('report.txt', '', 13);
  await progress.completed.promise;
  assert.equal(progress.legacyFailures(), 0);
  const share = createHost({ status: 403 });
  await assert.rejects(share.service.shareFile('report.txt', 'text/plain'));
  assert.equal(share.legacyFailures(), 0);
  const blob = createHost({ select: () => { throw new Error('Storage unavailable'); } });
  await assert.rejects(blob.service.downloadBlob('YQ==', 'report.txt'));
  assert.equal(blob.legacyFailures(), 0);
});

test('cancelled, disposed and successful legacy downloads never report a failure', async () => {
  const cancelled = createHost({ select: () => [] });
  cancelled.service.downloadFile('report.txt');
  await cancelled.completed.promise;
  assert.equal(cancelled.legacyFailures(), 0);
  const data = deferred();
  const disposed = createHost({ waitForData: data.promise });
  disposed.service.downloadFile('report.txt');
  await new Promise(resolve => setImmediate(resolve));
  disposed.service.dispose();
  data.resolve();
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(disposed.legacyFailures(), 0);
  const success = createHost();
  success.service.downloadFile('report.txt');
  await success.completed.promise;
  assert.equal(success.legacyFailures(), 0);
});

test('network initialization failure closes the staged file and removes temporary data', async () => {
  const host = createHost({ failCreateRequest: true });
  host.service.downloadFile('report.txt');
  const end = await host.completed.promise;
  await new Promise(resolve => setImmediate(resolve));
  assert.notEqual(end.error, '');
  assert.equal(host.legacyFailures(), 1);
  assert.equal(host.descriptors.size, 0);
  assert.equal([...host.files.keys()].filter(path => path.startsWith('/cache/')).length, 0);
  assert.equal(host.files.get('file://selected/report.txt').toString(), 'existing file');
});

test('headers-only request completion waits for body end, and disposal suppresses stale UI callbacks', async () => {
  const data = deferred();
  const host = createHost({ waitForData: data.promise });
  host.service.startDownload('report.txt', '', 12);
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(host.requests.length, 1);
  assert.equal(host.events.some(event => event.done), false);
  host.service.dispose();
  data.resolve();
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(host.events.some(event => event.done), false);
  assert.equal(host.files.get('file://selected/report.txt').toString(), 'existing file');
  assert.equal(host.requests[0].request.destroyed, true);
  assert.equal(host.descriptors.size, 0);
});

test('multi-file share uses URI records and retains files for the receiving app', async () => {
  const host = createHost({ payload: 'share fixture' });
  await host.service.shareFiles(JSON.stringify(['one.txt', 'two.txt']), JSON.stringify(['text/plain', 'text/plain']));
  assert.equal(host.shares.length, 1);
  assert.equal(host.shares[0].records.length, 2);
  assert.equal(host.shares[0].options.selectionMode, 1);
  assert.equal(host.shares[0].records[0].title, 'one.txt');
  assert.ok(host.shares[0].records.every(record => record.uri.startsWith('file:///cache/')));
  assert.equal([...host.files.keys()].filter(path => path.startsWith('/cache/')).length, 2);
});

test('share text and a small blob use native system services with exact payloads', async () => {
  const host = createHost();
  await host.service.shareText('fixture shared text');
  assert.equal(host.shares[0].records[0].content, 'fixture shared text');
  await host.service.downloadBlob(Buffer.from('blob fixture').toString('base64'), 'file.txt');
  assert.equal(host.files.get('file://selected/report.txt').toString(), 'blob fixture');
  assert.equal(host.requests.length, 0);
});

test('sharing rejects a full 1 GiB cache without starting a network transfer', async () => {
  const host = createHost({ cachedBytes: policy.MAX_SHARED_CACHE_BYTES });
  await assert.rejects(host.service.shareFile('report.txt', 'text/plain'), /分享缓存已满/);
  assert.equal(host.requests.length, 0);
  assert.equal(host.shares.length, 0);
});

test('remaining share-cache quota bounds incoming bytes and failed reservations are released', async () => {
  const host = createHost({ cachedBytes: policy.MAX_SHARED_CACHE_BYTES - 4, payload: '12345' });
  await assert.rejects(host.service.shareFile('one.txt', 'text/plain'), /无法分享文件/);
  assert.equal(host.shares.length, 0);
  await assert.rejects(host.service.shareFile('two.txt', 'text/plain'), /无法分享文件/);
  assert.equal(host.requests.length, 2);
});

for (const language of ['zh', 'en']) {
  test(`download progress errors use the active language (${language})`, async () => {
    const rejected = createHost({ status: 403, language });
    rejected.service.startDownload('report.txt', '', 51);
    const serverError = await rejected.completed.promise;
    assert.equal(serverError.error, nativeMessage('download_rejected', language));
    assert.equal(serverError.cancelled, false);
    assert.equal(rejected.files.get('file://selected/report.txt').toString(), 'existing file');

    const unavailable = createHost({ failCreateRequest: true, language });
    unavailable.service.startDownload('report.txt', '', 52);
    const localError = await unavailable.completed.promise;
    assert.equal(localError.error, nativeMessage('file_operation_failed', language));
    assert.equal(localError.cancelled, false);
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(unavailable.descriptors.size, 0);
    assert.equal([...unavailable.files.keys()].filter(path => path.startsWith('/cache/')).length, 0);
  });

  test(`share and save failures expose stable codes and localized messages (${language})`, async () => {
    const expectFailure = code => error => {
      assert.ok(error instanceof NativeFailure);
      assert.equal(error.code, code);
      assert.equal(error.message, nativeMessage(code, language));
      return true;
    };
    const empty = createHost({ language });
    await assert.rejects(empty.service.shareText(''), expectFailure('share_text_invalid'));
    assert.equal(empty.shares.length, 0);

    const full = createHost({ cachedBytes: policy.MAX_SHARED_CACHE_BYTES, language });
    await assert.rejects(full.service.shareFile('report.txt', 'text/plain'), expectFailure('share_cache_full'));
    assert.equal(full.requests.length, 0);

    const rejected = createHost({ status: 403, language });
    await assert.rejects(rejected.service.shareFile('report.txt', 'text/plain'), expectFailure('file_share_failed'));
    assert.equal(rejected.shares.length, 0);

    const storage = createHost({ select: () => { throw new Error('Storage unavailable'); }, language });
    await assert.rejects(storage.service.downloadBlob('YQ==', 'report.txt'), expectFailure('file_save_failed'));
    assert.equal(storage.files.get('file://selected/report.txt').toString(), 'existing file');
  });
}

test('changing the interface language updates native errors without reconnecting', async () => {
  const host = createHost({ language: 'zh' });
  host.service.setLanguage('en');
  await assert.rejects(host.service.shareText(''), error =>
    error.code === 'share_text_invalid' && error.message === nativeMessage('share_text_invalid', 'en'));
  host.service.setLanguage('zh');
  await assert.rejects(host.service.shareText(''), error =>
    error.code === 'share_text_invalid' && error.message === nativeMessage('share_text_invalid', 'zh'));
});
