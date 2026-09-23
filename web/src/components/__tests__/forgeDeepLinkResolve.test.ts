import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Regression guard: `clawbench-open-forge` has two producers that do NOT agree
 * on the target's field name, and App.vue must resolve both.
 *
 * The bug this exists for: the renderer's own producers (in-page notification
 * onClick, completion card) dispatch `{ projectPath, target }`, but the native
 * shell (Electron) forwards its whole `NotificationNav` through preload as the
 * event detail — and that object names the field `forgeTarget`. App.vue read
 * only `detail.target`, so on Electron the click opened the forge tab and
 * silently stopped there, while the in-page paths worked. Nothing caught it
 * because every unit test hand-built a `{ target }` payload, bypassing the real
 * native shape.
 *
 * The fix routes BOTH call sites (live click and cold-start replay) through
 * forgeTargetFromDetail. This asserts they actually do, so a future edit cannot
 * reintroduce a hand-rolled single-name read at one of them.
 *
 * Source-level because App.vue is a single large component with no mount-level
 * test (the same reason unreadNoAutoClear.test.ts asserts on source).
 */

const APP_VUE = readFileSync(join(__dirname, '..', '..', 'App.vue'), 'utf8')

/** Strip comments so an assertion cannot be satisfied by prose alone. */
function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')   // block comments
    .replace(/^\s*\/\/.*$/gm, '')       // whole-line comments
}

const code = stripComments(APP_VUE)

describe('clawbench-open-forge target resolution', () => {
  it('imports the shared resolver', () => {
    expect(code).toMatch(/import\s*\{[^}]*forgeTargetFromDetail[^}]*\}\s*from\s*'\.\/composables\/useForgeNavigation/)
  })

  it('resolves the target through the shared helper, not a single field name', () => {
    // Both the live-click handler and the cold-start replay must use it. A
    // bare `detail?.target` (or `parsed.forgeTarget`) read is the regression:
    // it silently drops the deep link on whichever path uses the other name.
    const uses = code.match(/forgeTargetFromDetail\(/g) ?? []
    expect(uses.length, 'both handleOpenForge and the cold-start branch must resolve via the helper').toBeGreaterThanOrEqual(2)
  })

  it('does not read the target by a single hand-rolled field name', () => {
    // The exact regression: reading one name directly.
    expect(code).not.toMatch(/const\s+target\s*=\s*detail\?\.target\b(?!\s*\?\?)/)
    expect(code).not.toMatch(/const\s+forgeTarget\s*=\s*parsed\.forgeTarget\b/)
  })
})
