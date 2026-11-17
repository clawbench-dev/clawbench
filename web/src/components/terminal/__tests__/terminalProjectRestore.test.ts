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

/**
 * A project switch must not mount the PREVIOUS project's panel in the new tree.
 *
 * `leftTab` is module-level and survives the keyed subtree replacement, while
 * the destination's remembered tab is only applied in Phase 6 (after the
 * background loads). Without pre-setting it before the key change, the new tree
 * first renders the old project's panel — and a terminal panel mounted that way
 * restores its tabs and CONNECTS them, i.e. it starts shells in the project the
 * user is switching to.
 */
describe('project switch lands on the destination panel from the first render', () => {
  const app = readFileSync(resolve(__dirname, '../../../App.vue'), 'utf8')
  const switchFn = app.slice(
    app.indexOf('async function hotSwitchProject'),
    app.indexOf('// ── Phase 5'),
  )

  it('sets leftTab before the tree key changes', () => {
    const leftSet = switchFn.indexOf('leftTab.value = nextLeft')
    const keyChange = switchFn.indexOf('projectKey.value = newProjectPath')

    expect(leftSet).toBeGreaterThan(-1)
    expect(keyChange).toBeGreaterThan(-1)
    expect(leftSet).toBeLessThan(keyChange)
  })

  it('resolves it from the destination project record', () => {
    expect(switchFn).toContain('loadProjectPanel(resolvedProjectPath)')
    expect(switchFn).toContain('resolvePanelTab(')
  })
})
