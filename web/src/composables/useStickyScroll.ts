import { ref, onBeforeUnmount } from 'vue'
import { fetchCodeSymbols } from '@/composables/useCodeSymbols'

/**
 * VS Code-style "Sticky Scroll" for code files.
 * When scrolling past a function/class definition, that definition line
 * sticks to the top of the code area so you always know which scope you're in.
 *
 * Position detection uses getBoundingClientRect() for both the scroll container
 * and line elements, giving viewport-relative coordinates that are correct
 * regardless of CSS transforms, position:relative parents, or scroll offsets.
 *
 * The overlay has min-width: max-content so sticky lines are always complete.
 * Line numbers use position:sticky;left:0 so they stay fixed during horizontal scroll.
 * Code text uses translateX(-scrollLeft) to follow horizontal scroll.
 *
 * Supports both normal and word-wrap modes — in word-wrap mode, each sticky line
 * measures its actual rendered height from the DOM (lines may wrap to multiple rows).
 */
export function useStickyScroll() {
  /** Reactive array of { lineNum, kind, top, height } for lines that should be sticky */
  const stickyLines = ref<Array<{ lineNum: number; kind: string; top: number; height: number }>>([])

  let symbols: Array<{ line: number; endLine: number; kind: string; lineNum: number }> = []
  let scrollEl: HTMLElement | null = null
  let scrollHandler: (() => void) | null = null
  let rafId: number | null = null
  let lineEls: Element[] = []  // cached .code-line elements

  const MAX_STICKY = 5  // max sticky lines to show at once

  function updateSticky() {
    if (!scrollEl || symbols.length === 0) {
      if (stickyLines.value.length > 0) stickyLines.value = []
      return
    }

    // The container's visible top edge in viewport coordinates
    const containerTop = scrollEl.getBoundingClientRect().top

    // Cache line elements on first call or after invalidation
    if (lineEls.length === 0) {
      const codeEl = scrollEl.querySelector(':scope > code')
      if (!codeEl) return
      lineEls = Array.from(codeEl.querySelectorAll(':scope > .code-line'))
      if (lineEls.length === 0) return
    }

    // Find the first line whose top edge is at or below the container's top edge.
    // getBoundingClientRect().top gives the viewport-relative position, which
    // automatically accounts for scroll position, position:relative parents, etc.
    let firstVisibleLine = -1
    for (let i = 0; i < lineEls.length; i++) {
      const lineTop = lineEls[i].getBoundingClientRect().top
      if (lineTop >= containerTop - 0.5) {  // 0.5px tolerance for sub-pixel rounding
        firstVisibleLine = i + 1
        break
      }
    }
    if (firstVisibleLine === -1) firstVisibleLine = lineEls.length

    // Find all enclosing scopes that contain the first visible line.
    // Deduplicate by line number — tree-sitter may return multiple symbols
    // on the same line (e.g., Go return types like `func foo() error`
    // produces both "foo" and "error" as definition.function on the same line).
    const enclosing = []
    const seenLines = new Set()
    for (const sym of symbols) {
      if (sym.line <= firstVisibleLine && sym.endLine >= firstVisibleLine) {
        if (!seenLines.has(sym.line)) {
          enclosing.push(sym)
          seenLines.add(sym.line)
        }
      }
    }

    // Sort by scope width descending (outermost first), then by line ascending for stability
    enclosing.sort((a, b) => {
      const widthDiff = (b.endLine - b.line) - (a.endLine - a.line)
      if (widthDiff !== 0) return widthDiff
      return a.line - b.line
    })

    // Build sticky lines from enclosing scopes.
    // Each line sticks when its DOM position reaches the bottom of the already-sticky zone
    // (containerTop + accTop), not the container top — so multi-level headings stack
    // immediately as they touch the previous sticky line.
    // A sticky line also expires when its endLine content reaches the sticky zone bottom,
    // meaning the scope has ended and the heading is no longer relevant.
    const result = []
    let accTop = 0
    for (let i = 0; i < enclosing.length && result.length < MAX_STICKY; i++) {
      const sym = enclosing[i]
      const defLineEl = lineEls[sym.line - 1]
      if (!defLineEl) continue

      const stickyThreshold = containerTop + accTop

      // Condition A: definition line must be above the sticky zone bottom to stick
      const defTop = defLineEl.getBoundingClientRect().top
      if (defTop >= stickyThreshold - 0.5) continue

      // Condition B: if endLine content has reached the sticky zone, scope has expired
      if (sym.endLine > sym.line) {
        const endLineEl = lineEls[sym.endLine - 1]
        if (endLineEl && endLineEl.getBoundingClientRect().top <= stickyThreshold + 0.5) continue
      }

      const h = defLineEl.getBoundingClientRect().height
      result.push({
        lineNum: sym.line,
        kind: sym.kind,
        top: accTop,   // pixel offset from overlay top
        height: h,     // actual rendered height (supports wrapped lines)
      })
      accTop += h
    }

    stickyLines.value = result

    // Sync horizontal scroll position to code-text elements
    syncHorizontalScroll()
  }

  function syncHorizontalScroll() {
    if (!scrollEl) return
    // Update each sticky line's code-text element to follow horizontal scroll
    const codeTextEls = scrollEl.querySelectorAll('.sticky-line .sticky-code-text')
    const scrollLeft = scrollEl.scrollLeft
    codeTextEls.forEach((el: Element) => {
      (el as HTMLElement).style.transform = `translateX(${-scrollLeft}px)`
    })
  }

  function onScroll() {
    if (rafId) return
    rafId = requestAnimationFrame(() => {
      updateSticky()
      rafId = null
    })
  }

  function attachScroll() {
    detachScroll()
    if (!scrollEl) return
    scrollHandler = onScroll
    scrollEl.addEventListener('scroll', scrollHandler, { passive: true })
    updateSticky()
  }

  function detachScroll() {
    if (scrollHandler && scrollEl) {
      scrollEl.removeEventListener('scroll', scrollHandler)
    }
    scrollHandler = null
    if (rafId) {
      cancelAnimationFrame(rafId)
      rafId = null
    }
  }

  function invalidateCache() {
    lineEls = []
  }

  /**
   * Initialize sticky scroll for a file.
   * @param filePath - file path for backend API
   * @param el - the scroll container (<pre class="raw-content-pre">)
   */
  function initSticky(filePath: string, el: HTMLElement) {
    detachScroll()
    symbols = []
    stickyLines.value = []
    invalidateCache()
    scrollEl = el

    if (!filePath || !el) return

    fetchCodeSymbols(filePath).then(result => {
      if (result && result.symbols.length > 0) {
        // Sort by line ascending
        symbols = [...result.symbols].sort((a, b) => a.line - b.line) as unknown as typeof symbols
        attachScroll()
      }
    }).catch(() => {})
  }

  function teardownSticky() {
    detachScroll()
    symbols = []
    stickyLines.value = []
    invalidateCache()
    scrollEl = null
  }

  onBeforeUnmount(() => {
    teardownSticky()
  })

  return {
    stickyLines,
    initSticky,
    teardownSticky,
    invalidateCache,
  }
}
