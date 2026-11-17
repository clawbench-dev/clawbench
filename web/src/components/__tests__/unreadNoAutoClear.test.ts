import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Regression guard: switching to a tab must NOT zero its unread badge.
 *
 * Why this test exists: both the forge and task badges used to be cleared as a
 * side effect of opening the tab. That made the number meaningless — it
 * vanished before the user could find which row it referred to — and, for
 * tasks, it was worse than cosmetic: `taskUnreadCount = 0` ran BEFORE
 * `loadTasks()`, which returns early on a non-OK response, so a failed fetch
 * left the badge stuck at zero while unread runs were still there.
 *
 * Read state is per item now: opening a row marks that row read, and an explicit
 * "mark all read" button clears the rest. Nothing should clear on tab switch.
 *
 * These assert on the source because App.vue is a single large component with no
 * mount-level test.
 *
 * The "does it still reload?" question moved when the reload moved: the task
 * list stopped being loaded from a `loadTasks()` call inside App.vue's
 * `switchTab` and became the tasks plugin's `onEnter`. The old assertion looked
 * for the literal text `loadTasks()` in `switchTab` and therefore failed on
 * correct code — it was pinning the shape of the refactor, not the behaviour.
 * It now follows the call: `switchTab` and the wide layout's `sideEffects` must
 * reach the plugin lifecycle, and the tasks plugin must reload from it.
 */

function repoFile(...parts: string[]): string {
  return readFileSync(join(__dirname, '..', '..', ...parts), 'utf8')
}

const APP_VUE = repoFile('App.vue')
const TASKS_PLUGIN = repoFile('plugins', 'tasks', 'plugin.ts')

/**
 * Strip comments so an assertion cannot be satisfied by prose.
 *
 * Without this, `expect(src).toContain('loadTasks()')` passed even after the
 * real call was deleted, because a nearby explanatory comment also contained the
 * literal text — the test guarded nothing.
 */
function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')   // block comments
    .replace(/^\s*\/\/.*$/gm, '')       // whole-line comments
}

/** Extract a top-level function body by brace matching from its `function` keyword. */
function functionBody(src: string, name: string): string {
  const start = src.indexOf(`function ${name}(`)
  if (start === -1) throw new Error(`function ${name} not found`)
  const open = src.indexOf('{', start)
  let depth = 0
  for (let i = open; i < src.length; i++) {
    if (src[i] === '{') depth++
    else if (src[i] === '}') {
      depth--
      if (depth === 0) return src.slice(open, i + 1)
    }
  }
  throw new Error(`unbalanced braces in ${name}`)
}

describe('unread badges are not cleared by tab switches', () => {
  const code = stripComments(APP_VUE)

  it('never assigns zero to taskUnreadCount', () => {
    // The badge is re-derived from the server by loadTasks(); nothing should
    // write a literal zero to it.
    const zeroAssignments = code.match(/taskUnreadCount\s*=\s*0/g) ?? []
    expect(zeroAssignments).toEqual([])
  })

  it('does not clear the forge badge on tab switch', () => {
    // `markForgeRead` must not be called from a tab-switch path. The "mark all
    // read" button is the only place that clears it wholesale.
    expect(functionBody(code, 'switchTab')).not.toContain('markForgeRead')
  })

  it('still runs the plugin lifecycle when the tasks tab is opened', () => {
    // Removing the zeroing must not remove the reload. The reload is the tasks
    // plugin's `onEnter` now, reached through the shared lifecycle — so what is
    // asserted here is that switchTab still drives it (and without zeroing).
    const body = functionBody(code, 'switchTab')
    expect(body).toContain('dockLifecycle.enter(tab)')
    expect(body).not.toMatch(/taskUnreadCount\s*=\s*0/)
  })

  it('the wide-screen side-effect callback drives the same lifecycle', () => {
    // The wide-screen path has its own callback; it must reach the same
    // lifecycle (two call sites, one implementation).
    const start = code.indexOf('sideEffects:')
    expect(start).toBeGreaterThan(-1)
    const body = code.slice(start, start + 400)
    expect(body).toContain('dockLifecycle.enter(tab)')
    expect(body).not.toMatch(/taskUnreadCount\s*=\s*0/)
  })

  it('the tasks plugin still reloads the list when entered', () => {
    // The other end of the chain: entering the tab has to actually reload, or
    // the badge would stop refreshing from the server.
    const plugin = stripComments(TASKS_PLUGIN)
    const start = plugin.indexOf('onEnter:')
    expect(start).toBeGreaterThan(-1)
    expect(plugin.slice(start, start + 200)).toContain('loadTasks()')
  })
})
