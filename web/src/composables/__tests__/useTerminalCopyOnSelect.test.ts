import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import {
  AUTO_COPY_SETTLE_MS,
  selectionFingerprint,
  shouldAutoCopySelection,
  useTerminalCopyOnSelect,
} from '@/composables/useTerminalCopyOnSelect'

describe('selectionFingerprint', () => {
  it('ignores CR and surrounding whitespace so equivalent selections match', () => {
    expect(selectionFingerprint('foo\r\nbar\r')).toBe(selectionFingerprint('foo\nbar'))
    expect(selectionFingerprint('  ls -la  ')).toBe(selectionFingerprint('ls -la'))
  })

  it('keeps internal differences', () => {
    expect(selectionFingerprint('a b')).not.toBe(selectionFingerprint('ab'))
  })
})

describe('shouldAutoCopySelection', () => {
  it('copies a non-empty selection when enabled', () => {
    expect(shouldAutoCopySelection(true, 'hello', '')).toBe(true)
  })

  it('never copies an empty selection (clearing must not wipe the clipboard)', () => {
    expect(shouldAutoCopySelection(true, '', '')).toBe(false)
  })

  it('rejects an empty selection even when the last copy was non-empty', () => {
    // This is the case the fingerprint comparison alone would wave through:
    // '' differs from 'foo', so without the explicit empty check the predicate
    // would report "yes, copy" and the caller would write '' to the clipboard.
    expect(shouldAutoCopySelection(true, '', selectionFingerprint('foo'))).toBe(false)
  })

  it('never copies while the setting is off', () => {
    expect(shouldAutoCopySelection(false, 'hello', '')).toBe(false)
  })

  it('skips a selection whose normalized form was already copied', () => {
    expect(shouldAutoCopySelection(true, 'foo\r\n', selectionFingerprint('foo'))).toBe(false)
  })

  it('copies again once the selection differs', () => {
    expect(shouldAutoCopySelection(true, 'bar', selectionFingerprint('foo'))).toBe(true)
  })
})

describe('useTerminalCopyOnSelect', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  function setup(enabled = true) {
    const copy = vi.fn()
    const hook = useTerminalCopyOnSelect({ isEnabled: () => enabled, copy })
    return { copy, hook }
  }

  it('copies once after the selection settles', () => {
    const { copy, hook } = setup()
    hook.onSelectionChanged('hello')
    expect(copy).not.toHaveBeenCalled()
    vi.advanceTimersByTime(AUTO_COPY_SETTLE_MS)
    expect(copy).toHaveBeenCalledTimes(1)
    expect(copy).toHaveBeenCalledWith('hello')
  })

  it('collapses a drag (many intermediate selections) into one copy', () => {
    const { copy, hook } = setup()
    // Touch selection mode re-selects on every touchmove; each fires a change.
    hook.onSelectionChanged('l')
    vi.advanceTimersByTime(50)
    hook.onSelectionChanged('ls -')
    vi.advanceTimersByTime(50)
    hook.onSelectionChanged('ls -la')
    vi.advanceTimersByTime(AUTO_COPY_SETTLE_MS)
    expect(copy).toHaveBeenCalledTimes(1)
    expect(copy).toHaveBeenCalledWith('ls -la')
  })

  it('does not copy the same text twice in a row', () => {
    const { copy, hook } = setup()
    hook.onSelectionChanged('same')
    vi.advanceTimersByTime(AUTO_COPY_SETTLE_MS)
    hook.onSelectionChanged('same')
    vi.advanceTimersByTime(AUTO_COPY_SETTLE_MS)
    expect(copy).toHaveBeenCalledTimes(1)
  })

  it('copies the same text again after the selection was cleared', () => {
    const { copy, hook } = setup()
    hook.onSelectionChanged('same')
    vi.advanceTimersByTime(AUTO_COPY_SETTLE_MS)
    hook.onSelectionChanged('')
    hook.onSelectionChanged('same')
    vi.advanceTimersByTime(AUTO_COPY_SETTLE_MS)
    expect(copy).toHaveBeenCalledTimes(2)
  })

  it('drops a pending copy on dispose (tab switch)', () => {
    const { copy, hook } = setup()
    hook.onSelectionChanged('pending')
    hook.dispose()
    vi.advanceTimersByTime(AUTO_COPY_SETTLE_MS * 4)
    expect(copy).not.toHaveBeenCalled()
  })

  it('respects the setting being turned off mid-selection', () => {
    const copy = vi.fn()
    let enabled = true
    const hook = useTerminalCopyOnSelect({ isEnabled: () => enabled, copy })
    enabled = false
    hook.onSelectionChanged('nope')
    vi.advanceTimersByTime(AUTO_COPY_SETTLE_MS * 2)
    expect(copy).not.toHaveBeenCalled()
  })

  it('honours a custom settle delay', () => {
    const copy = vi.fn()
    const hook = useTerminalCopyOnSelect({ isEnabled: () => true, copy, delayMs: 10 })
    hook.onSelectionChanged('x')
    vi.advanceTimersByTime(9)
    expect(copy).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(copy).toHaveBeenCalledTimes(1)
  })
})
