import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync, existsSync } from 'node:fs'
import { join, resolve } from 'node:path'

/**
 * Guard: durations and relative times have exactly ONE implementation each.
 *
 * Both had drifted into 3–4 near-identical copies (utils/format.ts,
 * utils/chatBlocks.ts, utils/contentBlocks.ts, AcpSessionDrawer.vue,
 * ForgePipelineDetail.vue), which is how a timestamp ends up reading
 * "5分钟前" in one panel and "5 min ago" in another, and how a duration ends up
 * as "1m30s" in one place and "1.5m" in another.
 *
 * A behavioural test cannot catch a recurrence: a new local helper works
 * perfectly in isolation, so nothing fails until the two surfaces are compared
 * side by side. Only the source can prove there is no second copy, so this
 * scans the tree.
 *
 * Deliberately NOT guarded: absolute-time helpers (forge's local `formatTime`
 * returning toLocaleString) are a different concern, and the streaming elapsed
 * counter (ContentBlocks' `elapsedLabel`) is a live ticker that needs
 * second-level precision — see the comment above it.
 */

function webSrcRoot(): string {
  for (const base of [process.cwd(), resolve(process.cwd(), 'web'), resolve(process.cwd(), '../web')]) {
    const candidate = resolve(base, 'src')
    if (existsSync(candidate)) return candidate
  }
  throw new Error(`web/src not found from cwd: ${process.cwd()}`)
}

/** Every .ts/.vue under src/, excluding tests (they legitimately pin literals). */
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

const CANONICAL = 'utils/format.ts'

describe('duration and relative-time formatters have a single source', () => {
  it('declares formatDuration only in utils/format.ts', () => {
    const root = webSrcRoot()
    const offenders: string[] = []
    for (const file of sourceFiles(root)) {
      const rel = file.replace(root + '/', '')
      if (rel === CANONICAL) continue
      const src = readFileSync(file, 'utf8')
      // A function or const declaration named formatDuration/formatJobDuration
      // that does the formatting itself (rather than delegating).
      if (!/\b(?:function|const)\s+format(?:Job)?Duration\s*[=(]/.test(src)) continue
      // A thin adapter that converts units and delegates is fine —
      // ForgePipelineDetail turns the API's seconds into ms and calls the
      // shared formatter. Requiring the IMPORT (not merely a call) is what
      // makes this honest: a local re-implementation whose body happens to
      // contain `formatDuration(` — including the declaration's own
      // parenthesis — must not pass.
      const importsShared = /import\s*\{[^}]*\bformatDuration\b[^}]*\}\s*from\s*['"]@\/utils\/format(?:\.ts)?['"]/.test(src)
      if (!importsShared) offenders.push(rel)
    }
    expect(
      offenders,
      `these files re-implement duration formatting instead of importing it from @/utils/format:\n${offenders.join('\n')}`,
    ).toEqual([])
  })

  it('resolves relative-time i18n keys only through the shared formatter', () => {
    const root = webSrcRoot()
    const offenders: string[] = []
    // The time.* namespace is owned by formatRelativeTime. Any other module
    // reaching for these keys is re-implementing the thresholds locally.
    const relativeKeys = /\bt\(\s*['"](?:time|chat\.acpSession|chat\.contentBlocks)\.(?:justNow|minutesAgo|minutesFromNow|hoursAgo|hoursFromNow|daysAgo|daysFromNow)['"]/
    for (const file of sourceFiles(root)) {
      const rel = file.replace(root + '/', '')
      if (rel === CANONICAL) continue
      if (relativeKeys.test(readFileSync(file, 'utf8'))) offenders.push(rel)
    }
    expect(
      offenders,
      `these files format relative time themselves instead of calling formatRelativeTime:\n${offenders.join('\n')}`,
    ).toEqual([])
  })

  // The scan is only meaningful if it actually reads the tree — a broken glob
  // would report "0 offenders" forever and silently stop guarding.
  it('actually scans the tree (guard is not vacuous)', () => {
    const root = webSrcRoot()
    const files = sourceFiles(root)
    expect(files.length).toBeGreaterThan(100)
    // And the canonical implementation must itself be present in the scan set,
    // so the CANONICAL exemption is not hiding a renamed file.
    expect(files.some((f) => f.replace(root + '/', '') === CANONICAL)).toBe(true)
  })
})
