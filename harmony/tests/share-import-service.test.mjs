import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { runInNewContext } from 'node:vm';
import * as policy from '../entry/src/main/ets/models/ShareImportPolicy.ts';
import { safeFileName, shareMime } from '../entry/src/main/ets/models/FileTransferPolicy.ts';
import { parseServerAddress } from '../entry/src/main/ets/models/ServerAddress.ts';
import { NativeFailure, nativeMessage, formatNativeError } from '../entry/src/main/ets/models/NativeMessages.ts';

const source = readFileSync(new URL(
  '../entry/src/main/ets/services/ShareImportService.ets', import.meta.url
), 'utf8').replace(/^import[\s\S]*?;\r?\n/gm, '').replace('export class ShareImportService', 'class ShareImportService');
const executable = stripTypeScriptTypes(source, { mode: 'strip' }) + '\nglobalThis.Service = ShareImportService;';
const defaultRecords = [{ uri: 'file://docs/storage/Downloads/report.txt', utd: 'general.file' }];
const tick = () => new Promise(resolve => setImmediate(resolve));

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function createHost({ records = defaultRecords, readWait, responses, uploadWait } = {}) {
  const files = new Map(records.filter(record => record.uri).map(record => [record.uri, Buffer.from('shared fixture')]));
  const descriptors = new Map();
  const requests = [];
  const changes = [];
  const opened = [];
  let cookie = 'clawbench_session=fixture; clawbench_project=%2Fproject';
  let nextFd = 0;
  let nextUuid = 0;
  let parsedWants = 0;
  const fs = {
    OpenMode: { READ_ONLY: 0, WRITE_ONLY: 1, CREATE: 64, TRUNC: 512 },
    accessSync: () => true,
    mkdirSync() {},
    listFileSync: () => [],
    unlinkSync: path => files.delete(path),
    async stat(fd) {
      return { size: files.get(descriptors.get(fd).path).length, isFile: () => true };
    },
    async open(path, mode) {
      opened.push(path);
      if (mode & 512) files.set(path, Buffer.alloc(0));
      if (!files.has(path)) throw new Error('File not found');
      const fd = ++nextFd;
      descriptors.set(fd, { path, position: 0 });
      return { fd, name: path.split('/').pop() };
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
      if (readWait) await readWait;
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
    RequestMethod: { POST: 'POST' }, HttpDataType: { STRING: 0 },
    createHttp() {
      const aborted = deferred();
      const request = {
        destroyed: false,
        destroy() {
          request.destroyed = true;
          aborted.reject(new Error('Request cancelled'));
        },
        async request(url, options) {
          requests.push({ url, options, request });
          if (uploadWait) await Promise.race([uploadWait, aborted.promise]);
          const response = responses?.[requests.length - 1];
          return response || { responseCode: 200, result: JSON.stringify({
            ok: true, path: '.clawbench/share-in/' + options.multiFormDataList[0].remoteFileName
          }) };
        }
      };
      // Destroy is also the normal resource release after a successful response.
      void aborted.promise.catch(() => {});
      return request;
    }
  };
  const context = {
    ...policy, safeFileName, shareMime, parseServerAddress, NativeFailure, nativeMessage, fs, http,
    nativeCookieHeader: (_origin, header) => {
      if (!header.includes('clawbench_session=fixture')) throw new Error('Not authenticated');
      return header;
    },
    webview: { WebCookieManager: { fetchCookieSync: () => cookie } },
    uniformTypeDescriptor: { getTypeDescriptor: () => ({ mimeTypes: ['application/octet-stream'] }) },
    util: { generateRandomUUID: () => String(++nextUuid) },
    systemShare: {
      async getSharedData() {
        parsedWants++;
        return { getRecords: () => records };
      }
    }
  };
  runInNewContext(executable, context);
  const service = context.Service.get({ cacheDir: '/cache', abilityInfo: { bundleName: 'com.clawbench.harmony' } });
  service.onChange(value => changes.push(value ? JSON.parse(JSON.stringify(value)) : null));
  return {
    service, files, descriptors, requests, changes, opened, parsedWants: () => parsedWants,
    setCookie: value => { cookie = value; }
  };
}

test('ShareKit input is staged locally and only a matching confirmed project permits native multipart upload', async () => {
  const host = createHost();
  assert.equal(await host.service.receive({ action: 'ohos.want.action.sendData' }), true);
  assert.equal(host.requests.length, 0);
  const summary = host.service.summary();
  assert.deepEqual(Array.from(summary.names), ['report.txt']);
  assert.equal(summary.busy, false);
  assert.equal(JSON.stringify(summary).includes('file://'), false);
  const destination = host.service.destination('https://example.com');
  assert.equal(destination.projectPath, '/project');
  const result = await host.service.uploadConfirmed(summary.id, destination.server, destination.projectPath);
  assert.deepEqual(Array.from(result.paths), ['.clawbench/share-in/report.txt']);
  assert.equal(result.failed, 0);
  assert.equal(result.cancelled, false);
  const { url, options } = host.requests[0];
  assert.equal(url, 'https://example.com/api/upload/file');
  assert.equal(options.maxRedirects, 0);
  assert.ok(options.multiFormDataList[0].filePath.startsWith('/cache/clawbench-share-in/import_'));
  assert.equal(options.multiFormDataList[0].data, undefined);
  assert.equal(options.multiFormDataList[1].data, '.clawbench/share-in');
  assert.equal(options.header.Cookie, 'clawbench_session=fixture; clawbench_project=%2Fproject');
  assert.equal(host.service.summary(), null);
  assert.equal([...host.files.keys()].filter(path => path.startsWith('/cache')).length, 0);
  assert.equal(host.descriptors.size, 0);
});

test('project switches or missing authentication cannot redirect an already confirmed import', async () => {
  const host = createHost();
  await host.service.receive({ action: 'ohos.want.action.sendData' });
  const summary = host.service.summary();
  host.setCookie('clawbench_session=fixture; clawbench_project=%2Fanother');
  await assert.rejects(host.service.uploadConfirmed(summary.id, 'https://example.com', '/project'), /目标项目已改变/);
  assert.equal(host.requests.length, 0);
  assert.equal(host.service.summary().id, summary.id);
  host.setCookie('clawbench_project=%2Fproject');
  await assert.rejects(host.service.uploadConfirmed(summary.id, 'https://example.com', '/project'), /Not authenticated/);
  assert.equal(host.requests.length, 0);
});

test('a stale confirmation cannot cancel a newer staged share batch', async () => {
  const host = createHost();
  await host.service.receive({ action: 'ohos.want.action.sendData' });
  const first = host.service.summary();
  await host.service.receive({ action: 'ohos.want.action.sendData' });
  const second = host.service.summary();
  assert.notEqual(first.id, second.id);
  await assert.rejects(host.service.uploadConfirmed(first.id, 'https://example.com', '/project'));
  host.service.cancel(first.id);
  assert.equal(host.service.summary().id, second.id);
  assert.equal([...host.files.keys()].filter(path => path.startsWith('/cache')).length, 1);
  const result = await host.service.uploadConfirmed(second.id, 'https://example.com', '/project');
  assert.equal(result.failed, 0);
  assert.equal(result.paths.length, 1);
  assert.equal(host.requests.length, 1);
  host.service.cancel(second.id);
  assert.equal(host.service.summary(), null);
});

test('arbitrary want parameters, own sandbox URIs and ordinary launch wants cannot read local files', async () => {
  const normal = createHost();
  assert.equal(await normal.service.receive({ action: 'ohos.want.action.home', uri: 'file:///private' }), false);
  assert.equal(normal.parsedWants(), 0);
  for (const records of [[{ utd: 'general.file' }],
    [{ uri: 'file://com.clawbench.harmony/data/storage/el2/base/files/private', utd: 'general.file' }]]) {
    const host = createHost({ records });
    await assert.rejects(host.service.receive({
      action: 'ohos.want.action.sendData', parameters: { path: '/data/storage/el2/base/files/private' }
    }));
    assert.equal(host.opened.length, 0);
    assert.equal(host.requests.length, 0);
  }
});

test('cancel during local staging closes descriptors and removes unfinished temporary files', async () => {
  const read = deferred();
  const host = createHost({ readWait: read.promise });
  const received = host.service.receive({ action: 'ohos.want.action.sendData' });
  const rejected = assert.rejects(received);
  await tick();
  host.service.cancel();
  read.resolve();
  await rejected;
  assert.equal(host.service.summary(), null);
  assert.equal(host.descriptors.size, 0);
  assert.equal([...host.files.keys()].filter(path => path.startsWith('/cache')).length, 0);
  assert.equal(host.requests.length, 0);
});

test('cancel during an upload aborts the owned request and prevents late attachments', async () => {
  const upload = deferred();
  const host = createHost({ uploadWait: upload.promise });
  await host.service.receive({ action: 'ohos.want.action.sendData' });
  const resultPromise = host.service.uploadConfirmed(host.service.summary().id, 'https://example.com', '/project');
  await tick();
  host.service.cancel();
  const result = await resultPromise;
  upload.resolve();
  assert.equal(result.cancelled, true);
  assert.equal(result.paths.length, 0);
  assert.equal(host.requests[0].request.destroyed, true);
  assert.equal([...host.files.keys()].filter(path => path.startsWith('/cache')).length, 0);
});

test('partial batches report only confirmed paths, stop after failure and never overwrite remotely', async () => {
  const records = ['one.txt', 'two.txt', 'three.txt'].map(name => ({
    uri: 'file://docs/storage/Downloads/' + name, utd: 'general.file'
  }));
  const host = createHost({ records, responses: [
    { responseCode: 200, result: '{"ok":true,"path":".clawbench/share-in/one_1.txt"}' },
    { responseCode: 413, result: 'File too large' }
  ] });
  await host.service.receive({ action: 'ohos.want.action.sendData' });
  const result = await host.service.uploadConfirmed(host.service.summary().id, 'https://example.com', '/project');
  assert.deepEqual(Array.from(result.paths), ['.clawbench/share-in/one_1.txt']);
  assert.equal(result.failed, 2);
  assert.equal(result.cancelled, false);
  assert.equal(host.requests.length, 2);
  assert.ok(host.requests.every(({ options }) => !options.multiFormDataList.some(field => field.name === 'onconflict')));
});

test('shared-file errors retain stable codes for localization at the active UI boundary', async () => {
  const invalid = createHost({ records: [{ utd: 'general.file' }] });
  await assert.rejects(invalid.service.receive({ action: 'ohos.want.action.sendData' }), error => {
    assert.ok(error instanceof NativeFailure);
    assert.equal(error.code, 'share_read_failed');
    assert.equal(formatNativeError(error, 'en'), nativeMessage('share_read_failed', 'en'));
    return true;
  });
  const changed = createHost();
  await changed.service.receive({ action: 'ohos.want.action.sendData' });
  await assert.rejects(changed.service.uploadConfirmed('expired-batch', 'https://example.com', '/project'), error => {
    assert.equal(error.code, 'share_changed');
    assert.equal(formatNativeError(error, 'en'), nativeMessage('share_changed', 'en'));
    return true;
  });
  changed.setCookie('clawbench_session=fixture; clawbench_project=%2Fanother');
  await assert.rejects(changed.service.uploadConfirmed(changed.service.summary().id,
    'https://example.com', '/project'), error => {
    assert.equal(error.code, 'share_target_changed');
    assert.equal(formatNativeError(error, 'zh'), nativeMessage('share_target_changed', 'zh'));
    return true;
  });
  assert.equal(changed.requests.length, 0);
  changed.service.cancel();
});
