import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Regression guard: `clawbench-open-forge` has two producers that do NOT agree
 * on the target's field name, and BOTH entry points must resolve both.
 *
 * The bug this exists for: the renderer's own producers (in-page notification
 * onClick, completion card) dispatch `{ projectPath, target }`, but the native
 * shell (Electron) forwards its whole `NotificationNav` through preload as the
 * ORIGINAL payload — and that object names the field `forgeTarget`. App.vue used
 * to read only `detail.target`, so on Electron the click opened the forge tab
 * and silently stopped there, while the in-page paths worked.
 *
 * Where the resolution lives now: the live click is the forge dock plugin's
 * deeplink handler (web/src/plugins/forge/plugin.ts) and the cold-start replay
 * is still in App.vue. They are two files, so the assertion follows the CALL —
 * "every place that reads a forge target goes through the helper" — rather than
 * counting calls inside one file, which is what made this guard fail on a
 * correct refactor.
 */

const ROOT = join(__dirname, '..', '..')

function source(rel: string): string {
  return stripComments(readFileSync(join(ROOT, rel), 'utf8'))
}

/** Strip comments so an assertion cannot be satisfied by prose alone. */
function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')   // block comments
    .replace(/^\s*\/\/.*$/gm, '')       // whole-line comments
}

/** Every file that handles the forge deep link (live click and cold start). */
const HANDLERS = ['App.vue', 'plugins/forge/plugin.ts'] as const

describe('clawbench-open-forge target resolution', () => {
  it.each(HANDLERS)('%s imports the shared resolver', (rel) => {
    const code = source(rel)
    // Both spellings are in use: App.vue imports from a relative path, the
    // plugin from the `@/` alias.
    expect(code).toMatch(
      /import\s*\{[^}]*forgeTargetFromDetail[^}]*\}\s*from\s*['"](?:@\/|\.\/)?composables\/useForgeNavigation(?:\.ts)?['"]/,
    )
  })

  it.each(HANDLERS)('%s resolves the target through that helper', (rel) => {
    // A bare `detail?.target` / `parsed.forgeTarget` read is the regression: it
    // silently drops the deep link on whichever producer uses the other name.
    expect(source(rel)).toMatch(/forgeTargetFromDetail\(/)
  })

  it('does not read the target by a single hand-rolled field name', () => {
    for (const rel of HANDLERS) {
      const code = source(rel)
      expect(code, rel).not.toMatch(/const\s+target\s*=\s*detail\?\.target\b(?!\s*\?\?)/)
      expect(code, rel).not.toMatch(/const\s+forgeTarget\s*=\s*parsed\.forgeTarget\b/)
    }
  })

  it('still finds both handlers (a moved file must fail here, not silently pass)', () => {
    // Without this, deleting a handler would make the per-file assertions above
    // vacuous only if the file itself vanished — readFileSync would throw, which
    // is the intended loud failure. This asserts the identifiers exist so a
    // renamed function is noticed too.
    expect(source('plugins/forge/plugin.ts')).toMatch(/clawbench-open-forge/)
    expect(source('App.vue')).toMatch(/getPendingNavigation/)
  })
})
