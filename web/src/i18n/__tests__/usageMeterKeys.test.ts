import { describe, it, expect } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * Guard: the usage-meter settings copy exists in BOTH locales and still says
 * what it has to say.
 *
 * These keys are reached only through `settingsFieldMap`'s data-driven panel
 * (the label/description are looked up by key at render time), so the generic
 * `literalKeys` scan cannot see them — it only resolves single-quoted `t('…')`
 * calls, and a missing key would render as the raw key path
 * ("settings.items.usageMeterMetricDesc") inside the settings panel.
 *
 * The entry row is the only place a user is told that switching the usage type
 * clears their quota. That warning has already been lost once: the key existed
 * in both locales but nothing rendered it. So this pins both the presence of the
 * text AND the fact that it still carries the warning.
 */
const KEYS = [
  'usageMeterMetric',
  'usageMeterMetricDesc',
  'usageMeterMetricTokens',
  'usageMeterMetricCredit',
  'usageMeterMetricCost',
  'usageMeterCycleMode',
  'usageMeterCycleModeDesc',
  'usageMeterCycleModeMonthly',
  'usageMeterCycleModeFixed',
  'usageMeterAnchor',
  'usageMeterAnchorDesc',
  'usageMeterPeriod',
  'usageMeterPeriodDesc',
  'usageMeterQuota',
  'usageMeterQuotaDesc',
] as const

describe('usage meter settings copy', () => {
  it.each(KEYS)('settings.items.%s exists in both locales', (key) => {
    const enItems = en.settings.items as Record<string, unknown>
    const zhItems = zh.settings.items as Record<string, unknown>
    expect(enItems[key], `en missing settings.items.${key}`).toBeTypeOf('string')
    expect(zhItems[key], `zh missing settings.items.${key}`).toBeTypeOf('string')
    expect(enItems[key]).not.toBe('')
    expect(zhItems[key]).not.toBe('')
  })

  // "Usage type" describes the row's own subject (what is counted). Reusing the
  // category title ("Usage") made the row read as a repeat of the page heading.
  it('names the entry row "usage type" rather than reusing the category title', () => {
    expect(en.settings.items.usageMeterMetric).toBe('Usage type')
    expect(zh.settings.items.usageMeterMetric).toBe('用量类型')
    expect(en.settings.items.usageMeterMetric).not.toBe(en.settings.items.usageMeter)
    expect(zh.settings.items.usageMeterMetric).not.toBe(zh.settings.items.usageMeter)
  })

  // The description is the ONLY notice that switching the type discards the
  // quota. A rewrite that drops that clause silently removes the warning.
  it('the usage-type description still warns that switching clears the quota', () => {
    expect(en.settings.items.usageMeterMetricDesc).toMatch(/switch/i)
    expect(en.settings.items.usageMeterMetricDesc).toMatch(/clear/i)
    expect(en.settings.items.usageMeterMetricDesc).toMatch(/quota/i)

    expect(zh.settings.items.usageMeterMetricDesc).toMatch(/切换/)
    expect(zh.settings.items.usageMeterMetricDesc).toMatch(/清空/)
    expect(zh.settings.items.usageMeterMetricDesc).toMatch(/额度/)
  })

  // Every description must actually describe something; a label accidentally
  // reused as the description ships a row that says its own name twice.
  it('descriptions differ from their labels', () => {
    const pairs = [
      ['usageMeterMetric', 'usageMeterMetricDesc'],
      ['usageMeterCycleMode', 'usageMeterCycleModeDesc'],
      ['usageMeterAnchor', 'usageMeterAnchorDesc'],
      ['usageMeterPeriod', 'usageMeterPeriodDesc'],
      ['usageMeterQuota', 'usageMeterQuotaDesc'],
    ] as const
    for (const [label, desc] of pairs) {
      const enItems = en.settings.items as Record<string, string>
      const zhItems = zh.settings.items as Record<string, string>
      expect(enItems[desc], `en ${desc} must not repeat ${label}`).not.toBe(enItems[label])
      expect(zhItems[desc], `zh ${desc} must not repeat ${label}`).not.toBe(zhItems[label])
    }
  })

  // The two cycle types are the whole point of the field, and the option labels
  // must be distinguishable — an identical pair would render a dropdown with two
  // indistinguishable rows.
  it('the two cycle types have distinct labels', () => {
    expect(en.settings.items.usageMeterCycleModeMonthly).not.toBe(en.settings.items.usageMeterCycleModeFixed)
    expect(zh.settings.items.usageMeterCycleModeMonthly).not.toBe(zh.settings.items.usageMeterCycleModeFixed)
  })

  // "Calendar month" is not obvious from a day count: the restart day comes
  // from the anchor's day-of-month. The start-time description is the only place
  // that can explain it, because the day-of-month is edited there.
  it('the start-time description explains the day-of-month rule for calendar months', () => {
    expect(en.settings.items.usageMeterAnchorDesc).toMatch(/day/i)
    expect(zh.settings.items.usageMeterAnchorDesc).toMatch(/日号|号/)
  })

  // The placeholder entry is the only thing an unconfigured user sees, and its
  // label carries the entire message ("set up usage alerts"). A missing or
  // echo-of-the-key value would leave a bare feature name with no action.
  it('the placeholder hint exists in both locales', () => {
    expect(en.usageMeter.setupHint).toBeTruthy()
    expect(zh.usageMeter.setupHint).toBeTruthy()
  })

  it('the placeholder hint names the action, not just the feature', () => {
    const enItems = en.usageMeter as Record<string, string>
    const zhItems = zh.usageMeter as Record<string, string>

    expect(enItems.setupHint).toBeTruthy()
    expect(zhItems.setupHint).toBeTruthy()
    // Must differ from the plain feature label, or the placeholder would read as
    // a disabled "Usage" row rather than an invitation.
    expect(enItems.setupHint).not.toBe(enItems.label)
    expect(zhItems.setupHint).not.toBe(zhItems.label)
    expect(zhItems.setupHint).toContain('设置')
  })
})
