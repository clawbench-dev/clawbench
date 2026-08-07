import { ref, onBeforeUnmount } from 'vue'
import { highlightTree, type Highlighter } from '@lezer/highlight'
import { ensureSyntaxTree, syntaxTree } from '@codemirror/language'
import type { EditorView } from '@codemirror/view'
import { fetchCodeSymbols } from '@/composables/useCodeSymbols'
import { buildHighlightedHtml, buildStickyLines, computeStickyOffsets, type ScopeSymbol, type StickyLine } from '@/utils/codeStickyScroll'

/**
 * VS Code-style sticky scroll for the CodeMirror viewer.
 *
 * When the user scrolls past a function/class definition, that definition line
 * sticks to the top of the code area so they always know which scope they're in.
 *
 * Unlike the removed highlight.js-based implementation (useStickyScroll.ts, which
 * measured `.code-line` DOM elements), this computes geometry from CodeMirror's
 * own block layout (`lineBlockAtHeight` / `lineBlockAt`), so wrapped (word-wrap)
 * lines report their true rendered height without DOM measurement.
 *
 * The pure computation lives in utils/codeStickyScroll.ts (unit-tested); this
 * composable owns the lifecycle: fetching symbols, the sticky overlay DOM, the
 * scroll handler, and per-line highlight caching.
 */

const MAX_STICKY = 5

interface StickyOptions {
  /** Smooth-scroll + flash a line when a sticky definition row is clicked. */
  onStickyClick?: (lineNum: number) => void
  /** Highlighter (HighlightStyle) used for sticky-line syntax highlighting. */
  highlighter?: Highlighter
}

