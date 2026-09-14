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
 * This asserts on the source because App.vue is a single large component with no
 * mount-level test.
 */

const APP_VUE = readFileSync(
  join(__dirname, '..', '..', 'App.vue'),
  'utf8',
)

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

  it('still reloads the task list when the tasks tab is opened', () => {
    // Removing the zeroing must not remove the reload — the badge has to be
    // refreshed from the server. Asserted on the COMMENT-STRIPPED body so the
    // check cannot be satisfied by a comment that merely mentions the call.
    const body = functionBody(code, 'switchTab')
    expect(body).toContain('loadTasks()')
  })

  it('the tasks side-effect callback reloads without zeroing', () => {
    // The wide-screen path has its own callback; it must reload too.
    const start = code.indexOf('sideEffects:')
    expect(start).toBeGreaterThan(-1)
    const body = code.slice(start, start + 400)
    expect(body).toContain('loadTasks()')
    expect(body).not.toMatch(/taskUnreadCount\s*=\s*0/)
  })
})
