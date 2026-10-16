import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { useToast } from '@/composables/useToast'

describe('useToast', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('returns visible, message, icon, type, onClick, show, dismiss, pause, resume', () => {
    const toast = useToast()
    expect(toast.visible).toBeDefined()
    expect(toast.message).toBeDefined()
    expect(toast.icon).toBeDefined()
    expect(toast.type).toBeDefined()
    expect(toast.onClick).toBeDefined()
    expect(typeof toast.show).toBe('function')
    expect(typeof toast.dismiss).toBe('function')
    expect(typeof toast.pause).toBe('function')
    expect(typeof toast.resume).toBe('function')
  })

  it('show() sets message, icon, type and makes visible', () => {
    const toast = useToast()
    toast.show('Hello world', { icon: 'hand', type: 'info', duration: 0 })

    expect(toast.message.value).toBe('Hello world')
    expect(toast.icon.value).toBe('hand')
    expect(toast.type.value).toBe('info')
    expect(toast.visible.value).toBe(true)
  })

  it('show() with default options uses success type and empty icon key', () => {
    const toast = useToast()
    toast.show('Saved', { duration: 0 })

    expect(toast.type.value).toBe('success')
    // Empty key is fine: the render layer falls back to the type's default
    // glyph, so the toast still shows an icon.
    expect(toast.icon.value).toBe('')
    expect(toast.message.value).toBe('Saved')
  })

  it('show() auto-dismisses after duration', () => {
    const toast = useToast()
    toast.show('Auto-dismiss', { duration: 3000 })

    expect(toast.visible.value).toBe(true)

    vi.advanceTimersByTime(2999)
    expect(toast.visible.value).toBe(true)

    vi.advanceTimersByTime(1)
    expect(toast.visible.value).toBe(false)
  })

  it('show() with duration=0 does not auto-dismiss', () => {
    const toast = useToast()
    toast.show('Manual only', { duration: 0 })

    vi.advanceTimersByTime(60000)
    expect(toast.visible.value).toBe(true)
  })

  it('dismiss() hides the toast', () => {
    const toast = useToast()
    toast.show('Hello', { duration: 0 })
    expect(toast.visible.value).toBe(true)

    toast.dismiss()
    expect(toast.visible.value).toBe(false)
  })

  it('show() while visible replaces the message', () => {
    const toast = useToast()
    toast.show('First', { duration: 5000 })
    expect(toast.message.value).toBe('First')

    toast.show('Second', { duration: 5000 })
    expect(toast.message.value).toBe('Second')
    expect(toast.visible.value).toBe(true)
  })

  it('show() while visible resets the timer', () => {
    const toast = useToast()
    toast.show('First', { duration: 4000 })

    vi.advanceTimersByTime(3000)
    // Replace before first toast auto-dismisses
    toast.show('Second', { duration: 4000 })

    vi.advanceTimersByTime(3000)
    // Should still be visible — new timer hasn't expired yet
    expect(toast.visible.value).toBe(true)

    vi.advanceTimersByTime(1000)
    expect(toast.visible.value).toBe(false)
  })

  it('show() sets onClick callback', () => {
    const toast = useToast()
    const cb = vi.fn()
    toast.show('Clickable', { onClick: cb, duration: 0 })

    expect(toast.onClick.value).toBe(cb)
  })

  it('show() with error type', () => {
    const toast = useToast()
    toast.show('Error occurred', { type: 'error', duration: 0 })
    expect(toast.type.value).toBe('error')
  })

  // 'loading' is the in-flight type: the notification renders a spinner instead
  // of the icon and callers pair it with duration: 0 so it stays until a real
  // result toast replaces it (fork / ACP sync).
  it('show() with loading type', () => {
    const toast = useToast()
    toast.show('Forking session… 3s', { type: 'loading', duration: 0 })
    expect(toast.type.value).toBe('loading')
    expect(toast.visible.value).toBe(true)
  })

  it('a result toast replaces a sticky loading toast', () => {
    const toast = useToast()
    toast.show('Forking session… 5s', { type: 'loading', duration: 0 })
    // The caller's own success toast overwrites the sticky one — no manual
    // dismiss needed, and the loading toast must not linger alongside it.
    toast.show('Session forked (3/20)', { icon: 'fork', type: 'success', duration: 1500 })

    expect(toast.type.value).toBe('success')
    expect(toast.message.value).toBe('Session forked (3/20)')
  })

  it('dismiss() clears the auto-dismiss timer', () => {
    const toast = useToast()
    toast.show('Test', { duration: 5000 })
    toast.dismiss()

    // Advance past the original duration — timer should have been cleared
    vi.advanceTimersByTime(10000)
    expect(toast.visible.value).toBe(false)
  })

  it('shared singleton state across multiple useToast() calls', () => {
    const toast1 = useToast()
    const toast2 = useToast()

    toast1.show('From instance 1', { duration: 0 })
    expect(toast2.message.value).toBe('From instance 1')
    expect(toast2.visible.value).toBe(true)
  })

  // ── hover pause / resume ──
  // Hovering holds the toast open so the user can read and copy it.
  it('pause() holds the toast open past its duration, resume() finishes the remainder', () => {
    const toast = useToast()
    toast.show('Copy me', { duration: 4000 })

    vi.advanceTimersByTime(3000)
    toast.pause()

    // Time passes while hovering — the toast must stay up.
    vi.advanceTimersByTime(60000)
    expect(toast.visible.value).toBe(true)

    // Resume: only the 1000ms that was left should remain.
    toast.resume()
    vi.advanceTimersByTime(999)
    expect(toast.visible.value).toBe(true)
    vi.advanceTimersByTime(1)
    expect(toast.visible.value).toBe(false)
  })

  it('pause() is a no-op for a manual-only (duration: 0) toast', () => {
    const toast = useToast()
    toast.show('Manual', { duration: 0 })

    // No timer to suspend; resuming must not arm one either.
    toast.pause()
    toast.resume()
    vi.advanceTimersByTime(60000)
    expect(toast.visible.value).toBe(true)
  })

  it('repeated pause() does not double-count or lose the remaining time', () => {
    const toast = useToast()
    toast.show('Copy me', { duration: 4000 })

    vi.advanceTimersByTime(1000)
    toast.pause()
    vi.advanceTimersByTime(2000)
    toast.pause() // second pause while already paused must not reset remaining
    vi.advanceTimersByTime(2000)
    toast.resume()

    // 3000ms were left when the first pause happened.
    vi.advanceTimersByTime(2999)
    expect(toast.visible.value).toBe(true)
    vi.advanceTimersByTime(1)
    expect(toast.visible.value).toBe(false)
  })

  it('a new show() during a pause cancels the pause', () => {
    const toast = useToast()
    toast.show('First', { duration: 4000 })
    toast.pause()
    // Replacing the toast arms a fresh timer; a stale resume must not extend it.
    toast.show('Second', { duration: 2000 })
    toast.resume()

    vi.advanceTimersByTime(2000)
    expect(toast.visible.value).toBe(false)
  })
})
