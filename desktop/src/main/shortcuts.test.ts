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
    code: '',
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
  let onZoom: (action: 'in' | 'out' | 'reset') => void
  let hardReloadSpy: ReturnType<typeof vi.fn>
  let devToolsSpy: ReturnType<typeof vi.fn>
  let zoomSpy: ReturnType<typeof vi.fn>
  let handlers: ShortcutHandlers

  beforeEach(() => {
    hardReloadSpy = vi.fn()
    devToolsSpy = vi.fn()
    zoomSpy = vi.fn()
    onHardReload = hardReloadSpy as unknown as () => void
    onToggleDevTools = devToolsSpy as unknown as () => void
    onZoom = zoomSpy as unknown as (a: 'in' | 'out' | 'reset') => void
    handlers = { onHardReload, onToggleDevTools, onZoom }
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

  describe('page zoom', () => {
    it('claims Ctrl+= / Cmd+= to zoom in', () => {
      expect(handleShortcut(key({ code: 'Equal', key: '=', control: true }), false, handlers)).toBe(true)
      expect(zoomSpy).toHaveBeenCalledWith('in')
      expect(handleShortcut(key({ code: 'Equal', key: '=', meta: true }), true, handlers)).toBe(true)
      expect(zoomSpy).toHaveBeenCalledTimes(2)
    })

    it('claims Ctrl+- to zoom out', () => {
      expect(handleShortcut(key({ code: 'Minus', key: '-', control: true }), false, handlers)).toBe(true)
      expect(zoomSpy).toHaveBeenCalledWith('out')
    })

    it('claims Ctrl+0 to reset', () => {
      expect(handleShortcut(key({ code: 'Digit0', key: '0', control: true }), false, handlers)).toBe(true)
      expect(zoomSpy).toHaveBeenCalledWith('reset')
    })

    it('claims the numpad variants, which carry unrelated key names', () => {
      // Matched on `code` for exactly this reason: Numpad0 reports key
      // 'Insert' and NumpadAdd reports '+' only on some layouts, so a
      // key-based table would silently miss the numpad.
      expect(handleShortcut(key({ code: 'NumpadAdd', key: '+' , control: true }), false, handlers)).toBe(true)
      expect(zoomSpy).toHaveBeenLastCalledWith('in')
      expect(handleShortcut(key({ code: 'NumpadSubtract', key: '-', control: true }), false, handlers)).toBe(true)
      expect(zoomSpy).toHaveBeenLastCalledWith('out')
      expect(handleShortcut(key({ code: 'Numpad0', key: 'Insert', control: true }), false, handlers)).toBe(true)
      expect(zoomSpy).toHaveBeenLastCalledWith('reset')
    })

    it('leaves Ctrl+Shift+Minus to the page', () => {
      // Ctrl+Shift+Minus is Ctrl+_ (0x1f), which readline binds to undo. The
      // terminal must keep it, so only the unshifted chord zooms out.
      expect(handleShortcut(key({ code: 'Minus', key: '_', control: true, shift: true }), false, handlers)).toBe(false)
      expect(zoomSpy).not.toHaveBeenCalled()
    })

    it('leaves AltGr chords alone', () => {
      // AltGr is delivered as Ctrl+Alt on Windows. Without the alt guard,
      // typing AltGr+0 (a brace on many European layouts) would reset the zoom
      // instead of inserting a character.
      for (const k of [
        key({ code: 'Digit0', key: '0', control: true, alt: true }),
        key({ code: 'Equal', key: '=', control: true, alt: true }),
        key({ code: 'Minus', key: '-', control: true, alt: true }),
      ]) {
        expect(handleShortcut(k, false, handlers), JSON.stringify(k)).toBe(false)
      }
      expect(zoomSpy).not.toHaveBeenCalled()
    })

    it('ignores the chords without a modifier', () => {
      for (const k of [
        key({ code: 'Equal', key: '=' }),
        key({ code: 'Minus', key: '-' }),
        key({ code: 'Digit0', key: '0' }),
      ]) {
        expect(handleShortcut(k, false, handlers), JSON.stringify(k)).toBe(false)
      }
      expect(zoomSpy).not.toHaveBeenCalled()
    })

    it('does not zoom on keyUp', () => {
      // Auto-repeat/hold must not keep stepping the zoom.
      expect(
        handleShortcut(key({ code: 'Equal', key: '=', control: true, type: 'keyUp' }), false, handlers),
      ).toBe(false)
      expect(zoomSpy).not.toHaveBeenCalled()
    })
  })

  describe('keys that must fall through to the page', () => {
    it('leaves a bare F5 to the renderer', () => {
      // F5 must reach the page, which decides what it means: the terminal
      // forwards it to the TUI, the file manager refreshes its listing, and
      // otherwise the renderer reloads (useF5Reload). Claiming it in the main
      // process would suppress the page's keydown entirely and break all three.
      expect(handleShortcut(key({ key: 'F5' }), false, handlers)).toBe(false)
      expect(handleShortcut(key({ key: 'F5' }), true, handlers)).toBe(false)
      expect(hardReloadSpy).not.toHaveBeenCalled()
      expect(devToolsSpy).not.toHaveBeenCalled()
    })

    it('leaves Ctrl+R to the renderer as well', () => {
      // The file manager also refreshes on Ctrl+R. Only the Ctrl+Shift+R
      // variant (cache-clearing) belongs to the main process.
      expect(handleShortcut(key({ key: 'r', control: true }), false, handlers)).toBe(false)
      expect(hardReloadSpy).not.toHaveBeenCalled()
    })

    it('leaves other function keys to the renderer', () => {
      for (const k of ['F1', 'F2', 'F9', 'F11']) {
        expect(handleShortcut(key({ key: k }), false, handlers), k).toBe(false)
      }
      expect(devToolsSpy).not.toHaveBeenCalled()
    })

    it('leaves Ctrl+C/V/S and other Ctrl chords to the page', () => {
      // The zoom branch must not widen into a general "Ctrl + punctuation"
      // claim: these all belong to the renderer.
      for (const k of [
        key({ code: 'KeyC', key: 'c', control: true }),
        key({ code: 'KeyV', key: 'v', control: true }),
        key({ code: 'KeyS', key: 's', control: true }),
        key({ code: 'KeyF', key: 'f', control: true }),
        key({ code: 'Backquote', key: '`', control: true }),
      ]) {
        expect(handleShortcut(k, false, handlers), JSON.stringify(k)).toBe(false)
      }
      expect(zoomSpy).not.toHaveBeenCalled()
      expect(hardReloadSpy).not.toHaveBeenCalled()
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
