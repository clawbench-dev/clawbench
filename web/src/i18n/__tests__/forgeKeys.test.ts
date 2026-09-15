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
    // The chosen Chinese terms, kept SHORT because they are tab labels. 合并
    // covers a GitHub PR and a GitLab MR.
    expect(zh.forge.type.issues).toBe('议题')
    expect(zh.forge.type.prs).toBe('合并')
  })

  it('translates the state filter chips in zh', () => {
    expect(zh.forge.state.open).not.toBe('Open')
    expect(zh.forge.state.closed).not.toBe('Closed')
    expect(zh.forge.state.all).not.toBe('All')
    expect(zh.forge.state.open).toBe('开启')
    expect(zh.forge.state.closed).toBe('已关闭')
    expect(zh.forge.state.all).toBe('全部')
    // "merged" is a first-class filter for change requests, not just a display
    // state: GitLab's state=closed excludes merged MRs, so the two must be
    // separately selectable for the platforms to agree.
    expect(zh.forge.state.merged).toBe('已合并')
  })

  it('translates the dock nav label in zh', () => {
    expect(zh.nav.forge).toBe('议题与合并请求')
  })

  it('leaves no English issue/PR wording anywhere in the zh locale', () => {
    // A mixed-language sentence ("新开 issue / PR") is exactly what we fixed.
    // Scans the WHOLE locale, not just the forge namespace: the same wording
    // also lives under task.form.* and settings.items.*, and a namespace-scoped
    // check silently missed those.
    const offenders: string[] = []
    const walk = (obj: Record<string, unknown>, prefix: string) => {
      for (const [k, v] of Object.entries(obj)) {
        if (typeof v === 'string' && /\b(issue|issues|PR|Pull Request)s?\b/i.test(v)) {
          offenders.push(`${prefix}.${k} = ${v}`)
        } else if (v && typeof v === 'object') {
          walk(v as Record<string, unknown>, `${prefix}.${k}`)
        }
      }
    }
    walk(zh as unknown as Record<string, unknown>, 'zh')
    expect(offenders, 'zh strings must not contain English issue/PR').toEqual([])
  })

  it('provides the change/unbind switcher labels in both locales', () => {
    for (const [name, loc] of [['en', en], ['zh', zh]] as const) {
      expect(loc.forge.bind.change, `${name} bind.change`).toBeTruthy()
      expect(loc.forge.bind.unbind, `${name} bind.unbind`).toBeTruthy()
    }
  })

  it('has matching activity filter and per-view empty-state keys in both locales', () => {
    // These are looked up DYNAMICALLY (`t('forge.overview.filter.' + f)`), so a
    // missing key would not fail a type check — it would render the raw key path
    // in the chip. The top-level key-parity check above cannot see this, because
    // it only compares the first level of the namespace.
    const filters = ['unread', 'read', 'all'] as const
    const views = ['unread', 'read', 'all'] as const
    for (const [name, loc] of [['en', en], ['zh', zh]] as const) {
      for (const f of filters) {
        expect(loc.forge.overview.filter[f], `${name} overview.filter.${f}`).toBeTruthy()
      }
      for (const v of views) {
        expect(loc.forge.overview.empty[v], `${name} overview.empty.${v}`).toBeTruthy()
        expect(loc.forge.overview.emptyHint[v], `${name} overview.emptyHint.${v}`).toBeTruthy()
      }
    }
  })

  it('distinguishes the unread empty state from the read one', () => {
    // "Nothing unread" and "nothing read yet" are different facts. If they read
    // the same, one of the two views is showing a lie.
    for (const [name, loc] of [['en', en], ['zh', zh]] as const) {
      expect(
        loc.forge.overview.empty.unread,
        `${name}: the unread and read empty states must differ`,
      ).not.toBe(loc.forge.overview.empty.read)
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
