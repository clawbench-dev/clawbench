import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync, existsSync } from 'node:fs'
import { join, resolve } from 'node:path'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * Guard: every literal `t('a.b.c')` key must exist in BOTH locales.
 *
 * A missing key is not a silent degradation — vue-i18n renders the KEY ITSELF,
 * so the UI shows "chat.quote.stagedFromDrag" instead of a sentence. That is
 * exactly what shipped: the toast for dragging a quote into the chat pointed at
 * a key that was never added, and the user saw the raw key.
 *
 * Nothing else catches this class:
 *   - `vue-tsc` types `t()` as (key: string), so any string type-checks;
 *   - the key is a runtime string, so a bundle grep cannot prove it resolves;
 *   - mounting the component would render the key but no test asserts the
 *     rendered text is not a key.
 *
 * So the whole source tree is scanned here, and the assertion is against the
 * real locale objects — the same lookup vue-i18n performs.
 *
 * Only SINGLE-QUOTED literal keys are checked. Template literals
 * (`t(\`settings.groups.${id}\`)`) and variables cannot be resolved statically;
 * they are covered by the per-feature key tests (src/i18n/__tests__/*Keys.test.ts)
 * and by the enum maps those features iterate.
 */
describe('i18n literal keys', () => {
  /**
   * Resolve the frontend source root, tolerating either working directory the
   * test runner may use. A bare `vitest` from `web/` has cwd = web/, while the
   * official `npm test` → scripts/vitest-run.sh cds to the repo root; resolving
   * against a single assumed cwd passes locally and fails in CI with ENOENT.
   */
  function webSrcRoot(): string {
    for (const base of [process.cwd(), resolve(process.cwd(), 'web'), resolve(process.cwd(), '../web')]) {
      const candidate = resolve(base, 'src')
      if (existsSync(candidate)) return candidate
    }
    throw new Error(`web/src not found from cwd: ${process.cwd()}`)
  }

  /** Every .ts/.vue under src/, excluding tests (fixtures use arbitrary keys). */
  function sourceFiles(dir: string, out: string[] = []): string[] {
    for (const entry of readdirSync(dir)) {
      const p = join(dir, entry)
      if (statSync(p).isDirectory()) {
        if (entry !== '__tests__' && entry !== 'node_modules') sourceFiles(p, out)
      } else if (/\.(ts|vue)$/.test(entry) && !/\.test\.ts$/.test(entry)) {
        out.push(p)
      }
    }
    return out
  }

  function lookup(messages: unknown, path: string[]): unknown {
    return path.reduce<unknown>((acc, part) => (acc as Record<string, unknown> | undefined)?.[part], messages)
  }

  /**
   * A key is present when BOTH locales resolve it to a non-empty string or
   * array. Arrays are legitimate: weekday lists (`task.form.weekdays`,
   * `cron.weekdayNames`) are consumed as `string[]`. Anything else — undefined,
   * null, a nested object with no value — renders as the raw key.
   */
  function isPresent(value: unknown): boolean {
    if (typeof value === 'string') return value.length > 0
    if (Array.isArray(value)) return value.length > 0
    return false
  }

  it('resolves every literal t() key in both locales', () => {
    const root = webSrcRoot()
    const missing: string[] = []

    for (const file of sourceFiles(root)) {
      const src = readFileSync(file, 'utf8')
      // A dotted key, single-quoted: t('a.b.c') / $t('a.b.c').
      for (const m of src.matchAll(/\bt\(\s*'([a-zA-Z][a-zA-Z0-9_]*(?:\.[a-zA-Z0-9_]+)+)'/g)) {
        const key = m[1]
        const path = key.split('.')
        if (!isPresent(lookup(en, path)) || !isPresent(lookup(zh, path))) {
          missing.push(`${key}  (${file.replace(root + '/', '')})`)
        }
      }
    }

    expect(
      missing,
      `these t() keys are missing from a locale and would render as the raw key:\n${missing.join('\n')}`,
    ).toEqual([])
  })

  // The scan is only meaningful if it actually reads the tree and finds keys —
  // a broken glob would report "0 missing" forever and silently stop guarding.
  it('actually scans the tree (guard is not vacuous)', () => {
    const root = webSrcRoot()
    const files = sourceFiles(root)
    expect(files.length).toBeGreaterThan(100)

    const keysFound = files.reduce((n, f) => {
      const src = readFileSync(f, 'utf8')
      return n + [...src.matchAll(/\bt\(\s*'([a-zA-Z][a-zA-Z0-9_]*(?:\.[a-zA-Z0-9_]+)+)'/g)].length
    }, 0)
    expect(keysFound).toBeGreaterThan(500)
  })
})
