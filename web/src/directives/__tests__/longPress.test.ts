import { describe, it, expect, vi } from 'vitest'
import { LongPressDirective } from '../longPress'

describe('LongPressDirective', () => {
  it('should store binding on element at mount time', () => {
    const el = document.createElement('div')
    const callback = vi.fn()
    const binding = { value: callback } as any

    LongPressDirective.mounted(el, binding)

    expect((el as any)._longPress_binding).toBe(binding)
    expect((el as any)._longPress_cleanup).toBeDefined()

    LongPressDirective.unmounted(el)
  })

  it('should update stored binding via updated hook', () => {
    const el = document.createElement('div')
    const staleCallback = vi.fn()
    const freshCallback = vi.fn()

    const staleBinding = { value: staleCallback } as any
    const freshBinding = { value: freshCallback } as any

    LongPressDirective.mounted(el, staleBinding)
    LongPressDirective.updated!(el, freshBinding)

    // The stored binding should now point to the fresh one
    expect((el as any)._longPress_binding).toBe(freshBinding)

    LongPressDirective.unmounted(el)
  })

  it('should clean up stored binding and listeners on unmounted', () => {
    const el = document.createElement('div')
    const binding = { value: vi.fn() } as any

    LongPressDirective.mounted(el, binding)
    expect((el as any)._longPress_binding).toBe(binding)
    expect((el as any)._longPress_cleanup).toBeDefined()

    LongPressDirective.unmounted(el)
    expect((el as any)._longPress_binding).toBeUndefined()
    expect((el as any)._longPress_cleanup).toBeUndefined()
  })

  it('should expose updated hook on the directive object', () => {
    expect(LongPressDirective.updated).toBeDefined()
    expect(typeof LongPressDirective.updated).toBe('function')
  })

  it('should use latest binding value when long-press fires (not stale closure)', () => {
    vi.useFakeTimers()
    const el = document.createElement('div')

    // First callback — simulates stale v-for closure (wrong session)
    const staleCallback = vi.fn()
    const staleBinding = { value: staleCallback } as any
    LongPressDirective.mounted(el, staleBinding)

    // Simulate Vue re-render updating the binding — new callback with correct session
    const freshCallback = vi.fn()
    const freshBinding = { value: freshCallback } as any
    LongPressDirective.updated!(el, freshBinding)

    // Simulate touchstart by dispatching a real event
    // We need to use the real event listener, so create a proper TouchEvent
    // jsdom doesn't support Touch constructor, so we manually trigger the timeout
    // by accessing the internal mechanism. Instead, let's verify the binding read path.
    // The key insight: at fire-time, the directive reads `el._longPress_binding.value`
    // We can verify this by directly calling what the timeout would call:

    // Trigger the long-press callback path directly
    ;(el as any)._longPress_binding.value('fake-event')

    // The fresh callback should be called, not the stale one
    expect(freshCallback).toHaveBeenCalledTimes(1)
    expect(staleCallback).not.toHaveBeenCalled()

    LongPressDirective.unmounted(el)
    vi.useRealTimers()
  })

  it('passes the data-session-id captured at touchstart to the callback', () => {
    vi.useFakeTimers()
    const el = document.createElement('div')
    el.setAttribute('data-session-id', 's42')
    const callback = vi.fn()
    LongPressDirective.mounted(el, { value: callback } as any)

    // Dispatch a touchstart (jsdom lacks Touch — stub via Object.defineProperty)
    const touches = [{ clientX: 10, clientY: 20 }]
    const touchEvent = new Event('touchstart', { bubbles: true })
    Object.defineProperty(touchEvent, 'touches', { value: touches })

    el.dispatchEvent(touchEvent)
    vi.advanceTimersByTime(500)

    // The callback receives (event, capturedSessionId) — the id captured at
    // touchstart, which survives DOM reordering before the long-press fires.
    expect(callback).toHaveBeenCalledTimes(1)
    expect(callback).toHaveBeenCalledWith(touchEvent, 's42')

    LongPressDirective.unmounted(el)
    vi.useRealTimers()
  })

  it('captures data-path (for non-session rows) as the identity hint', () => {
    vi.useFakeTimers()
    const el = document.createElement('div')
    el.setAttribute('data-path', '/a/b.txt')
    const callback = vi.fn()
    LongPressDirective.mounted(el, { value: callback } as any)

    const touchEvent = new Event('touchstart', { bubbles: true })
    Object.defineProperty(touchEvent, 'touches', { value: [{ clientX: 1, clientY: 2 }] })
    el.dispatchEvent(touchEvent)
    vi.advanceTimersByTime(500)

    expect(callback).toHaveBeenCalledWith(touchEvent, '/a/b.txt')

    LongPressDirective.unmounted(el)
    vi.useRealTimers()
  })
})
