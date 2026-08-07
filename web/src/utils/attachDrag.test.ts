import { describe, it, expect, vi, beforeEach } from 'vitest'
import {
  ATTACH_DRAG_MIME,
  setAttachDragData,
  readAttachDragData,
  hasAttachDragData,
  estimateTextWidth,
  computeAttachDragImageSize,
  toRgba,
  buildAttachDragImage,
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
    expect(w).toBeGreaterThanOrEqual(64)
    expect(h).toBe(38)
  })

  it('returns larger width for long names', () => {
    const short = computeAttachDragImageSize('a')
    const long = computeAttachDragImageSize('a-very-long-file-name.tsx')
    expect(long.w).toBeGreaterThan(short.w)
  })
})

describe('toRgba', () => {
  it('converts 6-digit hex', () => {
    expect(toRgba('#4a90d9', 0.5)).toBe('rgba(74, 144, 217, 0.5)')
  })

  it('converts 3-digit hex', () => {
    expect(toRgba('#abc', 1)).toBe('rgba(170, 187, 204, 1)')
  })

  it('converts hex without # prefix', () => {
    expect(toRgba('4a90d9', 0.3)).toBe('rgba(74, 144, 217, 0.3)')
  })

  it('converts rgb() string', () => {
    expect(toRgba('rgb(74, 144, 217)', 0.8)).toBe('rgba(74, 144, 217, 0.8)')
  })

  it('converts rgba() string replacing alpha', () => {
    expect(toRgba('rgba(74, 144, 217, 0.5)', 0.9)).toBe('rgba(74, 144, 217, 0.9)')
  })

  it('returns default for unrecognized color', () => {
    expect(toRgba('purple', 0.5)).toBe('rgba(74, 144, 217, 0.5)')
  })

  it('handles empty string', () => {
    expect(toRgba('', 0.5)).toBe('rgba(74, 144, 217, 0.5)')
  })
})

describe('buildAttachDragImage', () => {
  beforeEach(() => {
    document.documentElement.setAttribute('data-theme', 'dark')
  })

  it('returns a canvas element', () => {
    const canvas = buildAttachDragImage('test.ts', false)
    expect(canvas).toBeInstanceOf(HTMLCanvasElement)
  })

  it('sets canvas dimensions based on name', () => {
    const canvas = buildAttachDragImage('test.ts', false)
    expect(canvas.width).toBeGreaterThan(0)
    expect(canvas.height).toBeGreaterThan(0)
  })

  it('creates larger canvas for longer names', () => {
    const short = buildAttachDragImage('a.ts', false)
    const long = buildAttachDragImage('a-very-long-component-name.vue', false)
    expect(long.width).toBeGreaterThan(short.width)
  })

  it('exercises canvas drawing with stubbed 2D context (dark theme)', () => {
    const calls: string[] = []
    const fakeCtx = {
      scale: () => calls.push('scale'),
      fillStyle: '',
      strokeStyle: '',
      lineWidth: 0,
      font: '',
      textBaseline: '',
      textAlign: '',
      beginPath: () => calls.push('beginPath'),
      moveTo: () => {},
      arcTo: () => calls.push('arcTo'),
      closePath: () => calls.push('closePath'),
      fill: () => calls.push('fill'),
      stroke: () => calls.push('stroke'),
      fillText: () => calls.push('fillText'),
    }
    const origCreate = document.createElement.bind(document)
    vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
      if (tag === 'canvas') {
        const el = origCreate('canvas')
        ;(el as any).getContext = (type: string) => type === '2d' ? fakeCtx : null
        return el
      }
      return origCreate(tag)
    })
    document.documentElement.setAttribute('data-theme', 'dark')
    buildAttachDragImage('folder', true)
    expect(calls).toContain('scale')
    expect(calls).toContain('fill')
    expect(calls).toContain('stroke')
    expect(calls).toContain('fillText')
    vi.restoreAllMocks()
  })

  it('exercises canvas drawing with stubbed 2D context (light theme)', () => {
    const fakeCtx = {
      scale: () => {},
      fillStyle: '',
      strokeStyle: '',
      lineWidth: 0,
      font: '',
      textBaseline: '',
      textAlign: '',
      beginPath: () => {},
      moveTo: () => {},
      arcTo: () => {},
      closePath: () => {},
      fill: () => {},
      stroke: () => {},
      fillText: () => {},
    }
    const origCreate = document.createElement.bind(document)
    vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
      if (tag === 'canvas') {
        const el = origCreate('canvas')
        ;(el as any).getContext = (type: string) => type === '2d' ? fakeCtx : null
        return el
      }
      return origCreate(tag)
    })
    document.documentElement.setAttribute('data-theme', 'light')
    const canvas = buildAttachDragImage('file.txt', false)
    expect(canvas).toBeInstanceOf(HTMLCanvasElement)
    vi.restoreAllMocks()
  })
})
