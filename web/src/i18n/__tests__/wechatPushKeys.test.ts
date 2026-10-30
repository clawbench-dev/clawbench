import { describe, it, expect } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * Guard: the WeChat push copy exists in BOTH locales.
 *
 * These keys are reached only from the data-driven push panel
 * (`settingsFieldMap`) and from `WeChatLoginPanel`, whose keys are looked up at
 * render time. The generic `literalKeys` scan only resolves single-quoted
 * `t('…')` calls in `.ts`/`.vue`, so a key missing from one locale would render
 * as the raw key path inside the panel — which is exactly what a user sees on
 * the screen they use to log in.
 */
const KEYS = [
  'pushModeWechat',
  'wechatLoginDesc',
  'wechatScanLogin',
  'wechatScanHint',
  'wechatNotLoggedIn',
  'wechatLoggedIn',
  'wechatBotId',
  'wechatLogout',
  'wechatRelogin',
  'wechatCancel',
  'wechatRetry',
  'wechatQRRefresh',
  'wechatQRRenderFailed',
  'wechatStatusFailed',
  'wechatStartFailed',
  'wechatLogoutFailed',
  'wechatStatePending',
  'wechatStateScanned',
  'wechatStateConnected',
  'wechatStateRunning',
  'wechatStateExpired',
  'wechatStateFailed',
  'wechatStateCancelled',
  'wechatLoading',
  'wechatConnectionExpired',
  'wechatConnectionExpiredHint',
  'wechatPollGaveUp',
  'wechatRiskWarning',
] as const

describe('WeChat push settings copy', () => {
  it.each(KEYS)('settings.items.%s exists in both locales', (key) => {
    const enItems = en.settings.items as Record<string, unknown>
    const zhItems = zh.settings.items as Record<string, unknown>
    expect(enItems[key], `en missing settings.items.${key}`).toBeTypeOf('string')
    expect(zhItems[key], `zh missing settings.items.${key}`).toBeTypeOf('string')
    expect(enItems[key]).not.toBe('')
    expect(zhItems[key]).not.toBe('')
  })

  // The mode option label must name the channel, or the dropdown row would be
  // indistinguishable from "native push" for a Chinese user.
  it('names the channel in the push-mode option', () => {
    expect(en.settings.items.pushModeWechat.toLowerCase()).toContain('wechat')
    expect(zh.settings.items.pushModeWechat).toContain('微信')
  })

  // The risk warning is the ONLY place the user is told this is an unofficial
  // interface on a personal account. Dropping it silently removes informed
  // consent, so the assertion is on the substance rather than the exact words.
  it('the risk warning still says the interface is unofficial', () => {
    expect(en.settings.items.wechatRiskWarning.toLowerCase()).toMatch(/unofficial/)
    expect(zh.settings.items.wechatRiskWarning).toMatch(/非官方/)
  })

  // Every login state the panel can render must have a distinct label: two
  // identical strings would make "scanned, confirm on your phone" read the same
  // as "waiting for scan", which is the one moment the user must act.
  it('distinguishes the pending and scanned states', () => {
    for (const items of [en.settings.items, zh.settings.items]) {
      expect(items.wechatStatePending).not.toBe(items.wechatStateScanned)
      expect(items.wechatStateExpired).not.toBe(items.wechatStateFailed)
    }
  })
})
