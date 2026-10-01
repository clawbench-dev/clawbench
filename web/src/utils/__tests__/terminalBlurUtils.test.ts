import { describe, expect, it } from 'vitest'
import { shouldAutoRefocusTerminal, shouldInstallTerminalBlurRefocus } from '@/utils/terminalBlurUtils'

const body = () => ({ tagName: 'BODY' }) as unknown as Element
const button = () => ({ tagName: 'BUTTON' }) as unknown as Element
const input = () => ({ tagName: 'INPUT' }) as unknown as Element
const textarea = () => ({ tagName: 'TEXTAREA' }) as unknown as Element

describe('shouldAutoRefocusTerminal', () => {
  it('re-focuses when the terminal is active and focus falls back to body (tap on terminal surface)', () => {
    expect(shouldAutoRefocusTerminal(true, null)).toBe(true)
    expect(shouldAutoRefocusTerminal(true, body())).toBe(true)
  })

  it('does not re-focus when the terminal panel is inactive', () => {
    expect(shouldAutoRefocusTerminal(false, null)).toBe(false)
    expect(shouldAutoRefocusTerminal(false, body())).toBe(false)
  })

  it('does not steal focus from a real control (toolbar/dock button, input)', () => {
    expect(shouldAutoRefocusTerminal(true, button())).toBe(false)
    expect(shouldAutoRefocusTerminal(true, input())).toBe(false)
  })

  it('does not steal focus from a textarea (e.g. command editor inside a modal)', () => {
    expect(shouldAutoRefocusTerminal(true, textarea())).toBe(false)
  })
})

describe('shouldInstallTerminalBlurRefocus', () => {
  it('installs the blur-refocus workaround in the Android WebView host', () => {
    // The quirk is Android's soft keyboard, so the argument is the HOST axis —
    // not "is this a desktop?". Electron is also a native host yet must not
    // install it, which is exactly why the parameter is named isAndroidApp.
    expect(shouldInstallTerminalBlurRefocus(true)).toBe(true)
  })

  it('does NOT install the workaround outside the Android WebView', () => {
    // Electron and the browser must not reclaim focus from the chat input in the
    // wide-screen split layout.
    expect(shouldInstallTerminalBlurRefocus(false)).toBe(false)
  })
})
