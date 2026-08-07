import { describe, it, expect } from 'vitest'
import {
  escapeHtml,
  buildHighlightedHtml,
  firstVisibleLineNumber,
  findEnclosingScopes,
  buildStickyLines,
  type ScopeSymbol,
  type StickyView,
} from '@/utils/codeStickyScroll'

// ─── Helpers ──────────────────────────────────────────────────────────────────
function sym(line: number, endLine: number, extra: Partial<ScopeSymbol> = {}): ScopeSymbol {
  return { name: 'f', kind: 'function', line, endLine, level: 1, ...extra }
}

/**
 * Build a minimal StickyView mock. `heights` maps line number -> rendered height.
 * Each line is assumed 1 line tall unless overridden. `lineHeights` and `topFor`
 * simulate CM's block geometry where block.top = sum of heights of previous lines.
 */
function makeView(lines: number, heights: Record<number, number> = {}): StickyView {
  const lineTop: Record<number, number> = {}
  let acc = 0
  for (let n = 1; n <= lines; n++) {
    lineTop[n] = acc
    acc += heights[n] ?? 20
  }
  return {
    lineBlockAtHeight(height: number) {
      const clamped = Math.max(0, height)
      for (let n = 1; n <= lines; n++) {
        if (clamped >= lineTop[n] && clamped < lineTop[n] + (heights[n] ?? 20)) {
          return { from: n * 100 }
        }
      }
      // beyond last line -> clamp to last line's block start
      return { from: lines * 100 }
    },
    lineBlockAt(pos: number) {
      const n = Math.max(1, Math.min(lines, Math.round(pos / 100)))
      return { top: lineTop[n], height: heights[n] ?? 20 }
    },
    state: {
      doc: {
        lineAt(pos: number) {
          return { number: Math.max(1, Math.min(lines, Math.round(pos / 100))) }
        },
        line(n: number) {
          return { from: Math.max(1, Math.min(lines, n)) * 100 }
        },
      },
    },
  }
}

// ─── escapeHtml ───────────────────────────────────────────────────────────────
describe('escapeHtml', () => {
  it('escapes HTML-special characters', () => {
    expect(escapeHtml('<div a="b">\'&\'</div>')).toBe('&lt;div a=&quot;b&quot;&gt;&#39;&amp;&#39;&lt;/div&gt;')
  })

  it('leaves plain text untouched', () => {
    expect(escapeHtml('func foo(x) { return x }')).toBe('func foo(x) { return x }')
  })
})

// ─── buildHighlightedHtml ─────────────────────────────────────────────────────
describe('buildHighlightedHtml', () => {
  it('wraps styled ranges in spans with the given classes', () => {
    const text = 'const x = 1'
    const ranges = [
      { from: 0, to: 5, classes: 'tok-keyword' },
      { from: 10, to: 11, classes: 'tok-number' },
    ]
    expect(buildHighlightedHtml(text, ranges)).toBe(
      '<span class="tok-keyword">const</span> x = <span class="tok-number">1</span>',
    )
  })

  it('emits escaped plain text for gaps between ranges', () => {
    const text = 'a < b'
    const ranges = [{ from: 0, to: 1, classes: 'tok-var' }]
    expect(buildHighlightedHtml(text, ranges)).toBe('<span class="tok-var">a</span> &lt; b')
  })

  it('returns fully escaped text when there are no ranges', () => {
    expect(buildHighlightedHtml('<x>', [])).toBe('&lt;x&gt;')
  })
})

// ─── firstVisibleLineNumber ───────────────────────────────────────────────────
describe('firstVisibleLineNumber', () => {
  it('resolves the line at the given scrollTop', () => {
    const view = makeView(10)
    expect(firstVisibleLineNumber(view, 0)).toBe(1)
    expect(firstVisibleLineNumber(view, 25)).toBe(2)
    expect(firstVisibleLineNumber(view, 65)).toBe(4)
  })

  it('clamps negative scrollTop to the first line', () => {
    const view = makeView(5)
    expect(firstVisibleLineNumber(view, -50)).toBe(1)
  })
})

// ─── findEnclosingScopes ──────────────────────────────────────────────────────
describe('findEnclosingScopes', () => {
  it('returns only scopes containing the line, sorted outermost-first', () => {
    const symbols = [
      sym(1, 50, { kind: 'class' }),
      sym(10, 30),
      sym(20, 25),
      sym(100, 120), // not enclosing
      sym(1, 40), // smaller, should come after the big one
    ]
    const result = findEnclosingScopes(symbols, 22)
    expect(result.map((s) => [s.line, s.endLine])).toEqual([
      [1, 50],
      [1, 40],
      [10, 30],
      [20, 25],
    ])
  })

  it('returns empty when nothing encloses the line', () => {
    const symbols = [sym(1, 5), sym(10, 20)]
    expect(findEnclosingScopes(symbols, 30)).toEqual([])
  })
})

// ─── buildStickyLines ─────────────────────────────────────────────────────────
describe('buildStickyLines', () => {
  it('pins enclosing scopes whose definition line is scrolled above the top', () => {
    const view = makeView(50)
    const symbols = [sym(5, 40), sym(20, 30)]
    // scrolled so line 22 is visible (top = 420): def lines 5 and 20 are above
    const rows = buildStickyLines(view, symbols, 420, 5)
    expect(rows.map((r) => r.lineNum)).toEqual([5, 20])
    expect(rows[0]).toMatchObject({ lineNum: 5, top: 0, height: 20 })
    // second row stacks below the first
    expect(rows[1].top).toBe(20)
  })

  it('skips scopes whose definition line is still in view', () => {
    const view = makeView(50)
    const symbols = [sym(5, 40), sym(20, 30)]
    // scrolled to the very top: nothing pinned
    expect(buildStickyLines(view, symbols, 0, 5)).toEqual([])
  })

  it('caps the number of pinned rows at maxSticky', () => {
    const view = makeView(200)
    const symbols = [sym(1, 200), sym(10, 190), sym(20, 180), sym(30, 170), sym(40, 160), sym(50, 150)]
    const rows = buildStickyLines(view, symbols, 1200, 5)
    expect(rows.length).toBe(5)
  })

  it('honours wrapped-line heights from block geometry', () => {
    const view = makeView(50, { 5: 60 }) // line 5 wraps to 3 rows -> height 60
    const symbols = [sym(5, 40)]
    const rows = buildStickyLines(view, symbols, 300, 5)
    expect(rows[0]).toMatchObject({ lineNum: 5, height: 60 })
  })
})
