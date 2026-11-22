import { describe, expect, it } from 'vitest'
import { isTerminalSurfaceTarget, shouldAutoRefocusTerminal, shouldInstallTerminalBlurRefocus } from '@/utils/terminalBlurUtils'

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

  it('does not re-focus while actively scrolling or when an overlay is open (#9)', () => {
    expect(shouldAutoRefocusTerminal(true, null, { isScrolling: true })).toBe(false)
    expect(shouldAutoRefocusTerminal(true, body(), { hasOverlayOpen: true })).toBe(false)
    expect(shouldAutoRefocusTerminal(true, body(), { isScrolling: false, hasOverlayOpen: false })).toBe(true)
  })
})

describe('isTerminalSurfaceTarget', () => {
  it('accepts xterm surface descendants and xterm helper textarea inside the container', () => {
    const container = document.createElement('div')
    const screen = document.createElement('div')
    screen.className = 'xterm-screen'
    const helperTa = document.createElement('textarea')
    helperTa.className = 'xterm-helper-textarea'
    container.appendChild(screen)
    container.appendChild(helperTa)

    expect(isTerminalSurfaceTarget(container, container)).toBe(true)
    expect(isTerminalSurfaceTarget(screen, container)).toBe(true)
    expect(isTerminalSurfaceTarget(helperTa, container)).toBe(true)
  })

  it('rejects elements outside the terminal container or upper-layer overlays/controls', () => {
    const container = document.createElement('div')
    const errorOverlay = document.createElement('div')
    errorOverlay.className = 'terminal-error-overlay'
    const reconnectBtn = document.createElement('button')
    errorOverlay.appendChild(reconnectBtn)
    container.appendChild(errorOverlay)

    const outsideOverlay = document.createElement('div')
    outsideOverlay.className = 'bottom-sheet-overlay'

    expect(isTerminalSurfaceTarget(errorOverlay, container)).toBe(false)
    expect(isTerminalSurfaceTarget(reconnectBtn, container)).toBe(false)
    expect(isTerminalSurfaceTarget(outsideOverlay, container)).toBe(false)
    expect(isTerminalSurfaceTarget(null, container)).toBe(false)
    expect(isTerminalSurfaceTarget(container, null)).toBe(false)
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
