import { describe, it, expect, beforeEach } from 'vitest'
import {
  ATTACH_DRAG_MIME,
  setAttachDragData,
  readAttachDragData,
  hasAttachDragData,
  estimateTextWidth,
  computeAttachDragImageSize,
  buildAttachDragImage,
  cleanupDragGhost,
  resolveAccentColor,
} from '@/utils/attachDrag'

function mockDataTransfer(): DataTransfer {
  const store: Record<string, string> = {}
  const types: string[] = []
  return {
    setData(type: string, value: string) {
      store[type] = value
      if (!types.includes(type)) types.push(type)
    },
    getData(type: string) {
      return store[type] ?? ''
    },
    get types() {
      return Object.freeze([...types])
    },
  } as unknown as DataTransfer
}

describe('ATTACH_DRAG_MIME', () => {
  it('has expected value', () => {
    expect(ATTACH_DRAG_MIME).toBe('application/x-clawbench-attach')
  })
})

describe('setAttachDragData', () => {
  it('sets both MIME and text/plain', () => {
    const dt = mockDataTransfer()
    setAttachDragData(dt, '/foo/bar.ts', false)
    expect(dt.getData(ATTACH_DRAG_MIME)).toBe('{"path":"/foo/bar.ts","isDir":false}')
    expect(dt.getData('text/plain')).toBe('/foo/bar.ts')
  })

  it('sets isDir true for directories', () => {
    const dt = mockDataTransfer()
    setAttachDragData(dt, '/src', true)
    expect(dt.getData(ATTACH_DRAG_MIME)).toBe('{"path":"/src","isDir":true}')
  })

  it('does not throw when setData throws', () => {
    const dt = { setData: () => { throw new Error('nope') } } as unknown as DataTransfer
    expect(() => setAttachDragData(dt, '/x', false)).not.toThrow()
  })
})

describe('readAttachDragData', () => {
  it('returns null for null', () => {
    expect(readAttachDragData(null)).toBeNull()
  })

  it('returns null for undefined', () => {
    expect(readAttachDragData(undefined)).toBeNull()
  })

  it('returns null when MIME data is empty', () => {
    const dt = mockDataTransfer()
    expect(readAttachDragData(dt)).toBeNull()
  })

  it('reads valid attach data for a file', () => {
    const dt = mockDataTransfer()
    setAttachDragData(dt, '/foo/bar.ts', false)
    expect(readAttachDragData(dt)).toEqual({ path: '/foo/bar.ts', isDir: false })
  })

  it('reads valid attach data for a directory', () => {
    const dt = mockDataTransfer()
    setAttachDragData(dt, '/src', true)
    expect(readAttachDragData(dt)).toEqual({ path: '/src', isDir: true })
  })

  it('coerces isDir to boolean', () => {
    const dt = mockDataTransfer()
    dt.setData(ATTACH_DRAG_MIME, '{"path":"/x","isDir":1}')
    expect(readAttachDragData(dt)).toEqual({ path: '/x', isDir: false })
  })

  it('returns null for non-object JSON', () => {
    const dt = mockDataTransfer()
    dt.setData(ATTACH_DRAG_MIME, '"hello"')
    expect(readAttachDragData(dt)).toBeNull()
  })

  it('returns null when path is not a string', () => {
    const dt = mockDataTransfer()
    dt.setData(ATTACH_DRAG_MIME, '{"path":123}')
    expect(readAttachDragData(dt)).toBeNull()
  })

  it('returns null for malformed JSON', () => {
    const dt = mockDataTransfer()
    dt.setData(ATTACH_DRAG_MIME, '{bad json}')
    expect(readAttachDragData(dt)).toBeNull()
  })

  it('returns null when getData throws', () => {
    const dt = { getData: () => { throw new Error('nope') } } as unknown as DataTransfer
    expect(readAttachDragData(dt)).toBeNull()
  })
})

