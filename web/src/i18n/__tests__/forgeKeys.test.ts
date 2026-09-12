import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * Guards for the forge (GitHub/GitLab) locale entries.
 *
 * Regression: the forge components were fully keyed with t() from the start,
 * but several zh VALUES were left as English ('Issues', 'Pull Requests',
 * 'Open'/'Closed'/'All'). Keying alone is not localization — the rendered text
 * stayed English for Chinese users. These assertions pin the zh values so a
 * future key-only addition cannot silently regress the same way.
 */
describe('forge locale values', () => {
  it('en and zh have the same forge keys', () => {
    const enKeys = Object.keys(en.forge)
    const zhKeys = Object.keys(zh.forge)
    expect(enKeys.filter(k => !zhKeys.includes(k)), 'keys only in en').toEqual([])
    expect(zhKeys.filter(k => !enKeys.includes(k)), 'keys only in zh').toEqual([])
  })

  it('translates the Issue / PR type labels in zh', () => {
    // These are the visible tab labels; leaving them English was the bug.
    expect(zh.forge.type.issues).not.toBe('Issues')
    expect(zh.forge.type.prs).not.toBe('Pull Requests')
    // Still recognizable as the standard Chinese-developer shorthand.
    expect(zh.forge.type.issues).toBe('Issue')
    expect(zh.forge.type.prs).toBe('Pull Request')
  })

  it('translates the state filter chips in zh', () => {
    expect(zh.forge.state.open).not.toBe('Open')
    expect(zh.forge.state.closed).not.toBe('Closed')
    expect(zh.forge.state.all).not.toBe('All')
    expect(zh.forge.state.open).toBe('开启')
    expect(zh.forge.state.closed).toBe('已关闭')
    expect(zh.forge.state.all).toBe('全部')
  })

  it('translates the dock nav label in zh', () => {
    expect(zh.nav.forge).not.toBe('Issues & PRs')
  })

  it('uses a consistent Issue/PR casing in zh search and empty text', () => {
    expect(zh.forge.searchPlaceholder).not.toContain('issue /')
    expect(zh.forge.emptyList).not.toContain('issue /')
  })

  it('provides the change/unbind switcher labels in both locales', () => {
    for (const [name, loc] of [['en', en], ['zh', zh]] as const) {
      expect(loc.forge.bind.change, `${name} bind.change`).toBeTruthy()
      expect(loc.forge.bind.unbind, `${name} bind.unbind`).toBeTruthy()
    }
  })

  it('no forge values are empty strings in either locale', () => {
    // Shallow check over the leaf string values of the namespace.
    const walk = (obj: Record<string, unknown>, prefix: string) => {
      for (const [k, v] of Object.entries(obj)) {
        if (typeof v === 'string') {
          expect(v, `${prefix}.${k} should not be empty`).not.toBe('')
        } else if (v && typeof v === 'object') {
          walk(v as Record<string, unknown>, `${prefix}.${k}`)
        }
      }
    }
    walk(en.forge, 'en.forge')
    walk(zh.forge, 'zh.forge')
  })
})
