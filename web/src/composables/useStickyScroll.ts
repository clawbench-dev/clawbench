import { ref, onBeforeUnmount } from 'vue'
import { fetchCodeSymbols } from '@/composables/useCodeSymbols'

/**
 * VS Code-style "Sticky Scroll" for code files.
 * When scrolling past a function/class definition, that definition line
 * sticks to the top of the code area so you always know which scope you're in.
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
  const stickyLines = ref([])

  let symbols = []
  let scrollEl = null
  let scrollHandler = null
  let rafId = null
  let lineEls = []  // cached .code-line elements

  const MAX_STICKY = 5  // max sticky lines to show at once

  function updateSticky() {
    if (!scrollEl || symbols.length === 0) {
      if (stickyLines.value.length > 0) stickyLines.value = []
      return
    }

    const scrollTop = scrollEl.scrollTop

    // Find the first visible line number
    let firstVisibleLine = -1
    if (lineEls.length === 0) {
      lineEls = Array.from(scrollEl.querySelectorAll(':scope > code > .code-line'))
      if (lineEls.length === 0) return
    }

    for (let i = 0; i < lineEls.length; i++) {
      if (lineEls[i].offsetTop >= scrollTop) {
        firstVisibleLine = i + 1
        break
      }
    }
    if (firstVisibleLine === -1) firstVisibleLine = lineEls.length

    // Find all enclosing scopes that contain the first visible line
    const enclosing = []
    for (const sym of symbols) {
      if (sym.line <= firstVisibleLine && sym.endLine >= firstVisibleLine) {
        enclosing.push(sym)
      }
    }

    // Sort by scope width descending (outermost first)
    enclosing.sort((a, b) => (b.endLine - b.line) - (a.endLine - a.line))

    // Only keep scopes whose definition line is scrolled out of view.
    // Compute actual pixel offsets and heights from DOM measurements.
    const result = []
    let accTop = 0
    for (let i = 0; i < enclosing.length && result.length < MAX_STICKY; i++) {
      const sym = enclosing[i]
      const defLineEl = lineEls[sym.line - 1]
      if (defLineEl && defLineEl.offsetTop < scrollTop) {
        const h = defLineEl.getBoundingClientRect().height
        result.push({
          lineNum: sym.line,
          kind: sym.kind,
          top: accTop,   // pixel offset from overlay top
          height: h,     // actual rendered height (supports wrapped lines)
        })
        accTop += h
      }
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
    codeTextEls.forEach(el => {
      el.style.transform = `translateX(${-scrollLeft}px)`
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
  function initSticky(filePath, el) {
    detachScroll()
    symbols = []
    stickyLines.value = []
    invalidateCache()
    scrollEl = el

    if (!filePath || !el) return

    fetchCodeSymbols(filePath).then(result => {
      if (result && result.symbols.length > 0) {
        // Sort by line ascending
        symbols = [...result.symbols].sort((a, b) => a.line - b.line)
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
