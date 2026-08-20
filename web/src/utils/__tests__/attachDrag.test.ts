import { describe, expect, it } from 'vitest'
import {
  ATTACH_DRAG_MIME,
  setAttachDragData,
  readAttachDragData,
  hasAttachDragData,
  estimateTextWidth,
  computeAttachDragImageSize,
} from '@/utils/attachDrag'

/** Minimal DataTransfer stand-in that mirrors jsdom/browser behavior for set/getData + types. */
function makeDataTransfer() {
  const store = new Map<string, string>()
  const types: string[] = []
  return {
    setData(type: string, value: string) {
      if (!store.has(type)) types.push(type)
      store.set(type, value)
    },
    getData(type: string) {
      return store.get(type) ?? ''
    },
    types: types as unknown as readonly string[],
  } as unknown as DataTransfer
}

describe('attachDrag helpers', () => {
  it('round-trips a file/dir payload through the custom MIME type', () => {
    const dt = makeDataTransfer()
    setAttachDragData(dt, '/home/u/a.txt', false)
    expect(readAttachDragData(dt)).toEqual({ path: '/home/u/a.txt', isDir: false })

    const dtDir = makeDataTransfer()
    setAttachDragData(dtDir, '/home/u/src', true)
    expect(readAttachDragData(dtDir)).toEqual({ path: '/home/u/src', isDir: true })
  })

  it('also writes a plain-text fallback path', () => {
    const dt = makeDataTransfer()
    setAttachDragData(dt, '/x/y.md', false)
    expect(dt.getData('text/plain')).toBe('/x/y.md')
  })

  it('hasAttachDragData detects the internal drag by MIME type only', () => {
    const internal = makeDataTransfer()
    setAttachDragData(internal, '/p', false)
    expect(hasAttachDragData(internal)).toBe(true)

    // An OS file drop exposes 'Files' but not our MIME
    const osDrop = makeDataTransfer()
    osDrop.setData('text/plain', '')
    expect(hasAttachDragData(osDrop)).toBe(false)
  })

  it('readAttachDragData returns null for non-internal / malformed payloads', () => {
    expect(readAttachDragData(null)).toBe(null)
    expect(readAttachDragData(undefined)).toBe(null)

    const plain = makeDataTransfer()
    plain.setData('text/plain', 'just a path')
    expect(readAttachDragData(plain)).toBe(null)

    const bad = makeDataTransfer()
    bad.setData(ATTACH_DRAG_MIME, 'not-json')
    expect(readAttachDragData(bad)).toBe(null)

    const missingPath = makeDataTransfer()
    missingPath.setData(ATTACH_DRAG_MIME, JSON.stringify({ isDir: true }))
    expect(readAttachDragData(missingPath)).toBe(null)
  })

  it('estimateTextWidth treats CJK chars as double-width vs latin', () => {
    // 'abc' = 3 latin (~6.5px each) ≈ 19.5; '文件' = 2 CJK (~13px each) = 26
    expect(estimateTextWidth('abc')).toBeCloseTo(19.5)
    expect(estimateTextWidth('文件')).toBeCloseTo(26)
    expect(estimateTextWidth('')).toBe(0)
  })

  it('computeAttachDragImageSize scales with name and enforces a min width', () => {
    const empty = computeAttachDragImageSize('')
    expect(empty.w).toBeGreaterThanOrEqual(80)
    expect(empty.h).toBe(44)

    const long = computeAttachDragImageSize('报告文件报告文件报告.txt')
    expect(long.w).toBeGreaterThan(empty.w)
    expect(long.h).toBe(44)
  })

})
