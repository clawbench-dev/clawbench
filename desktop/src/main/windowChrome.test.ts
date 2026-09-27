import { describe, it, expect } from 'vitest'
import { shouldUseFramelessWindow } from './windowChrome'

/**
 * The frameless decision is a platform table, and getting it wrong is not
 * cosmetic: a frameless window has no native controls, so if the app-drawn
 * cluster does not appear the user cannot close, minimize or move the window.
 * Hence the assertions on both the supported and the unsupported platforms.
 */
describe('shouldUseFramelessWindow', () => {
  it('is frameless on Windows and Linux, where the controls are top-right', () => {
    expect(shouldUseFramelessWindow('win32')).toBe(true)
    expect(shouldUseFramelessWindow('linux')).toBe(true)
  })

  it('keeps the native frame on macOS, whose traffic lights are top-left', () => {
    // The app-drawn cluster would sit top-right, mirroring the macOS
    // convention, and would also duplicate the system traffic lights.
    expect(shouldUseFramelessWindow('darwin')).toBe(false)
  })

  it('keeps the native frame for anything unrecognised', () => {
    // Failing toward the native frame is the only safe default: an unknown
    // platform that got a frameless window without a working cluster would be
    // impossible to close.
    for (const p of ['freebsd', 'openbsd', 'sunos', 'aix', '', 'WIN32', 'Linux']) {
      expect(shouldUseFramelessWindow(p), `platform ${JSON.stringify(p)}`).toBe(false)
    }
  })
})
