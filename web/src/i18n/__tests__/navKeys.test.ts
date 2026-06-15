import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * Ensures all nav keys exist in both en and zh locales.
 */
describe('i18n nav keys completeness', () => {
  const enNavKeys = Object.keys(en.nav)
  const zhNavKeys = Object.keys(zh.nav)

  it('en and zh have the same nav keys', () => {
    const enOnly = enNavKeys.filter(k => !zhNavKeys.includes(k))
    const zhOnly = zhNavKeys.filter(k => !enNavKeys.includes(k))
    expect(enOnly, 'keys only in en').toEqual([])
    expect(zhOnly, 'keys only in zh').toEqual([])
  })

  it('no nav values are empty strings', () => {
    for (const key of enNavKeys) {
      expect((en.nav as Record<string, string>)[key], `en.nav.${key} should not be empty`).not.toBe('')
    }
    for (const key of zhNavKeys) {
      expect((zh.nav as Record<string, string>)[key], `zh.nav.${key} should not be empty`).not.toBe('')
    }
  })
})
