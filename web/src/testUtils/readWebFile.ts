import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Read a file from the repository, tolerating either working directory the test
 * runner may use.
 *
 * The frontend lives in `web/`, but the two ways to run the suite disagree on
 * cwd: a bare `vitest` from `web/` has cwd = web/, while the official
 * `npm test` → scripts/vitest-run.sh cds to the repo root. A spec that resolves
 * a path against a single assumed cwd passes locally and fails in CI (or the
 * reverse) with ENOENT — a failure that looks like a missing file rather than a
 * cwd mismatch.
 *
 * Pass a path relative to `web/` (e.g. 'src/components/chat/ContentBlocks.vue'
 * or 'css/variables.css'); both candidate roots are probed.
 */
export function readWebFile(relPath: string): string {
  const candidates = [process.cwd(), resolve(process.cwd(), 'web'), resolve(process.cwd(), '../web')]
  for (const base of candidates) {
    try {
      return readFileSync(resolve(base, relPath), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(`${relPath} not found from cwd: ${process.cwd()}`)
}
