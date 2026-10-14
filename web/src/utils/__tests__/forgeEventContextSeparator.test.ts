import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import { eventContextTemplateText } from '@/utils/forgeEventContextVars'

/**
 * The label/value separator in the read-only event-context block must be
 * localized, not a hardcoded fullwidth colon.
 *
 * The backend renders the SAME block for the prompt it injects
 * (EventContextSeparator resolves to ': ' in English, '：' in Chinese). A
 * literal '：' on this side made an English user's form preview differ from the
 * prompt the AI actually received.
 *
 * This lives in its own file because the sibling test mocks `@/i18n` to return
 * the raw key — which cannot show a separator's real value.
 */
describe('eventContextTemplateText separator', () => {
  function render(locale: 'en' | 'zh'): string {
    // createI18n here is only used to seed the module's global singleton; the
    // registry reads i18n.global at call time, so pointing it at the locale is
    // what matters. Importing the real singleton (not a mock) is the point.
    createI18n({ legacy: false, locale, messages: { en, zh } })
    return eventContextTemplateText({
      transitions: new Set(['pr.opened']),
      repoTargetedOnly: false,
    })
  }

  it('uses ": " and the English header for en', async () => {
    const { default: i18n } = await import('@/i18n')
    i18n.global.locale.value = 'en'
    const out = render('en')
    expect(out).toContain('## Forge event')
    expect(out).toContain(': {{EVENT_TYPE}}')
    expect(out).not.toContain('：')
  })

  it('uses "：" and the Chinese header for zh', async () => {
    const { default: i18n } = await import('@/i18n')
    i18n.global.locale.value = 'zh'
    const out = render('zh')
    expect(out).toContain('## Forge 事件')
    expect(out).toContain('：{{EVENT_TYPE}}')
  })
})
