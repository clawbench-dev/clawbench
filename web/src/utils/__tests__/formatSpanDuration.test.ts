import { describe, expect, it, afterEach } from 'vitest'
import i18n from '@/i18n'
import { formatDuration, formatSpanDuration } from '@/utils/format'

/**
 * formatSpanDuration is the accumulated-span formatter used by the stats panel's
 * work-time metric. It is deliberately NOT formatDuration:
 *
 *   formatDuration  labels ONE reply in the compact chat meta bar, so it caps at
 *                   hours ("3.5h").
 *   formatSpanDuration labels a REPORT TOTAL summed over hundreds of turns, so
 *                   it must promote to days — a month of work time is ~720 h,
 *                   and "720.0 小时" is a number nobody can place.
 *
 * These tests run against the REAL locales (not a hand-written message table)
 * so the unit words themselves are pinned, and they flip the locale explicitly
 * because the unit is localized prose rather than a compact glyph.
 */

const original = i18n.global.locale.value

function inLocale(locale: 'zh' | 'en'): void {
  i18n.global.locale.value = locale
}

afterEach(() => {
  i18n.global.locale.value = original
})

describe('formatSpanDuration tiers', () => {
  it('uses days from one day up, with one decimal', () => {
    inLocale('zh')
    // 1.5 days — the case the user asked for: "超过一天就是一点几天".
    expect(formatSpanDuration(1.5 * 24 * 3600 * 1000)).toBe('1.5 天')
    expect(formatSpanDuration(30 * 24 * 3600 * 1000)).toBe('30.0 天')
    // Just under a day stays in hours; exactly one day promotes.
    expect(formatSpanDuration(24 * 3600 * 1000 - 1)).toBe('24.0 小时')
    expect(formatSpanDuration(24 * 3600 * 1000)).toBe('1.0 天')
  })

  it('uses hours from one hour up to a day, with one decimal', () => {
    inLocale('zh')
    expect(formatSpanDuration(3600 * 1000)).toBe('1.0 小时')
    expect(formatSpanDuration(3.5 * 3600 * 1000)).toBe('3.5 小时')
    expect(formatSpanDuration(23.9 * 3600 * 1000)).toBe('23.9 小时')
  })

  it('uses minutes below an hour and seconds below a minute', () => {
    inLocale('zh')
    expect(formatSpanDuration(60 * 1000)).toBe('1.0 分钟')
    expect(formatSpanDuration(5.3 * 60 * 1000)).toBe('5.3 分钟')
    expect(formatSpanDuration(59_900)).toBe('59.9 秒')
    expect(formatSpanDuration(1500)).toBe('1.5 秒')
  })

  it('keeps sub-second values in whole milliseconds', () => {
    inLocale('zh')
    expect(formatSpanDuration(0)).toBe('0 毫秒')
    expect(formatSpanDuration(420)).toBe('420 毫秒')
    expect(formatSpanDuration(999)).toBe('999 毫秒')
  })

  it('never shows more than one decimal', () => {
    inLocale('zh')
    // 1 h 1 min 1 s = 1.0169... h → one decimal only.
    expect(formatSpanDuration(3661 * 1000)).toBe('1.0 小时')
    // 1 day 1 h = 1.0416... d → one decimal only.
    expect(formatSpanDuration(25 * 3600 * 1000)).toBe('1.0 天')
    for (const ms of [1234, 90_000, 5_400_000, 90_000_000, 999_999_999]) {
      expect(formatSpanDuration(ms)).toMatch(/^\d+(\.\d)?\s?[^\d]+$/)
    }
  })

  it('localizes the unit words', () => {
    inLocale('en')
    expect(formatSpanDuration(1.5 * 24 * 3600 * 1000)).toBe('1.5 days')
    expect(formatSpanDuration(3.5 * 3600 * 1000)).toBe('3.5 hr')
    expect(formatSpanDuration(5.3 * 60 * 1000)).toBe('5.3 min')
    expect(formatSpanDuration(1500)).toBe('1.5 s')
    expect(formatSpanDuration(420)).toBe('420 ms')

    inLocale('zh')
    expect(formatSpanDuration(1.5 * 24 * 3600 * 1000)).toBe('1.5 天')
    expect(formatSpanDuration(3.5 * 3600 * 1000)).toBe('3.5 小时')
  })

  it('returns empty for negative or non-finite input', () => {
    inLocale('zh')
    expect(formatSpanDuration(-1)).toBe('')
    expect(formatSpanDuration(NaN)).toBe('')
    expect(formatSpanDuration(Infinity)).toBe('')
  })

  // The two formatters must stay distinct: collapsing them (or reusing
  // formatDuration for the report) is the regression this guards.
  it('does not degrade into the single-turn formatter', () => {
    inLocale('zh')
    const month = 30 * 24 * 3600 * 1000
    expect(formatDuration(month)).toBe('720.0h') // capped at hours by design
    expect(formatSpanDuration(month)).toBe('30.0 天')
    expect(formatSpanDuration(3.5 * 3600 * 1000)).not.toBe(formatDuration(3.5 * 3600 * 1000))
  })
})
