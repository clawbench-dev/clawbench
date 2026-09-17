import { describe, it, expect, beforeEach, vi } from 'vitest'
import {
  ATTACH_DRAG_MIME,
  setAttachDragData,
  setMultiAttachDragData,
  readAttachDragData,
  hasAttachDragData,
  attachDragTargets,
  estimateTextWidth,
  computeAttachDragImageSize,
  buildAttachDragImage,
  cleanupDragGhost,
  resolveAccentColor,
  startAttachDrag,
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

  it('returns the default fallback when CSS variable is absent even in dark theme', () => {
    document.documentElement.removeAttribute('style')
    document.documentElement.setAttribute('data-theme', 'dark')
    const color = resolveAccentColor()
    expect(color).toBe('#4a90d9')
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

  it('a stale safety timer from an earlier ghost does not remove a newer one', () => {
    // Regression: the 5s auto-clean timer used to call cleanupDragGhost()
    // unconditionally. pendingGhost is module-global, so an old timer firing
    // during a newer drag would delete the NEW ghost mid-drag.
    vi.useFakeTimers()
    try {
      const elA = buildAttachDragImage('a.png', false)
      // 2s later a new drag replaces the ghost (A's timer now due at t=5s,
      // B's at t=7s).
      vi.advanceTimersByTime(2000)
      const elB = buildAttachDragImage('b.png', false)
      expect(elA.parentElement).toBeNull()

      // A's stale timer fires at t=5s — B must survive (still current ghost).
      vi.advanceTimersByTime(3000)
      expect(document.querySelector('[data-attach-ghost]')).toBe(elB)

      // B's own timer still cleans it up at t=7s.
      vi.advanceTimersByTime(2000)
      expect(document.querySelector('[data-attach-ghost]')).toBeNull()
    } finally {
      vi.useRealTimers()
      cleanupDragGhost()
    }
  })
})

describe('startAttachDrag', () => {
  beforeEach(() => {
    document.documentElement.setAttribute('data-theme', 'dark')
    cleanupDragGhost()
  })

  interface MockDragEvent {
    e: DragEvent
    dt: DataTransfer
    setDragImage: ReturnType<typeof vi.fn>
  }
  function dragEvent(): MockDragEvent {
    const setDragImage = vi.fn()
    const dt = Object.assign(mockDataTransfer(), { setDragImage }) as DataTransfer
    return { e: { dataTransfer: dt } as unknown as DragEvent, dt, setDragImage }
  }

  it('writes the internal payload, sets effectAllowed and installs a ghost', () => {
    const { e, dt, setDragImage } = dragEvent()
    const ok = startAttachDrag(e, 'assets/logo.png', 'logo.png')
    expect(ok).toBe(true)
    expect(dt.effectAllowed).toBe('copy')
    expect(readAttachDragData(dt)).toEqual({ path: 'assets/logo.png', isDir: false })
    expect(setDragImage).toHaveBeenCalledTimes(1)
    expect(document.querySelector('[data-attach-ghost]')).toBeTruthy()
    cleanupDragGhost()
  })

  it('attaches directories with isDir true', () => {
    const { e, dt } = dragEvent()
    startAttachDrag(e, 'src/utils', 'utils', true)
    expect(readAttachDragData(dt)).toEqual({ path: 'src/utils', isDir: true })
    cleanupDragGhost()
  })

  it('returns false and writes nothing when the path is empty', () => {
    const { e, dt, setDragImage } = dragEvent()
    expect(startAttachDrag(e, '', 'x.png')).toBe(false)
    expect(readAttachDragData(dt)).toBeNull()
    expect(setDragImage).not.toHaveBeenCalled()
  })

  it('returns false when the event has no dataTransfer', () => {
    expect(startAttachDrag({} as DragEvent, 'a.png', 'a.png')).toBe(false)
  })

  it('falls back to the path as the ghost label when name is empty', () => {
    const { e } = dragEvent()
    startAttachDrag(e, 'deep/dir/pic.png', '')
    const ghost = document.querySelector('[data-attach-ghost]')
    expect(ghost?.textContent).toContain('deep/dir/pic.png')
    cleanupDragGhost()
  })
})

describe('setMultiAttachDragData', () => {
  it('round-trips the whole multi-selection alongside the cursor item', () => {
    const dt = mockDataTransfer()
    setMultiAttachDragData(dt, 'src/a.ts', false, [
      { path: 'src/a.ts', isDir: false },
      { path: 'src', isDir: true },
    ])
    expect(readAttachDragData(dt)).toEqual({
      path: 'src/a.ts',
      isDir: false,
      entries: [
        { path: 'src/a.ts', isDir: false },
        { path: 'src', isDir: true },
      ],
    })
    expect(dt.getData('text/plain')).toBe('src/a.ts')
  })

  it('degrades to a single-item payload when fewer than two entries are given', () => {
    const dt = mockDataTransfer()
    setMultiAttachDragData(dt, 'src/a.ts', false, [{ path: 'src/a.ts', isDir: false }])
    // Exactly the single-item shape — no `entries` key at all.
    expect(readAttachDragData(dt)).toEqual({ path: 'src/a.ts', isDir: false })
    expect(dt.getData(ATTACH_DRAG_MIME)).not.toContain('entries')
  })

  it('does not throw when setData throws', () => {
    const dt = { setData: () => { throw new Error('nope') } } as unknown as DataTransfer
    expect(() => setMultiAttachDragData(dt, '/x', false, [
      { path: '/x', isDir: false },
      { path: '/y', isDir: false },
    ])).not.toThrow()
  })
})

describe('multi-selection payload sanitizing', () => {
  it('omits the entries key entirely when absent, so single-item payloads stay byte-identical', () => {
    const dt = mockDataTransfer()
    dt.setData(ATTACH_DRAG_MIME, '{"path":"/x","isDir":false}')
    const data = readAttachDragData(dt)
    expect(data).toEqual({ path: '/x', isDir: false })
    expect(Object.prototype.hasOwnProperty.call(data, 'entries')).toBe(false)
  })

  it('drops malformed entries and coerces isDir to boolean', () => {
    const dt = mockDataTransfer()
    dt.setData(ATTACH_DRAG_MIME, JSON.stringify({
      path: '/cursor',
      isDir: false,
      entries: [
        { path: '/ok', isDir: 1 },
        { path: '', isDir: false },        // empty path
        { path: 42, isDir: true },         // non-string path
        null,                              // not an object
        'nope',                            // not an object
        { isDir: true },                   // missing path
      ],
    }))
    expect(readAttachDragData(dt)).toEqual({
      path: '/cursor',
      isDir: false,
      entries: [{ path: '/ok', isDir: false }],
    })
  })

  it('drops the entries key when no entry survives sanitizing', () => {
    const dt = mockDataTransfer()
    dt.setData(ATTACH_DRAG_MIME, JSON.stringify({
      path: '/cursor',
      isDir: false,
      entries: [{ path: 42 }, null],
    }))
    const data = readAttachDragData(dt)
    expect(data).toEqual({ path: '/cursor', isDir: false })
    expect(Object.prototype.hasOwnProperty.call(data, 'entries')).toBe(false)
  })

  it('drops the entries key when it is not an array', () => {
    const dt = mockDataTransfer()
    dt.setData(ATTACH_DRAG_MIME, JSON.stringify({ path: '/cursor', isDir: false, entries: 'oops' }))
    const data = readAttachDragData(dt)
    expect(data).toEqual({ path: '/cursor', isDir: false })
    expect(Object.prototype.hasOwnProperty.call(data, 'entries')).toBe(false)
  })
})

describe('attachDragTargets', () => {
  it('expands a multi-selection into every entry as a whole file', () => {
    expect(attachDragTargets({
      path: 'src/a.ts',
      isDir: false,
      entries: [
        { path: 'src/a.ts', isDir: false },
        { path: 'src', isDir: true },
      ],
    })).toEqual([
      { path: 'src/a.ts', isDir: false },
      { path: 'src', isDir: true },
    ])
  })

  it('drops line ranges when entries are present — multi-selection is whole-file only', () => {
    expect(attachDragTargets({
      path: 'docs/guide.md',
      isDir: false,
      startLine: 5,
      endLine: 9,
      entries: [
        { path: 'docs/guide.md', isDir: false },
        { path: 'docs/other.md', isDir: false },
      ],
    })).toEqual([
      { path: 'docs/guide.md', isDir: false },
      { path: 'docs/other.md', isDir: false },
    ])
  })

  it('returns a single target preserving the line range when there are no entries', () => {
    expect(attachDragTargets({ path: 'docs/guide.md', isDir: false, startLine: 5, endLine: 9 }))
      .toEqual([{ path: 'docs/guide.md', isDir: false, startLine: 5, endLine: 9 }])
  })

  it('returns a single whole-file target for a plain drag', () => {
    expect(attachDragTargets({ path: '/x/a.ts', isDir: false }))
      .toEqual([{ path: '/x/a.ts', isDir: false, startLine: undefined, endLine: undefined }])
  })

  it('falls back to the single item when entries is an empty array', () => {
    expect(attachDragTargets({ path: '/x/a.ts', isDir: true, entries: [] }))
      .toEqual([{ path: '/x/a.ts', isDir: true, startLine: undefined, endLine: undefined }])
  })
})