export function useCodeStickyScroll(options: StickyOptions = {}) {
  const stickyLines = ref<StickyLine[]>([])

  let view: EditorView | null = null
  let scroller: HTMLElement | null = null
  let overlayEl: HTMLDivElement | null = null
  let symbols: ScopeSymbol[] = []
  const highlighter: Highlighter | undefined = options.highlighter
  let enabled = true
  let rafId: number | null = null
  let scrollHandler: (() => void) | null = null
  let fetchToken = 0
  const highlightCache = new Map<number, string>()

  /**
   * Collect highlight ranges for the given line. `highlightTree` reports absolute
   * document offsets, so they're shifted to be relative to the line start, matching
   * the 0-based slice passed to buildHighlightedHtml. Uses the same HighlightStyle
   * instance as the editor, so the sticky line matches the target line exactly.
   */
  function computeHighlightRanges(lineFrom: number, lineTo: number) {
    if (!view) return []
    const ranges: Array<{ from: number; to: number; classes: string }> = []
    const tree = ensureSyntaxTree(view.state, lineTo) || syntaxTree(view.state)
    if (!tree || !highlighter) return ranges
    highlightTree(
      tree,
      highlighter,
      (f, t, classes) => {
        if (f < lineFrom) f = lineFrom
        if (t > lineTo) t = lineTo
        if (t > f) ranges.push({ from: f - lineFrom, to: t - lineFrom, classes })
      },
      lineFrom,
      lineTo,
    )
    return ranges
  }

  function getLineHtml(lineNum: number): string {
    if (!view) return ''
    if (highlightCache.has(lineNum)) return highlightCache.get(lineNum)!
    const line = view.state.doc.line(lineNum)
    const text = view.state.sliceDoc(line.from, line.to)
    const html = buildHighlightedHtml(text, computeHighlightRanges(line.from, line.to))
    highlightCache.set(lineNum, html)
    return html
  }

  function clearOverlay() {
    if (overlayEl && overlayEl.parentNode) {
      overlayEl.parentNode.removeChild(overlayEl)
    }
    overlayEl = null
  }

  function renderOverlay() {
    if (!scroller) return
    const rows = stickyLines.value
    if (rows.length === 0) {
      clearOverlay()
      return
    }
    if (!overlayEl) {
      overlayEl = document.createElement('div')
      overlayEl.className = 'sticky-scroll-overlay'
      const content = scroller.querySelector('.cm-content')
      if (content) scroller.insertBefore(overlayEl, content)
      else scroller.appendChild(overlayEl)
    }
    // The sticky overlay is a flex item whose horizontal offset varies depending on
    // whether the line-number gutter was present at initial layout or toggled later
    // (it can sit at x=0 or x=gutterWidth). To avoid a double gutter offset, measure
    // both the overlay and the content each render and align the code text to the
    // content's left edge, while the row spans from the overlay to the content's
    // right edge (filling the whole container, not just the visible viewport).
    const scrollerRect = scroller.getBoundingClientRect()
    const overlayRect = overlayEl.getBoundingClientRect()
    const overlayLeft = overlayRect.left - scrollerRect.left
    const contentEl = scroller.querySelector('.cm-content') as HTMLElement | null
    const contentLeft = contentEl ? contentEl.getBoundingClientRect().left - scrollerRect.left : overlayLeft
    const contentRight = contentEl ? contentEl.getBoundingClientRect().right - scrollerRect.left : overlayLeft + scroller.clientWidth
    const { left, width } = computeStickyOffsets(overlayLeft, contentLeft, contentRight)
    overlayEl.style.setProperty('--sticky-left', `${left}px`)
    overlayEl.style.setProperty('--sticky-width', `${width}px`)
    overlayEl.textContent = ''
    for (const s of rows) {
      const row = document.createElement('div')
      row.className = 'sticky-line'
      row.dataset.line = String(s.lineNum)
      row.style.top = `${s.top}px`
      row.style.height = `${s.height}px`
      // No line number here — CodeMirror's own gutter already shows it.
      const code = document.createElement('span')
      code.className = 'sticky-line-code'
      code.innerHTML = getLineHtml(s.lineNum)
      row.appendChild(code)
      row.addEventListener('click', () => options.onStickyClick?.(s.lineNum))
      overlayEl.appendChild(row)
    }
  }

  function update() {
    if (!view || !scroller || symbols.length === 0 || !enabled) {
      if (stickyLines.value.length > 0) {
        stickyLines.value = []
        renderOverlay()
      }
      return
    }
    const rows = buildStickyLines(view, symbols, scroller.scrollTop, MAX_STICKY)
    const changed =
      rows.length !== stickyLines.value.length ||
      rows.some((r, i) => r.lineNum !== stickyLines.value[i]?.lineNum || r.top !== stickyLines.value[i]?.top || r.height !== stickyLines.value[i]?.height)
    if (!changed) return
    stickyLines.value = rows
    renderOverlay()
  }

  function onScroll() {
    if (rafId) return
    rafId = requestAnimationFrame(() => {
      rafId = null
      update()
    })
  }

  function attachScroll() {
    detachScroll()
    if (!scroller) return
    scrollHandler = onScroll
    scroller.addEventListener('scroll', scrollHandler, { passive: true })
    update()
  }

  function detachScroll() {
    if (scrollHandler && scroller) {
      scroller.removeEventListener('scroll', scrollHandler)
    }
    scrollHandler = null
    if (rafId) {
      cancelAnimationFrame(rafId)
      rafId = null
    }
  }

  /**
   * (Re)initialize sticky scroll for a file. Re-fetches symbols from the backend
   * tree-sitter API.
   */
  function init(v: EditorView, filePath: string | null | undefined, isEnabled = true) {
    view = v
    scroller = v.scrollDOM
    enabled = isEnabled
    detachScroll()
    symbols = []
    stickyLines.value = []
    highlightCache.clear()
    clearOverlay()
    const token = ++fetchToken
    if (!enabled || !filePath) return
    fetchCodeSymbols(filePath)
      .then((res) => {
        if (token !== fetchToken || !view || !enabled) return
        if (res && res.symbols.length > 0) {
          symbols = [...res.symbols].sort((a, b) => a.line - b.line)
        }
        attachScroll()
      })
      .catch(() => {})
  }

  /** Toggle sticky scroll on/off without re-fetching symbols. */
  function setEnabled(next: boolean) {
    enabled = next
    if (!enabled) {
      detachScroll()
      stickyLines.value = []
      clearOverlay()
    } else if (view) {
      attachScroll()
    }
  }

  /**
   * Call when the editor geometry or content may have changed (line-number toggle,
   * word-wrap toggle, resize, doc edit). Forces a re-render so geometry-derived
   * CSS vars (--sticky-left / --sticky-width) are re-measured even when the pinned
   * row set is unchanged.
   */
  function refresh() {
    highlightCache.clear()
    renderOverlay()
  }

  function teardown() {
    fetchToken++
    detachScroll()
    symbols = []
    stickyLines.value = []
    highlightCache.clear()
    clearOverlay()
    view = null
    scroller = null
  }

  onBeforeUnmount(teardown)

  return {
    stickyLines,
    init,
    setEnabled,
    refresh,
    teardown,
  }
}

export type { StickyLine }
