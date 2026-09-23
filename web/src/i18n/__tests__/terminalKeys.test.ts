import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * Ensures all terminal keys exist in both en and zh locales.
 */
describe('i18n terminal keys completeness', () => {
  const enTerminalKeys = Object.keys(en.terminal)
  const zhTerminalKeys = Object.keys(zh.terminal)

  it('en and zh have the same terminal keys', () => {
    const enOnly = enTerminalKeys.filter(k => !zhTerminalKeys.includes(k))
    const zhOnly = zhTerminalKeys.filter(k => !enTerminalKeys.includes(k))
    expect(enOnly, 'keys only in en').toEqual([])
    expect(zhOnly, 'keys only in zh').toEqual([])
  })

  it('no terminal values are empty strings', () => {
    for (const key of enTerminalKeys) {
      expect((en.terminal as Record<string, string>)[key], `en.terminal.${key} should not be empty`).not.toBe('')
    }
    for (const key of zhTerminalKeys) {
      expect((zh.terminal as Record<string, string>)[key], `zh.terminal.${key} should not be empty`).not.toBe('')
    }
  })

  it('keeps the keys the terminal panel renders', () => {
    // Each of these is referenced by name from TerminalPanelContent.vue. They are
    // asserted individually because a locale edit that ADDS a key next to one of
    // these can silently REPLACE it — the parity check above still passes (both
    // locales lose it together), but the UI starts showing the raw key string.
    const referenced = [
      'tabLimitReached', // new-tab button tooltip when the session cap is hit
      'newTab',
      'openCurrentDir',
      'cwdUnavailable',
      'copyPath',
    ]
    for (const key of referenced) {
      expect(zhTerminalKeys, `zh.terminal.${key} missing`).toContain(key)
      expect(enTerminalKeys, `en.terminal.${key} missing`).toContain(key)
    }
  })

  it('has the back-to-terminal navigation label in both locales', () => {
    // surfaceLabel('terminal') resolves this; without it the return banner falls
    // back to the generic "Back".
    expect(en.file.nav.backToTerminal).toBeTruthy()
    expect(zh.file.nav.backToTerminal).toBeTruthy()
  })
})
