import { describe, expect, it } from 'vitest'
import {
  isSelectAllDeleteSignature,
  shouldRebuildInputOnSelectAllDelete,
} from '@/utils/dialogInputRecovery'

describe('isSelectAllDeleteSignature', () => {
  it('detects the Android signature: empty insert while text is selected', () => {
    // Captured from a real device: deleting the whole selection makes the
    // WebView IME emit an empty insert instead of a delete.
    expect(isSelectAllDeleteSignature({ inputType: 'insertText', data: '' }, 6)).toBe(true)
  })

  it('treats missing data as empty (WebView may omit it)', () => {
    expect(isSelectAllDeleteSignature({ inputType: 'insertText', data: null }, 6)).toBe(true)
  })

  it('ignores the desktop deletes, which carry no data', () => {
    // Verified in a real browser: desktop Chromium fires these instead.
    expect(isSelectAllDeleteSignature({ inputType: 'deleteContentBackward', data: null }, 6)).toBe(false)
    expect(isSelectAllDeleteSignature({ inputType: 'deleteContentForward', data: null }, 6)).toBe(false)
  })

  it('ignores typing a real character over a selection', () => {
    expect(isSelectAllDeleteSignature({ inputType: 'insertText', data: 'x' }, 6)).toBe(false)
  })

  it('ignores an empty insert with a collapsed caret (nothing to delete)', () => {
    expect(isSelectAllDeleteSignature({ inputType: 'insertText', data: '' }, 0)).toBe(false)
  })

  it('matches a partial selection, not just a select-all', () => {
    // The failure mode is "an empty insert replaced a selection", so any
    // non-collapsed selection qualifies — verified in a real browser, where a
    // selection of chars 2-5 emits the same insertText + data:"" event.
    expect(isSelectAllDeleteSignature({ inputType: 'insertText', data: '' }, 3)).toBe(true)
  })

  it('ignores an empty insert that is part of a composition', () => {
    // Defensive: composition events arrive as insertCompositionText, so the
    // inputType check already rejects them (verified in a real browser). This
    // guards an IME emitting a bare insertText at a composition boundary.
    expect(isSelectAllDeleteSignature({ inputType: 'insertText', data: '', isComposing: true }, 6)).toBe(false)
  })
})

describe('shouldRebuildInputOnSelectAllDelete', () => {
  it('rebuilds on Android when the signature matched', () => {
    expect(shouldRebuildInputOnSelectAllDelete(true, true)).toBe(true)
  })

  it('never rebuilds on non-Android, even when the signature matched', () => {
    // The rebuild costs a focus round-trip and the bug does not exist on
    // desktop, so PC behaviour must stay exactly as it is today.
    expect(shouldRebuildInputOnSelectAllDelete(false, true)).toBe(false)
  })

  it('does not rebuild on Android when the signature did not match', () => {
    expect(shouldRebuildInputOnSelectAllDelete(true, false)).toBe(false)
  })
})
