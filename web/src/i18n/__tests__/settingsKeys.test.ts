import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * Guards the `settings.*` namespace against a key that exists on one side only.
 *
 * The UI reads translations through `t('settings.someKey')`, and vue-i18n
 * answers a missing key with the key path itself — so a gap shows up as
 * "settings.resetToDefault" rendered as a tooltip, which is easy to ship and
 * hard to notice. Chinese is the source of truth here (every key is authored
 * there first), so the assertion runs from zh to en.
 *
 * Scope is deliberately the settings namespace only: a repo-wide check would
 * need to model the whole fallback/pluralisation surface. `kbKeys.test.ts`
 * covers the knowledge-base namespace from the other direction (the keys the
 * components actually use).
 */
type Dict = Record<string, unknown>

function collect(node: Dict, prefix: string, out: Set<string>): void {
  for (const [key, value] of Object.entries(node)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      collect(value as Dict, path, out)
    } else {
      out.add(path)
    }
  }
}

function keysOf(locale: Dict): Set<string> {
  const out = new Set<string>()
  collect((locale.settings ?? {}) as Dict, 'settings', out)
  return out
}

describe('settings locale coverage', () => {
  it('has every zh settings key in en', () => {
    const enKeys = keysOf(en as unknown as Dict)
    const missing = [...keysOf(zh as unknown as Dict)].filter((k) => !enKeys.has(k)).sort()
    expect(missing, `en.ts is missing these settings keys:\n${missing.join('\n')}`).toEqual([])
  })

  it('has every en settings key in zh', () => {
    const zhKeys = keysOf(zh as unknown as Dict)
    const missing = [...keysOf(en as unknown as Dict)].filter((k) => !zhKeys.has(k)).sort()
    expect(missing, `zh.ts is missing these settings keys:\n${missing.join('\n')}`).toEqual([])
  })

  it('scans a namespace with real depth, so the check cannot pass vacuously', () => {
    // If the collector silently returned nothing (a refactor of the locale
    // shape), both assertions above would compare two empty sets and pass.
    const keys = keysOf(zh as unknown as Dict)
    expect(keys.size).toBeGreaterThan(200)
    expect(keys.has('settings.resetToDefault')).toBe(true)
    expect([...keys].some((k) => k.startsWith('settings.dockPlugins.'))).toBe(true)
  })
})
