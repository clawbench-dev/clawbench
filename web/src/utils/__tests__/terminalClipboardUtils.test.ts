import { describe, expect, it } from 'vitest'
import {
  isCopySelectionShortcut,
  isPasteSelectionShortcut,
  type CopyKeyEvent,
} from '@/utils/terminalClipboardUtils'

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

describe('isPasteSelectionShortcut', () => {
  /** Shift+Insert keydown, the chord this predicate exists for. */
  const pasteChord = (partial: Partial<CopyKeyEvent> = {}) =>
    ev({ key: 'Insert', shiftKey: true, ...partial })

  it('claims Shift+Insert', () => {
    expect(isPasteSelectionShortcut(pasteChord())).toBe(true)
  })

  it('falls back to keyCode 45 when the browser reports no usable key', () => {
    // Some IMEs / older WebViews leave `key` empty for non-character keys; the
    // chord must still work there rather than silently doing nothing.
    expect(isPasteSelectionShortcut(pasteChord({ key: '', keyCode: 45 }))).toBe(true)
  })

  it('does not claim plain Insert (the PTY wants it as ESC[2~)', () => {
    expect(isPasteSelectionShortcut(ev({ key: 'Insert' }))).toBe(false)
    expect(isPasteSelectionShortcut(ev({ key: '', keyCode: 45 }))).toBe(false)
  })

  it('leaves Ctrl+Insert to the copy chord', () => {
    expect(isPasteSelectionShortcut(pasteChord({ ctrlKey: true }))).toBe(false)
  })

  it('leaves Alt/Meta combinations to their normal meaning', () => {
    expect(isPasteSelectionShortcut(pasteChord({ altKey: true }))).toBe(false)
    expect(isPasteSelectionShortcut(pasteChord({ metaKey: true }))).toBe(false)
  })

  it('only reacts to keydown, so a chord cannot paste three times', () => {
    expect(isPasteSelectionShortcut(pasteChord({ type: 'keypress' }))).toBe(false)
    expect(isPasteSelectionShortcut(pasteChord({ type: 'keyup' }))).toBe(false)
  })

  it('ignores other shifted keys', () => {
    expect(isPasteSelectionShortcut(pasteChord({ key: 'Delete' }))).toBe(false)
    expect(isPasteSelectionShortcut(pasteChord({ key: 'v' }))).toBe(false)
  })
})
