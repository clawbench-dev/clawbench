import { describe, expect, it } from 'vitest'
import {
  ATTACH_DRAG_MIME,
  setAttachDragData,
  readAttachDragData,
  hasAttachDragData,
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
})
