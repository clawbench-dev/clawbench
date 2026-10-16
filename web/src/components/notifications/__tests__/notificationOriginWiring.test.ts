import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Opening a notification must be able to come BACK.
 *
 * The notification center is a dock tab whose rows jump to the row's source
 * (session / task / forge). That jump has to record the log as its origin, or
 * Back (header button, edge swipe, Android hardware back) returns the user to
 * whatever tab preceded the log — a place they were never in.
 *
 * The dispatch and the recording live in two different files (the panel
 * dispatches a window event; App.vue's handlers do the navigation), so neither
 * side can be verified by mounting the other. This pins the contract between
 * them at the source level:
 *
 *   - the panel must flag its dispatches as coming from the log, and
 *   - App.vue must act on that flag for all three event kinds.
 *
 * It also pins the cross-project rule: the origin is deliberately NOT recorded
 * when the jump switches projects (that path tears the subtree down and drops
 * the previous project's navigation context, so a Back target pointing into the
 * old project would be a lie). The confirmation dialog is what covers that case
 * — asserted here so a future refactor cannot quietly drop both.
 *
 * cwd differs between a bare `vitest` run (web/) and scripts/vitest-run.sh
 * (repo root), so probe both roots.
 */

function readWebFile(relPath: string): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, relPath), 'utf8')
    } catch {
      // try the next candidate root
    }
  }
  throw new Error(`${relPath} not found from cwd: ` + process.cwd())
}

const panelRaw = readWebFile('src/components/notifications/NotificationsPanelContent.vue')
const appRaw = readWebFile('src/App.vue')
const coordinatorRaw = readWebFile('src/composables/useNavigationCoordinator.ts')

/**
 * Strip `//` line comments and `/* *​/` blocks before matching.
 *
 * Without this, a mutation that comments a line out still satisfies a `toMatch`
 * on the surrounding text — the guard would report success on exactly the
 * change it exists to catch.
 */
function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/(^|[^:])\/\/[^\n]*/g, '$1')
}

const panel = stripComments(panelRaw)
const app = stripComments(appRaw)
const coordinator = stripComments(coordinatorRaw)

describe('notification navigation origin wiring', () => {
  it('the panel flags every dispatch as coming from the log', () => {
    // One flag per dispatch site (session / task / forge). A missed one is a
    // silently dead Back button on that row kind only.
    const flags = panel.match(/fromNotificationCenter: true/g) ?? []
    expect(flags.length, 'each of the three open-session/task/forge dispatches must carry the flag').toBe(3)
  })

  it('App.vue records the log as origin for all three event kinds', () => {
    // The helper is the single place that arms the origin.
    expect(app).toMatch(/function recordNotificationOrigin\(/)
    expect(app).toMatch(/beginExternalJump\('notifications', surfaceLabel\('notifications'\)/)
    // Called from each of the three handlers.
    const calls = app.match(/recordNotificationOrigin\(detail\)/g) ?? []
    expect(calls.length, 'handleOpenSession / handleOpenTask / handleOpenForge must each record the origin').toBe(3)
  })

  it('does NOT record an origin on the cross-project path', () => {
    // The origin is only armed when the jump stays in this project. Both the
    // session and task handlers gate on the same `crossProject` value.
    expect(app).toMatch(/const crossProject = [^\n]+\n\s*if \(!crossProject\) recordNotificationOrigin\(detail\)/)
    // The forge handler has no `crossProject` local — it gates on its own
    // (inverted) project check, with the recording on the same-project branch.
    expect(app).toMatch(/if \(!projectPath \|\| projectPath === store\.state\.projectRoot\) \{[^}]*recordNotificationOrigin\(detail\)/)
  })

  it('the panel confirms before a cross-project jump, and cancelling aborts', () => {
    // The dialog is what replaces Back for the cross-project case; without it
    // the jump would be both irreversible AND unreturnable.
    expect(panel).toMatch(/crossProjectConfirm/)
    expect(panel).toMatch(/if \(!ok\) return/)
    // The confirmation must use the SAME canonical comparison as the row
    // marker — an exact `!==` here would pop a "switch project?" dialog for
    // the project the user is already in, whenever the two sides are spelled
    // differently (raw POST path vs canonical DB path).
    expect(panel).toMatch(/const crossProject = isExternalProject\(n\)/)
    expect(panel).not.toMatch(/n\.projectPath !== activeProject\.value/)
  })

  it('gives the jump origin a Back label of its own', () => {
    // A surface with no label falls through to the generic "Back", which tells
    // the user nothing about where Back goes.
    expect(coordinator).toMatch(/surface === 'notifications'\) return t\('file\.nav\.backToNotifications'\)/)
  })
})
