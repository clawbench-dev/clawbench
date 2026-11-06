import { test } from 'node:test';
import assert from 'node:assert/strict';
import { filePickerPlan, FileSelectionGate } from '../entry/src/main/ets/models/FilePickerPolicy.ts';

test('media selection uses Gallery and capture uses Camera with one output', () => {
  assert.equal(filePickerPlan(['image/*'], false, false).kind, 'photos');
  assert.equal(filePickerPlan(['video/*', '.JPG'], false, true).kind, 'photos');
  const camera = filePickerPlan(['video/*'], true, true);
  assert.equal(camera.kind, 'camera');
  assert.equal(camera.maxSelectNumber, 1);
  assert.equal(camera.videos, true);
  assert.equal(camera.images, false);
});

test('document selection preserves mixed types and bounds filters and selection count', () => {
  const documents = filePickerPlan(['application/pdf', '.TXT', '.pdf'], false, true);
  assert.equal(documents.kind, 'documents');
  assert.equal(documents.maxSelectNumber, 20);
  assert.deepEqual(documents.suffixFilters, ['.pdf,.txt']);
  assert.equal(filePickerPlan(['image/*', 'application/pdf'], false, true).kind, 'documents');
  assert.deepEqual(filePickerPlan(['application/unknown'], false, false).suffixFilters, []);
  assert.deepEqual(filePickerPlan(['.pdf', '../secret', '*.*'], false, true).suffixFilters, []);
  assert.equal(filePickerPlan([], true, false).kind, 'documents');
});

test('picker selection returns only the requested count and cancellation reports an empty list', async () => {
  const gate = new FileSelectionGate();
  const lists = [];
  await gate.select({ handleFileList: (files) => lists.push(files) }, () => true,
    async () => ['file://a', 'file://b'], 1);
  assert.deepEqual(lists, [['file://a']]);
  await gate.select({ handleFileList: (files) => lists.push(files) }, () => true,
    async () => { throw new Error('cancelled'); }, 1);
  assert.deepEqual(lists, [['file://a'], []]);
});

test('navigation settles a file request once and ignores files arriving for an old page', async () => {
  const gate = new FileSelectionGate();
  const lists = [];
  let finish;
  const work = gate.select({ handleFileList: (files) => lists.push(files) }, () => true,
    () => new Promise((resolve) => { finish = resolve; }), 20);
  gate.cancel();
  finish(['file://private-old-result']);
  await work;
  assert.deepEqual(lists, [[]]);
});

test('concurrent file pickers never open two system panels', async () => {
  const gate = new FileSelectionGate();
  let finish;
  const firstLists = [];
  const secondLists = [];
  const work = gate.select({ handleFileList: (files) => firstLists.push(files) }, () => true,
    () => new Promise((resolve) => { finish = resolve; }), 20);
  await gate.select({ handleFileList: (files) => secondLists.push(files) }, () => true,
    async () => assert.fail('unexpected picker'), 20);
  finish([]);
  await work;
  assert.deepEqual(firstLists, [[]]);
  assert.deepEqual(secondLists, [[]]);
});
