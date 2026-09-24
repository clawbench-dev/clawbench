import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import { QUOTE_TYPE_LABEL_KEY } from '@/utils/quoteSourceMeta'

/**
 * The quote type badge labels (file / git diff / CI run / task / terminal / …).
 *
 * These are what the user asked for on both the card and the drawer: a git icon
 * WITH a "PR" tag, a terminal icon WITH a "terminal" tag, and so on. A missing
 * key renders the raw key string in the UI, which is worse than a wrong word —
 * so parity between the two locales is asserted here rather than trusted.
 */
describe('quote type labels', () => {
  // Every type in the icon map must have a label in BOTH locales. Iterating the
  // map (rather than a hand-written list) is what makes a newly added type fail
  // here instead of shipping as a raw key.
  it('defines every type label in both locales', () => {
    for (const [type, key] of Object.entries(QUOTE_TYPE_LABEL_KEY)) {
      // Walk the FULL dotted key from the root (e.g. 'quoteBar.typeTask').
      const path = key.split('.')
      const lookup = (obj: Record<string, unknown>) =>
        path.reduce<unknown>((acc, part) => (acc as Record<string, unknown>)?.[part], obj)

      const enValue = lookup(en as unknown as Record<string, unknown>)
      const zhValue = lookup(zh as unknown as Record<string, unknown>)

      expect(enValue, `en missing label for quote type "${type}" (${key})`).toBeTypeOf('string')
      expect(zhValue, `zh missing label for quote type "${type}" (${key})`).toBeTypeOf('string')
      expect((enValue as string).length, `en label for "${type}" is empty`).toBeGreaterThan(0)
      expect((zhValue as string).length, `zh label for "${type}" is empty`).toBeGreaterThan(0)
    }
  })

  // The zh locale must not use the English "PR"/"issue" wording (see
  // forgeKeys.test.ts); it uses 合并请求 / 议题. Pinned here too so the type
  // labels cannot reintroduce the mixed-language wording this suite exists to
  // prevent.
  it('uses Chinese wording for PR and issue in the zh locale', () => {
    expect(zh.quoteBar.typePr).toBe('合并请求')
    expect(zh.quoteBar.typeIssue).toBe('议题')
  })

  it('labels a git diff, a CI run and a task distinctly', () => {
    // Three different sources that a user must be able to tell apart at a glance.
    const labels = [
      en.quoteBar.typeDiff,
      en.quoteBar.typePipeline,
      en.quoteBar.typeTask,
      en.quoteBar.typeTerminal,
    ]
    expect(new Set(labels).size).toBe(labels.length)
  })
})
