import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { useToast } from '@/composables/useToast'

describe('useToast', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('returns visible, message, icon, type, onClick, show, dismiss', () => {
    const toast = useToast()
    expect(toast.visible).toBeDefined()
    expect(toast.message).toBeDefined()
    expect(toast.icon).toBeDefined()
    expect(toast.type).toBeDefined()
    expect(toast.onClick).toBeDefined()
    expect(typeof toast.show).toBe('function')
    expect(typeof toast.dismiss).toBe('function')
  })

  it('show() sets message, icon, type and makes visible', () => {
    const toast = useToast()
    toast.show('Hello world', { icon: '👋', type: 'info', duration: 0 })

    expect(toast.message.value).toBe('Hello world')
    expect(toast.icon.value).toBe('👋')
    expect(toast.type.value).toBe('info')
    expect(toast.visible.value).toBe(true)
  })

  it('show() with default options uses success type and empty icon', () => {
    const toast = useToast()
    toast.show('Saved', { duration: 0 })

    expect(toast.type.value).toBe('success')
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
    toast.show('Session forked (3/20)', { icon: '🔀', type: 'success', duration: 1500 })

    expect(toast.type.value).toBe('success')
    expect(toast.message.value).toBe('Session forked (3/20)')
  })

  it('dismiss() clears the auto-dismiss timer', () => {
    const toast = useToast()
    toast.show('Test', { duration: 5000 })
    toast.dismiss()

    // Advance past the original duration — timer should have been cleared
    vi.advanceTimersByTime(10000)
    // No late state change since timer was already cleared
    expect(toast.visible.value).toBe(false)
  })

  it('shared singleton state across multiple useToast() calls', () => {
    const toast1 = useToast()
    const toast2 = useToast()

    toast1.show('From instance 1', { duration: 0 })
    expect(toast2.message.value).toBe('From instance 1')
    expect(toast2.visible.value).toBe(true)
  })
})
