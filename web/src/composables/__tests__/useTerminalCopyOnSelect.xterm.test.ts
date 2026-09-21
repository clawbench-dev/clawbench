import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { Terminal } from '@xterm/xterm'
import { AUTO_COPY_SETTLE_MS, useTerminalCopyOnSelect } from '@/composables/useTerminalCopyOnSelect'

/**
 * Integration guard for the design assumption behind copy-on-select: xterm's
 * `onSelectionChange` fires for programmatic `select()` / `clearSelection()`
 * (which our touch selection mode calls on every touchmove) but not during a
 * real mouse drag. If a future xterm release changed that, the settle delay in
 * useTerminalCopyOnSelect would stop collapsing drags into a single copy, and
 * the unit tests alone would not catch it — they feed the hook directly.
 */
describe('copy-on-select against a real xterm instance', () => {
  let term: Terminal
  let container: HTMLDivElement

  beforeEach(() => {
    vi.useFakeTimers()
    // jsdom implements neither matchMedia (xterm's CoreBrowserService reads it
    // for the DPR) nor canvas. xterm only needs them to construct/open; the
    // selection model we assert on is renderer-independent.
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }))
    container = document.createElement('div')
    document.body.appendChild(container)
    term = new Terminal({ cols: 40, rows: 10, allowProposedApi: true })
    term.open(container)
  })

  afterEach(() => {
    term.dispose()
    container.remove()
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  function attachCopyHook() {
    const copy = vi.fn()
    const hook = useTerminalCopyOnSelect({ isEnabled: () => true, copy })
    term.onSelectionChange(() => hook.onSelectionChanged(term.getSelection()))
    return { copy, hook }
  }

  /** xterm's public Terminal buffers writes behind setTimeout; flush them. */
  function writeText(text: string) {
    term.write(text)
    vi.advanceTimersByTime(50)
  }

  it('fires onSelectionChange for programmatic select() and clearSelection()', () => {
    const changes: string[] = []
    term.onSelectionChange(() => changes.push(term.getSelection()))

    writeText('hello world')
    term.select(0, 0, 5)
    expect(changes.length).toBe(1)
    expect(term.getSelection()).not.toBe('')

    term.clearSelection()
    expect(changes.length).toBe(2)
    expect(changes[changes.length - 1]).toBe('')
  })

  it('collapses a touch-selection drag (repeated select() calls) into one copy', () => {
    writeText('hello world')
    const { copy } = attachCopyHook()

    // Touch selection mode calls term.select() on every touchmove.
    term.select(0, 0, 1)
    vi.advanceTimersByTime(50)
    term.select(0, 0, 5)
    vi.advanceTimersByTime(50)
    term.select(0, 0, 11)
    vi.advanceTimersByTime(AUTO_COPY_SETTLE_MS)

    expect(copy).toHaveBeenCalledTimes(1)
    expect(copy).toHaveBeenCalledWith(term.getSelection())
  })

  it('does not copy when the selection is merely cleared', () => {
    writeText('hello')
    const { copy } = attachCopyHook()

    term.select(0, 0, 5)
    vi.advanceTimersByTime(AUTO_COPY_SETTLE_MS)
    copy.mockClear()

    term.clearSelection()
    vi.advanceTimersByTime(AUTO_COPY_SETTLE_MS * 2)
    // Clearing must never overwrite the clipboard with ''.
    expect(copy).not.toHaveBeenCalled()
  })

  it('does not re-copy when xterm suppresses a re-fire for an identical range', () => {
    writeText('hello')
    const { copy } = attachCopyHook()

    term.select(0, 0, 5)
    vi.advanceTimersByTime(AUTO_COPY_SETTLE_MS)
    expect(copy).toHaveBeenCalledTimes(1)

    // clearSelection() fires, but xterm's SelectionService does NOT reset its
    // `_oldSelectionStart`, so re-selecting the identical range is suppressed
    // by its own change-detection and never reaches our handler. That is safe
    // here: the clipboard already holds exactly this text, so a second copy
    // would be a no-op write.
    term.clearSelection()
    term.select(0, 0, 5)
    vi.advanceTimersByTime(AUTO_COPY_SETTLE_MS)
    expect(copy).toHaveBeenCalledTimes(1)
    expect(copy).toHaveBeenCalledWith('hello')
  })

  it('copies each distinct selection once, in selection order', () => {
    writeText('alpha beta')
    const { copy } = attachCopyHook()

    term.select(0, 0, 5)
    vi.advanceTimersByTime(AUTO_COPY_SETTLE_MS)
    // Re-selecting the identical range must not duplicate the copy (xterm does
    // not even re-fire for an unchanged range — this pins that behaviour).
    term.select(0, 0, 5)
    vi.advanceTimersByTime(AUTO_COPY_SETTLE_MS)
    expect(copy).toHaveBeenCalledTimes(1)

    term.select(6, 0, 4)
    vi.advanceTimersByTime(AUTO_COPY_SETTLE_MS)
    expect(copy).toHaveBeenCalledTimes(2)
    expect(copy.mock.calls[1][0]).toBe('beta')
  })
})
