import { describe, expect, it } from 'vitest'
import { shouldAutoRefocusTerminal } from '@/utils/terminalBlurUtils'

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
