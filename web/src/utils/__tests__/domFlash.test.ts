import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  LINE_FLASH_MS,
  REDUCED_FLASH_MS,
  LINE_FLASH_FALLBACK_PAD_MS,
  DEFAULT_FLASH_CLASS,
  flashElement,
  clearFlash,
} from '@/utils/domFlash'

const FULL_WAIT = LINE_FLASH_MS + LINE_FLASH_FALLBACK_PAD_MS
const REDUCED_WAIT = REDUCED_FLASH_MS + LINE_FLASH_FALLBACK_PAD_MS

function mountEl(className = 'target'): HTMLElement {
  const el = document.createElement('div')
  el.className = className
  document.body.appendChild(el)
  return el
}

describe('flashElement — fallback timer cleanup (jsdom has no CSS animation)', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('adds the flash class immediately and removes it after the fallback timer', () => {
    const el = mountEl()
    flashElement(el)
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(true)

    vi.advanceTimersByTime(FULL_WAIT - 1)
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(true)

    vi.advanceTimersByTime(1)
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(false)
  })

  it('supports a custom className', () => {
    const el = mountEl()
    flashElement(el, { className: 'chat-message-highlight' })
    expect(el.classList.contains('chat-message-highlight')).toBe(true)
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(false)

    vi.advanceTimersByTime(FULL_WAIT)
    expect(el.classList.contains('chat-message-highlight')).toBe(false)
  })

  it('uses the custom durationMs for the fallback window', () => {
    const el = mountEl()
    flashElement(el, { durationMs: 200 })
    vi.advanceTimersByTime(200 + LINE_FLASH_FALLBACK_PAD_MS - 1)
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(true)
    vi.advanceTimersByTime(1)
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(false)
  })

  it('runs onDone exactly once after removal', () => {
    const el = mountEl()
    const onDone = vi.fn()
    flashElement(el, { onDone })
    vi.advanceTimersByTime(FULL_WAIT)
    expect(onDone).toHaveBeenCalledTimes(1)
  })
})

describe('flashElement — animationend cleanup path', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('removes the class when animationend fires and cancels the fallback timer', () => {
    const el = mountEl()
    const onDone = vi.fn()
    flashElement(el, { onDone })
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(true)

    el.dispatchEvent(new Event('animationend'))

    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(false)
    expect(onDone).toHaveBeenCalledTimes(1)

    // The fallback timer must not run a second cleanup / onDone afterwards.
    vi.advanceTimersByTime(FULL_WAIT + 100)
    expect(onDone).toHaveBeenCalledTimes(1)
  })

  it('is idempotent when both animationend and the timer race', () => {
    const el = mountEl()
    const onDone = vi.fn()
    flashElement(el, { onDone })
    el.dispatchEvent(new Event('animationend'))
    vi.advanceTimersByTime(FULL_WAIT)
    expect(onDone).toHaveBeenCalledTimes(1)
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(false)
  })
})

describe('flashElement — restart on a pending flash', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('removes then re-adds the class when the same element is flashed again', async () => {
    const el = mountEl()

    // Watch the class attribute for remove + re-add between the two calls.
    const records: MutationRecord[] = []
    const observer = new MutationObserver((muts) => records.push(...muts))
    observer.observe(el, { attributes: true, attributeFilter: ['class'] })

    flashElement(el)
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(true)

    flashElement(el) // same element, still flashing
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(true)

    // Flush the mutation microtasks.
    await Promise.resolve()
    observer.disconnect()

    // Two class mutations happened (remove, then add) → a restart occurred
    // instead of a no-op add on an already-flashing element.
    const classMutations = records.filter((r) => r.attributeName === 'class')
    expect(classMutations.length).toBeGreaterThanOrEqual(2)

    // A single timer owns the cleanup: class is gone after one full window.
    vi.advanceTimersByTime(FULL_WAIT)
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(false)
  })

  it('supersedes the previous pending flash instead of double-cleaning', () => {
    const el = mountEl()
    const onDone = vi.fn()
    flashElement(el, { onDone })
    flashElement(el, { onDone })

    vi.advanceTimersByTime(FULL_WAIT)
    expect(onDone).toHaveBeenCalledTimes(1)
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(false)
  })
})

describe('flashElement — reduced motion', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('holds the class for the shorter reduced-motion window when injected', () => {
    const el = mountEl()
    flashElement(el, { prefersReducedMotion: () => true })

    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(true)

    // Still present at the reduced window boundary minus a tick.
    vi.advanceTimersByTime(REDUCED_WAIT - 1)
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(true)

    // Removed right at the boundary — well before the full normal window.
    vi.advanceTimersByTime(1)
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(false)
  })

  it('falls back to the module-level matchMedia probe by default', () => {
    const el = mountEl()
    flashElement(el)
    vi.advanceTimersByTime(REDUCED_WAIT)
    // No matchMedia stub → reduced-motion is false → the long window applies.
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(true)
    vi.advanceTimersByTime(FULL_WAIT - REDUCED_WAIT)
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(false)
  })
})

describe('flashElement — disconnected elements', () => {
  it('skips the class but still runs onDone', () => {
    const el = document.createElement('div') // never attached → isConnected false
    const onDone = vi.fn()
    flashElement(el, { onDone })
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(false)
    expect(onDone).toHaveBeenCalledTimes(1)
  })
})

describe('clearFlash', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('removes the class immediately and cancels the pending timer', () => {
    const el = mountEl()
    const onDone = vi.fn()
    flashElement(el, { onDone })
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(true)

    clearFlash(el)
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(false)

    // The cancelled timer must not fire later.
    vi.advanceTimersByTime(FULL_WAIT + 100)
    expect(onDone).not.toHaveBeenCalled()
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(false)
  })

  it('is a safe no-op when nothing is flashing', () => {
    const el = mountEl()
    expect(() => clearFlash(el)).not.toThrow()
    expect(el.classList.contains(DEFAULT_FLASH_CLASS)).toBe(false)
  })
})
