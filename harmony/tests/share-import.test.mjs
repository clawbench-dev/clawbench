import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  validateSharedUri, sharedProjectFromCookies, validateUploadedSharePath
} from '../entry/src/main/ets/models/ShareImportPolicy.ts';

test('share input only accepts granted external file-provider authorities', () => {
  for (const uri of ['file://docs/storage/Downloads/report.txt',
    'file://media/Photo/1/photo.jpg', 'file://com.example.sender/data/storage/el2/base/files/report.txt']) {
    assert.equal(validateSharedUri(uri, 'com.clawbench.harmony'), uri);
  }
  for (const uri of ['file:///data/storage/el2/base/files/private', 'file://com.clawbench.harmony/data/private',
    'file://COM.CLAWBENCH.HARMONY/data/private', 'file://localhost/data/private',
    '/data/storage/el2/base/files/private', 'https://example.com/file', 'datashare://unknown/private',
    'file://docs/storage/../private', 'file://docs/storage/%2e%2e/private',
    'file://docs/storage/%252e%252e/private', 'file://docs/storage/%5cprivate',
    'file://docs/storage/a%00.txt', 'file://docs/storage/file?target=/private']) {
    assert.throws(() => validateSharedUri(uri, 'com.clawbench.harmony'), undefined, uri);
  }
});

test('destination comes from an already scoped project cookie and decodes the same form encoding as Go', () => {
  assert.equal(sharedProjectFromCookies('clawbench_session=fixture; clawbench_project=%2Fhome%2FMy+Project'),
    '/home/My Project');
  assert.equal(sharedProjectFromCookies('cb20300_clawbench_session=fixture; cb20300_clawbench_project=C%3A%5Cproject'),
    'C:\\project');
  for (const cookies of ['clawbench_session=fixture', 'clawbench_project=',
    'clawbench_project=%00private', 'clawbench_project=%zz',
    'clawbench_project=%2Fone; cb20300_clawbench_project=%2Ftwo']) {
    assert.throws(() => sharedProjectFromCookies(cookies));
  }
});

test('only the upload handler share directory can become a composer attachment', () => {
  assert.equal(validateUploadedSharePath('.clawbench/share-in/报告_1.txt'), '.clawbench/share-in/报告_1.txt');
  for (const path of ['/private.txt', '../private.txt', '.clawbench/share-in/../private.txt',
    '.clawbench/share-in/', '.clawbench/share-in/..', '.clawbench/share-in/a\\b.txt',
    'https://example.com/private', '.clawbench/uploads/report.txt']) {
    assert.throws(() => validateUploadedSharePath(path));
  }
});
