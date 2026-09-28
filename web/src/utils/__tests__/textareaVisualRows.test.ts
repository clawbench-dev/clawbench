import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { measureCaretVisualRows } from '@/utils/textareaVisualRows'

/**
 * jsdom performs no layout: every textarea reports clientWidth 0, scrollHeight 0
 * and `line-height: normal`. The helper must report "unmeasurable" (null) in
 * that world rather than inventing row numbers, so these tests install a fake
 * layout: a fixed column count decides how a line wraps, and scrollHeight is
 * derived from it exactly the way a real engine would.
 */
const LINE_HEIGHT = 18
const COLUMNS = 20

function rowsOf(value: string): number {
  // Model the engine's soft wrap: each logical line occupies ceil(len/columns)
  // visual rows, and an empty line still occupies one.
  return value
    .split('\n')
    .reduce((sum, line) => sum + Math.max(1, Math.ceil(line.length / COLUMNS)), 0)
}

const layout = {
  lineHeight: '18px',
  paddingTop: '4px',
  paddingBottom: '4px',
  clientWidth: 200,
}

// Several cases mutate `layout` to model a degenerate environment, so each test
// starts from the baseline again.
beforeEach(() => {
  layout.lineHeight = '18px'
  layout.paddingTop = '4px'
  layout.paddingBottom = '4px'
  layout.clientWidth = 200
})

function stubLayout() {
  vi.stubGlobal('getComputedStyle', () => ({
    lineHeight: layout.lineHeight,
    paddingTop: layout.paddingTop,
    paddingBottom: layout.paddingBottom,
    fontFamily: 'monospace',
    fontSize: '13px',
    fontWeight: '400',
    fontStyle: 'normal',
    letterSpacing: 'normal',
    wordSpacing: 'normal',
    textTransform: 'none',
    textIndent: '0px',
    tabSize: '8',
    whiteSpace: 'pre-wrap',
    wordBreak: 'break-word',
    overflowWrap: 'break-word',
    paddingRight: '4px',
    paddingLeft: '4px',
    borderTopWidth: '0px',
    borderRightWidth: '0px',
    borderBottomWidth: '0px',
    borderLeftWidth: '0px',
  }))

  Object.defineProperty(HTMLTextAreaElement.prototype, 'clientWidth', {
    configurable: true,
    get() {
      return layout.clientWidth
    },
  })
  Object.defineProperty(HTMLTextAreaElement.prototype, 'scrollHeight', {
    configurable: true,
    get() {
      return rowsOf(this.value) * LINE_HEIGHT + 8
    },
  })
}

/** Create a textarea in the document with the caret at `pos`. */
function makeTextarea(value: string, pos?: number): HTMLTextAreaElement {
  const el = document.createElement('textarea')
  el.value = value
  document.body.appendChild(el)
  el.setSelectionRange(pos ?? value.length, pos ?? value.length)
  return el
}

afterEach(() => {
  vi.unstubAllGlobals()
  // Restore the jsdom originals so other tests in the same worker are unaffected.
  delete (HTMLTextAreaElement.prototype as unknown as Record<string, unknown>).clientWidth
  delete (HTMLTextAreaElement.prototype as unknown as Record<string, unknown>).scrollHeight
  document.body.innerHTML = ''
})

describe('measureCaretVisualRows', () => {
  it('counts soft-wrapped rows, not just explicit newlines', () => {
    stubLayout()
    // 36 chars over 20 columns = 2 visual rows, with zero newlines.
    const text = 'a'.repeat(36)
    const el = makeTextarea(text, text.length)

    const result = measureCaretVisualRows(el)
    expect(result).toEqual({ caretRow: 1, totalRows: 2 })
  })

  it('reports row 0 at the start of a soft-wrapped draft', () => {
    stubLayout()
    const el = makeTextarea('a'.repeat(36), 0)

    expect(measureCaretVisualRows(el)).toEqual({ caretRow: 0, totalRows: 2 })
  })

  it('counts explicit newlines and soft wraps together', () => {
    stubLayout()
    // Row 0: 'a'*25 wraps to 2 rows (25 > 20). Then a newline, then 5 chars.
    const text = `${'a'.repeat(25)}\nbbbbb`
    const el = makeTextarea(text, text.length)

    // 3 visual rows total; the caret after 'bbbbb' is on the last one.
    expect(measureCaretVisualRows(el)).toEqual({ caretRow: 2, totalRows: 3 })
  })

  it('treats a trailing newline as opening a new row', () => {
    stubLayout()
    const el = makeTextarea('abc\n', 4)

    expect(measureCaretVisualRows(el)).toEqual({ caretRow: 1, totalRows: 2 })
  })

  it('returns null when the line-height is not a pixel value', () => {
    stubLayout()
    layout.lineHeight = 'normal'
    const el = makeTextarea('abc')

    expect(measureCaretVisualRows(el)).toBeNull()
  })

  it('returns null when there is no width to wrap against', () => {
    stubLayout()
    layout.clientWidth = 0
    const el = makeTextarea('abc')

    expect(measureCaretVisualRows(el)).toBeNull()
  })

  it('returns null for a missing element', () => {
    stubLayout()
    expect(measureCaretVisualRows(null)).toBeNull()
    expect(measureCaretVisualRows(undefined)).toBeNull()
  })

  it('leaves no mirror behind in the document', () => {
    stubLayout()
    const before = document.body.querySelectorAll('textarea').length
    measureCaretVisualRows(makeTextarea('abc'))

    expect(document.body.querySelectorAll('textarea').length).toBe(before + 1)
  })

  it('removes the mirror even when measuring throws', () => {
    stubLayout()
    // Force the measurement body to throw after the mirror is attached.
    Object.defineProperty(HTMLTextAreaElement.prototype, 'scrollHeight', {
      configurable: true,
      get() {
        throw new Error('boom')
      },
    })
    const el = makeTextarea('abc')
    const before = document.body.querySelectorAll('textarea').length

    expect(() => measureCaretVisualRows(el)).toThrow('boom')
    expect(document.body.querySelectorAll('textarea').length).toBe(before)
  })
})
