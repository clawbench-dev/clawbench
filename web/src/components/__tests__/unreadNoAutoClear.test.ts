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
 * mount-level test; the check is deliberately narrow (the specific assignment
 * must not reappear) rather than trying to trace every code path.
 */

const APP_VUE = readFileSync(
  join(__dirname, '..', '..', 'App.vue'),
  'utf8',
)

describe('unread badges are not cleared by tab switches', () => {
  it('never assigns zero to taskUnreadCount', () => {
    // The badge is re-derived from the server by loadTasks(); nothing should
    // write a literal zero to it.
    const zeroAssignments = APP_VUE.match(/taskUnreadCount\s*=\s*0/g) ?? []
    expect(zeroAssignments).toEqual([])
  })

  it('does not clear the forge badge on tab switch', () => {
    // `markForgeRead` must not be called from a tab-switch path. The "mark all
    // read" button is the only place that clears it wholesale.
    const switchTab = APP_VUE.slice(
      APP_VUE.indexOf('function switchTab('),
      APP_VUE.indexOf('function switchTab(') + 2000,
    )
    expect(switchTab).not.toContain('markForgeRead')
  })

  it('still reloads the task list when the tasks tab is opened', () => {
    // Removing the zeroing must not remove the reload — the badge has to be
    // refreshed from the server.
    const switchTab = APP_VUE.slice(
      APP_VUE.indexOf('function switchTab('),
      APP_VUE.indexOf('function switchTab(') + 2000,
    )
    expect(switchTab).toContain('loadTasks()')
  })
})