describe('hasAttachDragData', () => {
  it('returns false for null', () => {
    expect(hasAttachDragData(null)).toBe(false)
  })

  it('returns false for undefined', () => {
    expect(hasAttachDragData(undefined)).toBe(false)
  })

  it('returns true when MIME type is present', () => {
    const dt = mockDataTransfer()
    setAttachDragData(dt, '/x', false)
    expect(hasAttachDragData(dt)).toBe(true)
  })

  it('returns false when MIME type is absent', () => {
    const dt = mockDataTransfer()
    expect(hasAttachDragData(dt)).toBe(false)
  })

  it('returns false when types.includes throws', () => {
    const dt = {
      types: { includes: () => { throw new Error('nope') } },
    } as unknown as DataTransfer
    expect(hasAttachDragData(dt)).toBe(false)
  })
})

describe('estimateTextWidth', () => {
  it('estimates ASCII text width', () => {
    const w = estimateTextWidth('abc')
    expect(w).toBe(6.5 * 3)
  })

  it('estimates CJK character width as larger', () => {
    const w = estimateTextWidth('你')
    expect(w).toBe(13)
  })

  it('handles mixed ASCII and CJK', () => {
    const w = estimateTextWidth('a你b')
    expect(w).toBe(6.5 + 13 + 6.5)
  })

  it('returns 0 for empty string', () => {
    expect(estimateTextWidth('')).toBe(0)
  })
})

describe('computeAttachDragImageSize', () => {
  it('returns minimum width for short names', () => {
    const { w, h } = computeAttachDragImageSize('a')
    expect(w).toBeGreaterThanOrEqual(80)
    expect(h).toBe(44)
  })

  it('returns larger width for long names', () => {
    const short = computeAttachDragImageSize('a')
    const long = computeAttachDragImageSize('a-very-long-file-name.tsx')
    expect(long.w).toBeGreaterThan(short.w)
  })
})

describe('resolveAccentColor', () => {
  it('returns a non-empty color string', () => {
    const color = resolveAccentColor()
    expect(color).toBeTruthy()
    expect(color.length).toBeGreaterThan(0)
  })

  it('returns light fallback when CSS variable is absent and theme is light', () => {
    document.documentElement.removeAttribute('style')
    document.documentElement.setAttribute('data-theme', 'light')
    const color = resolveAccentColor()
    expect(color).toBe('#4a90d9')
  })

  it('returns dark fallback when CSS variable is absent and theme is dark', () => {
    document.documentElement.removeAttribute('style')
    document.documentElement.setAttribute('data-theme', 'dark')
    const color = resolveAccentColor()
    expect(color).toBe('#5b9bd5')
  })
})

describe('buildAttachDragImage', () => {
  beforeEach(() => {
    document.documentElement.setAttribute('data-theme', 'dark')
    cleanupDragGhost()
  })

  it('returns a DOM element appended to the body', () => {
    const el = buildAttachDragImage('test.ts', false)
    expect(el).toBeInstanceOf(HTMLElement)
    expect(el.getAttribute('data-attach-ghost')).toBe('')
    expect(el.parentElement).toBe(document.body)
    cleanupDragGhost()
  })

  it('contains the file name as text', () => {
    const el = buildAttachDragImage('hello.md', false)
    expect(el.textContent).toContain('hello.md')
    cleanupDragGhost()
  })

  it('contains folder SVG for directories', () => {
    const el = buildAttachDragImage('src', true)
    const svg = el.querySelector('svg')
    expect(svg).toBeTruthy()
    cleanupDragGhost()
  })

  it('contains file SVG for files', () => {
    const el = buildAttachDragImage('a.ts', false)
    const svg = el.querySelector('svg')
    expect(svg).toBeTruthy()
    cleanupDragGhost()
  })

  it('uses accent background color', () => {
    const el = buildAttachDragImage('x.ts', false)
    const bg = el.style.background || el.style.backgroundColor
    expect(bg).toBeTruthy()
    cleanupDragGhost()
  })

  it('cleanupDragGhost removes the element from DOM', () => {
    const el = buildAttachDragImage('y.ts', false)
    expect(el.parentElement).toBe(document.body)
    cleanupDragGhost()
    expect(el.parentElement).toBeNull()
  })

  it('buildAttachDragImage cleans up previous ghost', () => {
    const el1 = buildAttachDragImage('first.ts', false)
    expect(el1.parentElement).toBe(document.body)
    const el2 = buildAttachDragImage('second.ts', false)
    expect(el1.parentElement).toBeNull() // first ghost cleaned up
    expect(el2.parentElement).toBe(document.body)
    cleanupDragGhost()
  })
})
