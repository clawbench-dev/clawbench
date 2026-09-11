import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { LongPressDirective } from '../longPress'

// jsdom has no Touch/TouchEvent constructor, so build a plain Event and stub
// `touches` on it — the directive only reads e.touches[0].{clientX,clientY}.
function makeTouch(type: string, x: number, y: number): Event {
  const ev = new Event(type, { bubbles: true, cancelable: true })
  Object.defineProperty(ev, 'touches', { value: [{ clientX: x, clientY: y }] })
  return ev
}

describe('LongPressDirective', () => {
  // Fake only the timer APIs the directive actually uses. The default
  // vi.useFakeTimers() also fakes queueMicrotask/Date/performance, which can
  // stall Vue's scheduler and leave the fork worker unable to exit — this repo
  // already fights fork-worker hangs (vitest-dev/vitest#8766). Always restore
  // real timers, even if an assertion throws mid-test.
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  })
  afterEach(() => {
    vi.clearAllTimers()
    vi.useRealTimers()
  })

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
    const el = document.createElement('div')

    // First callback — simulates stale v-for closure (wrong session)
    const staleCallback = vi.fn()
    const staleBinding = { value: staleCallback } as any
    LongPressDirective.mounted(el, staleBinding)

    // Simulate Vue re-render updating the binding — new callback with correct session
    const freshCallback = vi.fn()
    const freshBinding = { value: freshCallback } as any
    LongPressDirective.updated!(el, freshBinding)

    // Trigger the long-press callback path directly
    ;(el as any)._longPress_binding.value('fake-event')

    // The fresh callback should be called, not the stale one
    expect(freshCallback).toHaveBeenCalledTimes(1)
    expect(staleCallback).not.toHaveBeenCalled()

    LongPressDirective.unmounted(el)
  })

  it('passes the data-session-id captured at touchstart to the callback', () => {
    const el = document.createElement('div')
    el.setAttribute('data-session-id', 's42')
    const callback = vi.fn()
    LongPressDirective.mounted(el, { value: callback } as any)

    const ev = makeTouch('touchstart', 10, 20)
    el.dispatchEvent(ev)
    vi.advanceTimersByTime(500)

    // The callback receives (event, capturedSessionId) — the id captured at
    // touchstart, which survives DOM reordering before the long-press fires.
    expect(callback).toHaveBeenCalledTimes(1)
    expect(callback).toHaveBeenCalledWith(ev, 's42')

    LongPressDirective.unmounted(el)
  })

  it('captures data-path (for non-session rows) as the identity hint', () => {
    const el = document.createElement('div')
    el.setAttribute('data-path', '/a/b.txt')
    const callback = vi.fn()
    LongPressDirective.mounted(el, { value: callback } as any)

    const ev = makeTouch('touchstart', 1, 2)
    el.dispatchEvent(ev)
    vi.advanceTimersByTime(500)

    expect(callback).toHaveBeenCalledWith(ev, '/a/b.txt')

    LongPressDirective.unmounted(el)
  })

  it('passes empty string when neither data-session-id nor data-path is present', () => {
    const el = document.createElement('div')
    const callback = vi.fn()
    LongPressDirective.mounted(el, { value: callback } as any)

    el.dispatchEvent(makeTouch('touchstart', 5, 5))
    vi.advanceTimersByTime(500)

    expect(callback).toHaveBeenCalledWith(expect.anything(), '')

    LongPressDirective.unmounted(el)
  })

  it('adds the long-pressing class while active and removes it on touchend', () => {
    const el = document.createElement('div')
    LongPressDirective.mounted(el, { value: vi.fn() } as any)

    el.dispatchEvent(makeTouch('touchstart', 0, 0))
    expect(el.classList.contains('long-pressing')).toBe(false)

    vi.advanceTimersByTime(500)
    expect(el.classList.contains('long-pressing')).toBe(true)

    el.dispatchEvent(makeTouch('touchend', 0, 0))
    expect(el.classList.contains('long-pressing')).toBe(false)

    LongPressDirective.unmounted(el)
  })

  it('cancels the pending long-press when the finger moves beyond the threshold', () => {
    const el = document.createElement('div')
    const callback = vi.fn()
    LongPressDirective.mounted(el, { value: callback } as any)

    el.dispatchEvent(makeTouch('touchstart', 10, 20))
    // Move 30px horizontally — beyond MOVE_THRESHOLD_PX (10)
    el.dispatchEvent(makeTouch('touchmove', 40, 20))
    vi.advanceTimersByTime(500)

    expect(callback).not.toHaveBeenCalled()
    expect(el.classList.contains('long-pressing')).toBe(false)

    LongPressDirective.unmounted(el)
  })

  it('keeps the long-press alive when movement stays within the threshold', () => {
    const el = document.createElement('div')
    const callback = vi.fn()
    LongPressDirective.mounted(el, { value: callback } as any)

    el.dispatchEvent(makeTouch('touchstart', 10, 20))
    // Move only 3px — within threshold, so the gesture continues
    el.dispatchEvent(makeTouch('touchmove', 13, 22))
    vi.advanceTimersByTime(500)

    expect(callback).toHaveBeenCalledTimes(1)

    LongPressDirective.unmounted(el)
  })

  it('ignores touchmove when no long-press is pending', () => {
    const el = document.createElement('div')
    const callback = vi.fn()
    LongPressDirective.mounted(el, { value: callback } as any)

    // No touchstart first — handler must early-return without throwing
    expect(() => el.dispatchEvent(makeTouch('touchmove', 99, 99))).not.toThrow()
    expect(callback).not.toHaveBeenCalled()

    LongPressDirective.unmounted(el)
  })

  it('treats a short tap as a normal click and does not preventDefault', () => {
    const el = document.createElement('div')
    const callback = vi.fn()
    LongPressDirective.mounted(el, { value: callback } as any)

    el.dispatchEvent(makeTouch('touchstart', 0, 0))
    vi.advanceTimersByTime(200) // release before 450ms

    const end = makeTouch('touchend', 0, 0)
    el.dispatchEvent(end)

    expect(callback).not.toHaveBeenCalled()
    // Synthetic click must be allowed to fire on a short tap
    expect(end.defaultPrevented).toBe(false)
    // Advancing past the threshold must not fire a stale timer
    vi.advanceTimersByTime(500)
    expect(callback).not.toHaveBeenCalled()

    LongPressDirective.unmounted(el)
  })

  it('prevents the synthetic click after a long-press fires', () => {
    const el = document.createElement('div')
    const callback = vi.fn()
    LongPressDirective.mounted(el, { value: callback } as any)

    el.dispatchEvent(makeTouch('touchstart', 0, 0))
    vi.advanceTimersByTime(500)
    expect(callback).toHaveBeenCalledTimes(1)

    const end = makeTouch('touchend', 0, 0)
    el.dispatchEvent(end)

    expect(end.defaultPrevented).toBe(true)

    LongPressDirective.unmounted(el)
  })

  it('cancels the pending long-press on touchcancel', () => {
    const el = document.createElement('div')
    const callback = vi.fn()
    LongPressDirective.mounted(el, { value: callback } as any)

    el.dispatchEvent(makeTouch('touchstart', 0, 0))
    vi.advanceTimersByTime(200)
    el.dispatchEvent(makeTouch('touchcancel', 0, 0))
    vi.advanceTimersByTime(500)

    expect(callback).not.toHaveBeenCalled()
    expect(el.classList.contains('long-pressing')).toBe(false)

    LongPressDirective.unmounted(el)
  })

  it('blocks the iOS contextmenu callout once a long-press has fired', () => {
    const el = document.createElement('div')
    LongPressDirective.mounted(el, { value: vi.fn() } as any)

    el.dispatchEvent(makeTouch('touchstart', 0, 0))
    vi.advanceTimersByTime(500)

    const menu = new Event('contextmenu', { bubbles: true, cancelable: true })
    el.dispatchEvent(menu)
    expect(menu.defaultPrevented).toBe(true)

    LongPressDirective.unmounted(el)
  })

  it('allows the native contextmenu when no long-press has fired', () => {
    const el = document.createElement('div')
    LongPressDirective.mounted(el, { value: vi.fn() } as any)

    const menu = new Event('contextmenu', { bubbles: true, cancelable: true })
    el.dispatchEvent(menu)

    // Long-press never fired, so the system callout must not be suppressed
    expect(menu.defaultPrevented).toBe(false)

    LongPressDirective.unmounted(el)
  })

  it('clears a pending timer on unmounted', () => {
    const el = document.createElement('div')
    const callback = vi.fn()
    LongPressDirective.mounted(el, { value: callback } as any)

    el.dispatchEvent(makeTouch('touchstart', 0, 0))
    // Unmount mid-gesture, before the 450ms threshold
    LongPressDirective.unmounted(el)
    vi.advanceTimersByTime(500)

    expect(callback).not.toHaveBeenCalled()
  })
})
