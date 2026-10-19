import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Read a repo-root file (outside web/). readWebFile is web-relative by design,
 * so the Go-side guard resolves from the repo root instead. Both cwds the runner
 * may use are probed, same reason as readWebFile.
 */
function readRepoFile(relPath: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    try {
      return readFileSync(resolve(base, relPath), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(`${relPath} not found from cwd: ${process.cwd()}`)
}

/**
 * Source guards for invariants that no behavioural test can see.
 *
 * Both defects these guard against are silent: a duplicated formatter produces
 * identical output today and drifts later, and a duplicated SQL expression
 * produces a plausible-looking number that disagrees with the stats panel only
 * for edge-case rows.
 */

function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/\/\/.*$/gm, '')
}

describe('usage formatter is defined exactly once', () => {
  it('statsChart does not define its own compact-count formatter', () => {
    const src = stripComments(readWebFile('src/components/stats/statsChart.ts'))
    // The K/M/B thresholds must live only in utils/usageFormat.ts. A local copy
    // here would drift from the header capsule's numbers the first time one of
    // the two is tuned (they are read side by side).
    expect(src).not.toMatch(/function\s+formatTokenCount\s*\(/)
    expect(src).not.toMatch(/999_500_000|999_950/)
    expect(src).toContain("from '@/utils/usageFormat'")
  })

  it('the shared formatter keeps the documented tier boundaries', () => {
    const src = stripComments(readWebFile('src/utils/usageFormat.ts'))
    expect(src).toContain('999_500_000')
    expect(src).toContain('999_950')
    expect(src).toContain('999.5')
  })
})

describe('usage meter token expression mirrors the stats panel', () => {
  it('the meter uses the same total-tokens fallback as usage_stats', () => {
    const meter = stripComments(readRepoFile('internal/service/usage_meter.go'))
    // total_tokens is 0 for rows written by older parsers; both the stats
    // panel's "total" column and the meter must fall back to input+output, or
    // the same period shows two different numbers.
    expect(meter).toContain(
      'CASE WHEN m.total_tokens > 0 THEN m.total_tokens ELSE m.input_tokens + m.output_tokens END',
    )
  })
})
