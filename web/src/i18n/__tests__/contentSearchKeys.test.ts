import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * Guards the content-search dialog's i18n keys.
 *
 * The dialog renders every one of these keys, so a key present on only one side
 * (or left empty) would show a raw key or a blank label in that locale. Mirrors
 * the other key-completeness suites (navKeys / terminalKeys / fontKeys).
 */
describe('i18n content-search keys completeness', () => {
  const enKeys = Object.keys((en.file as Record<string, unknown>).contentSearch as object)
  const zhKeys = Object.keys((zh.file as Record<string, unknown>).contentSearch as object)

  it('en and zh have the same contentSearch keys', () => {
    const enOnly = enKeys.filter(k => !zhKeys.includes(k))
    const zhOnly = zhKeys.filter(k => !enKeys.includes(k))
    expect(enOnly, 'keys only in en').toEqual([])
    expect(zhOnly, 'keys only in zh').toEqual([])
  })

  it('every contentSearch key the dialog uses is defined', () => {
    // Spelled out rather than derived from the component so adding a new
    // t('file.contentSearch.X') without a translation fails here.
    const used = [
      'title', 'scopeCurrent', 'scopeRecursive', 'scopeProject',
      'placeholder', 'button', 'caseSensitive', 'wholeWord', 'regex',
      'filters', 'includeLabel', 'includePlaceholder', 'excludeLabel',
      'excludePlaceholder', 'hint', 'noResultsHint', 'summary', 'summaryPlus',
      'summaryStopped', 'stop', 'stoppedEmpty', 'fileTruncated',
    ]
    const enCs = (en.file as Record<string, Record<string, string>>).contentSearch
    const zhCs = (zh.file as Record<string, Record<string, string>>).contentSearch
    for (const key of used) {
      expect(enCs[key], `en.file.contentSearch.${key} missing`).toBeTruthy()
      expect(zhCs[key], `zh.file.contentSearch.${key} missing`).toBeTruthy()
    }
  })

  it('no contentSearch value is an empty string', () => {
    const enCs = (en.file as Record<string, Record<string, string>>).contentSearch
    const zhCs = (zh.file as Record<string, Record<string, string>>).contentSearch
    for (const key of enKeys) {
      expect(enCs[key], `en.file.contentSearch.${key} should not be empty`).not.toBe('')
    }
    for (const key of zhKeys) {
      expect(zhCs[key], `zh.file.contentSearch.${key} should not be empty`).not.toBe('')
    }
  })

  it('placeholder-bearing keys keep their interpolation placeholders in both locales', () => {
    const enCs = (en.file as Record<string, Record<string, string>>).contentSearch
    const zhCs = (zh.file as Record<string, Record<string, string>>).contentSearch
    for (const key of ['summary', 'summaryPlus', 'summaryStopped', 'fileTruncated']) {
      expect(enCs[key], `en ${key} placeholders`).toMatch(/\{\w+\}/)
      expect(zhCs[key], `zh ${key} placeholders`).toMatch(/\{\w+\}/)
    }
  })
})
