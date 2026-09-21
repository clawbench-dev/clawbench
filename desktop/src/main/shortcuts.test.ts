import { describe, it, expect, vi, beforeEach } from 'vitest'
import { handleShortcut, type KeyInput, type ShortcutHandlers } from './shortcuts'

/**
 * `handleShortcut` decides which key events the app claims. The return value is
 * the contract that matters: true means the main process handled it and the
 * event is suppressed, false means it falls through to the renderer.
 *
 * Falling through is load-bearing: the terminal sends F5/F12 to the running TUI
 * and the file manager refreshes on F5, so claiming those would break the page.
 */

function key(over: Partial<KeyInput> = {}): KeyInput {
  return {
    type: 'keyDown',
    key: '',
    control: false,
    shift: false,
    alt: false,
    meta: false,
    ...over,
  }
}

describe('handleShortcut', () => {
  let onHardReload: () => void
  let onToggleDevTools: () => void
  let hardReloadSpy: ReturnType<typeof vi.fn>
  let devToolsSpy: ReturnType<typeof vi.fn>
  let handlers: ShortcutHandlers

  beforeEach(() => {
    hardReloadSpy = vi.fn()
    devToolsSpy = vi.fn()
    onHardReload = hardReloadSpy as unknown as () => void
    onToggleDevTools = devToolsSpy as unknown as () => void
    handlers = { onHardReload, onToggleDevTools }
  })

  describe('Ctrl+Shift+R hard reload', () => {
    it('claims Ctrl+Shift+R on Windows/Linux', () => {
      expect(handleShortcut(key({ key: 'r', control: true, shift: true }), false, handlers)).toBe(true)
      expect(hardReloadSpy).toHaveBeenCalledTimes(1)
    })

    it('claims Cmd+Shift+R on macOS', () => {
      expect(handleShortcut(key({ key: 'R', meta: true, shift: true }), true, handlers)).toBe(true)
      expect(hardReloadSpy).toHaveBeenCalledTimes(1)
    })

    it('ignores the chord without Shift', () => {
      // Ctrl+R alone is the browser reload convention and the page may use it.
      expect(handleShortcut(key({ key: 'r', control: true }), false, handlers)).toBe(false)
      expect(hardReloadSpy).not.toHaveBeenCalled()
    })

    it('ignores the chord without the platform modifier', () => {
      // Shift+R alone types a capital R.
      expect(handleShortcut(key({ key: 'R', shift: true }), false, handlers)).toBe(false)
      expect(hardReloadSpy).not.toHaveBeenCalled()
    })

    it('does not hard-reload on the macOS chord when running on Windows', () => {
      // meta is the Windows key there, not a command modifier.
      expect(handleShortcut(key({ key: 'r', meta: true, shift: true }), false, handlers)).toBe(false)
      expect(hardReloadSpy).not.toHaveBeenCalled()
    })
  })

  describe('DevTools', () => {
    it('claims F12 on Windows/Linux', () => {
      expect(handleShortcut(key({ key: 'F12' }), false, handlers)).toBe(true)
      expect(devToolsSpy).toHaveBeenCalledTimes(1)
      expect(hardReloadSpy).not.toHaveBeenCalled()
    })

    it('claims F12 on macOS too', () => {
      // F12 is not on every macOS keyboard, but if present it should work.
      expect(handleShortcut(key({ key: 'F12' }), true, handlers)).toBe(true)
      expect(devToolsSpy).toHaveBeenCalledTimes(1)
    })

    it('claims Ctrl+Shift+I on Windows/Linux', () => {
      expect(handleShortcut(key({ key: 'i', control: true, shift: true }), false, handlers)).toBe(true)
      expect(devToolsSpy).toHaveBeenCalledTimes(1)
    })

    it('claims Cmd+Option+I on macOS', () => {
      expect(handleShortcut(key({ key: 'i', meta: true, alt: true }), true, handlers)).toBe(true)
      expect(devToolsSpy).toHaveBeenCalledTimes(1)
    })

    it('ignores Ctrl+Shift+I on macOS (that chord is not the native one)', () => {
      expect(handleShortcut(key({ key: 'i', control: true, shift: true }), true, handlers)).toBe(false)
      expect(devToolsSpy).not.toHaveBeenCalled()
    })
  })

  describe('keys that must fall through to the page', () => {
    it('leaves a bare F5 to the renderer', () => {
      // The terminal sends F5 (ESC[15~) to the TUI and the file manager
      // refreshes its listing on F5. Claiming it here would break both.
      expect(handleShortcut(key({ key: 'F5' }), false, handlers)).toBe(false)
      expect(handleShortcut(key({ key: 'F5' }), true, handlers)).toBe(false)
      expect(hardReloadSpy).not.toHaveBeenCalled()
      expect(devToolsSpy).not.toHaveBeenCalled()
    })

    it('leaves other function keys to the renderer', () => {
      for (const k of ['F1', 'F2', 'F9', 'F11']) {
        expect(handleShortcut(key({ key: k }), false, handlers), k).toBe(false)
      }
      expect(devToolsSpy).not.toHaveBeenCalled()
    })

    it('leaves plain typing and other chords alone', () => {
      for (const k of [
        key({ key: 'a' }),
        key({ key: 'Enter' }),
        key({ key: 'Escape' }),
        key({ key: 'c', control: true }),
        key({ key: 'v', control: true }),
        key({ key: 's', control: true }),
        key({ key: 'R', shift: true }),
      ]) {
        expect(handleShortcut(k, false, handlers), JSON.stringify(k)).toBe(false)
      }
      expect(hardReloadSpy).not.toHaveBeenCalled()
      expect(devToolsSpy).not.toHaveBeenCalled()
    })

    it('ignores keyUp so a held key does not re-trigger', () => {
      // Auto-repeat would otherwise reload repeatedly or flap DevTools.
      expect(handleShortcut(key({ key: 'F12', type: 'keyUp' }), false, handlers)).toBe(false)
      expect(
        handleShortcut(key({ key: 'r', control: true, shift: true, type: 'keyUp' }), false, handlers),
      ).toBe(false)
      expect(hardReloadSpy).not.toHaveBeenCalled()
      expect(devToolsSpy).not.toHaveBeenCalled()
    })
  })
})
