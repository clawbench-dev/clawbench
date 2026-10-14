import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * Keys for the click-time model switch feedback.
 *
 * A missing key renders the raw key string — and these land in a toast the user
 * sees the moment they pick a model, so a raw `chat.modelSwitcher.switching`
 * would be the most visible possible failure. vue-tsc and the build both ignore
 * it, so parity is asserted here instead of trusted.
 */
describe('model switcher keys', () => {
  const KEYS = ['switching', 'deferred', 'switchFailed', 'switchFailedDetail'] as const

  it('defines every model-switcher key in both locales', () => {
    for (const key of KEYS) {
      expect(en.chat.modelSwitcher[key], `en missing chat.modelSwitcher.${key}`).toBeTypeOf('string')
      expect(zh.chat.modelSwitcher[key], `zh missing chat.modelSwitcher.${key}`).toBeTypeOf('string')
      expect(en.chat.modelSwitcher[key].length, `en chat.modelSwitcher.${key} is empty`).toBeGreaterThan(0)
      expect(zh.chat.modelSwitcher[key].length, `zh chat.modelSwitcher.${key} is empty`).toBeGreaterThan(0)
    }
  })

  it('keeps the detail placeholder in both locales', () => {
    // The detail carries the agent's own explanation (e.g. which provider to
    // connect); losing `{detail}` degrades the toast to a generic failure.
    expect(en.chat.modelSwitcher.switchFailedDetail).toContain('{detail}')
    expect(zh.chat.modelSwitcher.switchFailedDetail).toContain('{detail}')
  })

  it('distinguishes "switching" from "deferred" in both locales', () => {
    // "switching" is a loading state; "deferred" says the switch will happen on
    // the next message. An identical label would misreport which one it is.
    expect(en.chat.modelSwitcher.switching).not.toBe(en.chat.modelSwitcher.deferred)
    expect(zh.chat.modelSwitcher.switching).not.toBe(zh.chat.modelSwitcher.deferred)
  })
})
