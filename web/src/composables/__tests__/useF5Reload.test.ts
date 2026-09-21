import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { useF5Reload } from '../useF5Reload'

/**
 * F5 must reload the page ONLY when nothing else claimed the key.
 *
 * Two existing consumers keep F5:
 *   - the terminal forwards it to the TUI (xterm, on its own container)
 *   - the file manager refreshes its listing (document level, when browse is active)
 *
 * The load-bearing detail is that the decision is DEFERRED: a document-level
 * listener registered before the file manager's would otherwise read
 * `defaultPrevented === false` and reload anyway. These tests pin that down.
 */

const Host = defineComponent({
  setup() {
    useF5Reload()
    return () => null
  },
})

function f5(over: Partial<KeyboardEventInit> = {}): KeyboardEvent {
  return new KeyboardEvent('keydown', { key: 'F5', bubbles: true, cancelable: true, ...over })
}

describe('useF5Reload', () => {
  let reload: ReturnType<typeof vi.fn>

  beforeEach(() => {
    vi.useFakeTimers()
    // jsdom's location.reload is not implemented; replace it outright.
    reload = vi.fn()
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { ...window.location, reload },
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('reloads when no one claimed F5', () => {
    const w = mount(Host)
    document.dispatchEvent(f5())
    vi.runAllTimers()
    expect(reload).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('does not reload when a listener already preventDefault-ed it', () => {
    // Stands in for the file manager: document level, and it claims the key.
    const claim = (e: KeyboardEvent) => { e.preventDefault() }
    document.addEventListener('keydown', claim)

    const w = mount(Host)
    document.dispatchEvent(f5())
    vi.runAllTimers()

    expect(reload).not.toHaveBeenCalled()
    document.removeEventListener('keydown', claim)
    w.unmount()
  })

  it('does not reload when the claim comes from a DIFFERENT target', () => {
    // Stands in for xterm: the terminal's handler is on its container, so the
    // event bubbles up to document already marked as handled.
    const container = document.createElement('div')
    document.body.appendChild(container)
    const claim = (e: KeyboardEvent) => { e.preventDefault() }
    container.addEventListener('keydown', claim)

    const w = mount(Host)
    container.dispatchEvent(f5())
    vi.runAllTimers()

    expect(reload).not.toHaveBeenCalled()
    container.removeEventListener('keydown', claim)
    container.remove()
    w.unmount()
  })

  it('still sees a claim registered AFTER it (the ordering hazard)', () => {
    // The regression this guards: the composable registers on mount, so a
    // consumer that registers later (or a lazily-created terminal) would be
    // invisible to a synchronous defaultPrevented read.
    const w = mount(Host)
    const late = (e: KeyboardEvent) => { e.preventDefault() }
    document.addEventListener('keydown', late)

    document.dispatchEvent(f5())
    vi.runAllTimers()

    expect(reload).not.toHaveBeenCalled()
    document.removeEventListener('keydown', late)
    w.unmount()
  })

  it('ignores F5 with modifiers so app shortcuts keep working', () => {
    const w = mount(Host)
    for (const mod of [{ ctrlKey: true }, { metaKey: true }, { altKey: true }, { shiftKey: true }]) {
      document.dispatchEvent(f5(mod))
    }
    vi.runAllTimers()
    expect(reload).not.toHaveBeenCalled()
    w.unmount()
  })

  it('ignores other keys', () => {
    const w = mount(Host)
    for (const key of ['F4', 'F6', 'r', 'Escape', 'Enter']) {
      document.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }))
    }
    vi.runAllTimers()
    expect(reload).not.toHaveBeenCalled()
    w.unmount()
  })

  it('does not reload after unmount', () => {
    const w = mount(Host)
    w.unmount()
    document.dispatchEvent(f5())
    vi.runAllTimers()
    expect(reload).not.toHaveBeenCalled()
  })
})
