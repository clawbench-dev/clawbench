import { describe, expect, it } from 'vitest'
import { isCopySelectionShortcut, type CopyKeyEvent } from '@/utils/terminalClipboardUtils'

/** Build a keydown event with sensible defaults; override per case. */
function ev(partial: Partial<CopyKeyEvent> = {}): CopyKeyEvent {
  return {
    type: 'keydown',
    key: 'c',
    ctrlKey: false,
    metaKey: false,
    altKey: false,
    shiftKey: false,
    ...partial,
  }
}

describe('isCopySelectionShortcut', () => {
  describe('Windows / Linux (isMac = false)', () => {
    it('treats Ctrl+C with a selection as copy', () => {
      expect(isCopySelectionShortcut(ev({ ctrlKey: true }), true, false)).toBe(true)
    })

    it('treats Ctrl+Shift+C with a selection as copy', () => {
      expect(isCopySelectionShortcut(ev({ ctrlKey: true, shiftKey: true }), true, false)).toBe(true)
    })

    it('does NOT copy without a selection, so Ctrl+C can still send SIGINT', () => {
      expect(isCopySelectionShortcut(ev({ ctrlKey: true }), false, false)).toBe(false)
    })

    it('does not copy on Cmd+C (macOS chord on a non-mac platform)', () => {
      expect(isCopySelectionShortcut(ev({ metaKey: true }), true, false)).toBe(false)
    })

    it('ignores Ctrl+Alt+C (AltGr composes characters, must not copy)', () => {
      expect(isCopySelectionShortcut(ev({ ctrlKey: true, altKey: true }), true, false)).toBe(false)
    })

    it('ignores a bare "c" keystroke', () => {
      expect(isCopySelectionShortcut(ev(), true, false)).toBe(false)
    })
  })

  describe('macOS (isMac = true)', () => {
    it('treats Cmd+C with a selection as copy', () => {
      expect(isCopySelectionShortcut(ev({ metaKey: true }), true, true)).toBe(true)
    })

    it('treats Cmd+Shift+C with a selection as copy', () => {
      expect(isCopySelectionShortcut(ev({ metaKey: true, shiftKey: true }), true, true)).toBe(true)
    })

    it('leaves Ctrl+C to the PTY so macOS keeps its SIGINT binding', () => {
      expect(isCopySelectionShortcut(ev({ ctrlKey: true }), true, true)).toBe(false)
    })

    it('does not copy without a selection', () => {
      expect(isCopySelectionShortcut(ev({ metaKey: true }), false, true)).toBe(false)
    })
  })

  describe('event gating', () => {
    it('only reacts to keydown, so a chord cannot copy three times', () => {
      expect(isCopySelectionShortcut(ev({ type: 'keypress', ctrlKey: true }), true, false)).toBe(false)
      expect(isCopySelectionShortcut(ev({ type: 'keyup', ctrlKey: true }), true, false)).toBe(false)
    })

    it('matches the letter case-insensitively (Shift produces "C")', () => {
      expect(isCopySelectionShortcut(ev({ key: 'C', ctrlKey: true }), true, false)).toBe(true)
    })

    it('ignores unrelated letters with the primary modifier held', () => {
      expect(isCopySelectionShortcut(ev({ key: 'v', ctrlKey: true }), true, false)).toBe(false)
      expect(isCopySelectionShortcut(ev({ key: 'a', metaKey: true }), true, true)).toBe(false)
    })
  })
})
