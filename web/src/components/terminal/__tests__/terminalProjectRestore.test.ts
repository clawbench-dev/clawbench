import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * Terminal tabs survive a project switch AND a page reload.
 *
 * A terminal's shell runs on the server and outlives its WebSocket client, but
 * everything that identifies it locally (above all the backend session id)
 * lives in TerminalPanelContent, and App.vue keys the whole tree by project
 * path — so a project switch destroys that component. A reload never even
 * unmounts it. Both cases are only observable with a real server, so these
 * guards pin the wiring that makes them work:
 *
 *   1. the panel reads its project's record BEFORE building the tab manager,
 *   2. it hands that record to the tab manager,
 *   3. it records every change as it happens (not on unmount — a reload has no
 *      unmount),
 *   4. the recorder stops BEFORE the tab list is cleared on teardown, or the
 *      last write would describe an empty panel.
 */
describe('terminal tabs survive a project switch and a reload', () => {
  const vue = readFileSync(resolve(__dirname, '../TerminalPanelContent.vue'), 'utf8')

  /** The teardown hook — the file has several onBeforeUnmount registrations. */
  function teardownHook(): string {
    return vue.slice(vue.lastIndexOf('onBeforeUnmount(() => {'))
  }

  it('reads the project record at setup, keyed by the project it mounted in', () => {
    // `store.state.projectRoot` must be captured at setup: switchProject calls
    // setProject() before changing the tree key, so re-reading it on unmount
    // would file this project's shells under the project being switched TO.
    expect(vue).toMatch(/const projectPathAtMount = store\.state\.projectRoot/)
    expect(vue).toContain('readTerminalTabs(projectPathAtMount)')

    const recordRead = vue.indexOf('readTerminalTabs(projectPathAtMount)')
    const tabManager = vue.indexOf('useTerminalTabs(getWsUrl')
    expect(recordRead).toBeGreaterThan(-1)
    expect(tabManager).toBeGreaterThan(recordRead)
  })

  it('hands the record to the tab manager so it reattaches instead of opening new shells', () => {
    expect(vue).toMatch(/restore: savedTabsAtMount/)
  })

  it('records every tab change as it happens, not on unmount', () => {
    const persistence = vue.slice(
      vue.indexOf('const stopTabsPersistence = watch('),
      vue.indexOf('const stopTabsPersistence = watch(') + 600,
    )
    expect(persistence).toContain('saveTerminalTabs(projectPathAtMount, tabManager.snapshotTabs())')
    // Synchronous flush is what makes the teardown order below meaningful.
    expect(persistence).toContain("flush: 'sync'")
    // A deferred write would land after suspendAll() cleared the tab list.
    expect(teardownHook()).not.toContain('saveTerminalTabs')
  })

  it('stops recording before clearing the tab list on teardown', () => {
    const hook = teardownHook()
    const stop = hook.indexOf('stopTabsPersistence()')
    const suspend = hook.indexOf('tabManager.suspendAll()')

    expect(stop).toBeGreaterThan(-1)
    expect(suspend).toBeGreaterThan(-1)
    // suspendAll() empties the tab list; a still-running recorder would write
    // that emptiness as the project's tabs, so the next visit starts blank.
    expect(stop).toBeLessThan(suspend)
    // Killing the PTYs here is exactly the bug: the shells must keep running.
    expect(hook).not.toContain('disposeAll()')
  })
})
