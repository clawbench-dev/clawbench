import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Regression guards for issue #474 — the left panel's open state is remembered
 * per project across a switch.
 *
 * The bug had two halves, and BOTH must stay fixed:
 *  1. `leftTab` was persisted under a single global key, so one project
 *     overwrote another's.
 *  2. During a switch, resetProjectState() nulls `currentFile`, which fired the
 *     "file closed → fall back to the file manager" watcher. That rewrote the
 *     left tab to 'browse' — actively destroying the very tab being restored.
 *
 * These assert on the source because App.vue is a single large component with no
 * mount-level test (same approach as unreadNoAutoClear.test.ts). Comments are
 * stripped first: without that, an assertion can be satisfied by prose describing
 * the behaviour instead of the code that implements it.
 */

const APP_VUE = readFileSync(join(__dirname, '..', '..', 'App.vue'), 'utf8')

/**
 * Strip comments so an assertion cannot be satisfied by prose.
 *
 * Without this, `expect(src).toContain('loadTasks()')` passed even after the
 * real call was deleted, because a nearby explanatory comment also contained
 * the literal text — the test guarded nothing.
 *
 * Trailing (`// …` after code) comments are removed too, not just whole-line
 * ones. Otherwise `if (!file && prevFile) { // isPanelSwitchInFlight()` would
 * satisfy a same-line `toContain` assertion for the guard while the guard is
 * absent from the condition.
 *
 * Note this is deliberately naive about strings — App.vue contains no `//`
 * inside a string literal on a line this file asserts about. If that changes,
 * switch to a real parser rather than loosening this.
 */
function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')   // block comments
    .replace(/\/\/.*$/gm, '')           // line comments, whole-line or trailing
}

/**
 * Extract a top-level function body by brace matching from its `function` keyword.
 *
 * Skips the parameter list first: `hotSwitchProject` has an inline object type in
 * its signature (`pendingTaskNav?: { taskId: string }`), so taking the first `{`
 * after the function name would match that type instead of the body.
 */
function functionBody(src: string, name: string): string {
  const start = src.indexOf(`function ${name}(`)
  if (start === -1) throw new Error(`function ${name} not found`)
  // Walk the parameter list to its closing paren.
  let parenDepth = 0
  let i = src.indexOf('(', start)
  for (; i < src.length; i++) {
    if (src[i] === '(') parenDepth++
    else if (src[i] === ')') {
      parenDepth--
      if (parenDepth === 0) break
    }
  }
  const open = src.indexOf('{', i)
  if (open === -1) throw new Error(`no body for ${name}`)
  let depth = 0
  for (let j = open; j < src.length; j++) {
    if (src[j] === '{') depth++
    else if (src[j] === '}') {
      depth--
      if (depth === 0) return src.slice(open, j + 1)
    }
  }
  throw new Error(`unbalanced braces in ${name}`)
}

const code = stripComments(APP_VUE)

describe('project switch suppresses panel writes', () => {
  const body = functionBody(code, 'hotSwitchProject')

  it('opens the suppression window before the store is reset', () => {
    const beginAt = body.indexOf('beginPanelSwitch()')
    const setProjectAt = body.indexOf('store.setProject(')
    expect(beginAt, 'beginPanelSwitch() missing from hotSwitchProject').toBeGreaterThan(-1)
    expect(setProjectAt, 'store.setProject() missing from hotSwitchProject').toBeGreaterThan(-1)
    // setProject() is what nulls currentFile and fires the fallback watcher, so
    // the guard has to be armed first — otherwise the write slips through.
    expect(beginAt).toBeLessThan(setProjectAt)
  })

  it('releases the suppression window on every exit path', () => {
    // Release must not depend on reaching the end of the function: callers
    // invoke hotSwitchProject fire-and-forget with a swallowing `.catch()`, so an
    // early return or a thrown error would otherwise strand the flag at >= 1 and
    // silently disable panel persistence for the rest of the session.
    expect(body, 'no try/finally around the switch body').toContain('finally {')
    // The backstop is the idempotent releaser, called from the finally.
    const finallyAt = body.lastIndexOf('finally {')
    expect(body.slice(finallyAt)).toContain('releasePanelSwitch()')
  })

  it('makes the release idempotent so the finally cannot double-count', () => {
    // The normal path releases before Phase 7 (so the pending navigation's
    // landing panel gets recorded); the finally then runs too. Without the guard
    // flag the counter would go negative and later switches would leak.
    expect(body).toContain('panelSwitchReleased')
    const guardAt = body.indexOf('if (panelSwitchReleased) return')
    expect(guardAt, 'release is not idempotent').toBeGreaterThan(-1)
    expect(body.indexOf('endPanelSwitch()')).toBeGreaterThan(guardAt)
  })

  it('releases the window before the pending-navigation phase', () => {
    // Phase 7 switches to tasks / opens a session; that landing panel is what the
    // user should return to, so persistence must be live again by then.
    const releaseAt = body.indexOf('releasePanelSwitch()')
    const phase7At = body.indexOf('if (pendingTaskNav)', releaseAt)
    expect(releaseAt).toBeGreaterThan(-1)
    expect(phase7At, 'Phase 7 must still follow the release').toBeGreaterThan(-1)
  })

  it('applies the remembered panel only after the workspace restore', () => {
    const restoreAt = body.indexOf('await restoreProjectWorkspace()')
    const applyAt = body.indexOf('applyProjectPanel(')
    expect(restoreAt).toBeGreaterThan(-1)
    expect(applyAt).toBeGreaterThan(-1)
    // currentFile must already be populated, or a remembered 'view' panel would
    // resolve to an empty viewer.
    expect(restoreAt).toBeLessThan(applyAt)
  })

  it('stands down when a newer switch already landed on another project', () => {
    // Two switches can overlap (callers are not serialized). If a newer one
    // finished first, applying this older switch's panel would drag the user to
    // the wrong project's tab and record the wrong panel under this key.
    const guardAt = body.indexOf('store.state.projectRoot === resolvedProjectPath')
    expect(guardAt, 'missing re-entrancy guard before applying the panel').toBeGreaterThan(-1)
    const applyAt = body.indexOf('applyProjectPanel(')
    expect(applyAt).toBeGreaterThan(guardAt)
  })

  it('writes the applied panel back to the target project explicitly', () => {
    // The save watcher is still suppressed at this point, so the write must be
    // explicit — otherwise the first switch to a project never records anything.
    // Both the tab and the collapsed flag are written by applyProjectPanel.
    const applyBody = functionBody(code, 'applyProjectPanel')
    expect(applyBody).toContain('saveProjectPanel(root, currentPanelTab.value)')
    expect(applyBody).toContain('saveProjectPanelCollapsed(root, leftCollapsed.value)')
  })
})

describe('project switch guards unsaved file edits', () => {
  const body = functionBody(code, 'hotSwitchProject')

  it('consults the file-editor leave guard before touching any switch state', () => {
    // The switch destroys the editor subtree (Phase 4), so the save/discard
    // confirmation has to run first — otherwise unsaved edits vanish silently.
    const guardAt = body.indexOf('fileEditor.confirmLeave()')
    expect(guardAt, 'confirmLeave() missing from hotSwitchProject').toBeGreaterThan(-1)
    // Awaited: the confirm dialog is async, and the switch must not proceed
    // until the user has answered.
    expect(body.slice(guardAt - 6, guardAt)).toContain('await ')
    const beginAt = body.indexOf('beginPanelSwitch()')
    const setProjectAt = body.indexOf('store.setProject(')
    expect(beginAt).toBeGreaterThan(-1)
    expect(setProjectAt).toBeGreaterThan(-1)
    expect(guardAt).toBeLessThan(beginAt)
    expect(guardAt).toBeLessThan(setProjectAt)
  })

  it('aborts the switch when the guard refuses to leave', () => {
    // Cancelling the save prompt (or a failed save) must return early — an
    // unguarded fall-through would switch projects and drop the edits anyway.
    const guardAt = body.indexOf('fileEditor.confirmLeave()')
    const lineEnd = body.indexOf('\n', guardAt)
    expect(body.slice(guardAt, lineEnd)).toContain('return')
  })
})

describe('project switch does not leak staged input across projects', () => {
  const body = functionBody(code, 'hotSwitchProject')

  it('snapshots the leaving session before resetIdentity clears it', () => {
    // Staged quotes/attachments are module-level singletons that survive the
    // keyed subtree rebuild. The session id must be read BEFORE resetIdentity()
    // clears it, or the snapshot lands under '' and is never restorable.
    const captureAt = body.indexOf('const leavingSessionId = sessionIdentity.currentSessionId.value')
    const resetAt = body.indexOf('resetIdentity()')
    expect(captureAt, 'leaving session id is not captured').toBeGreaterThan(-1)
    expect(resetAt).toBeGreaterThan(-1)
    expect(captureAt).toBeLessThan(resetAt)
  })

  it('clears the live staged input via leaveInputDraft', () => {
    // Without this the previous project's quote stays in the live refs and
    // reappears in the new project's input box.
    expect(body, 'hotSwitchProject never clears the staged input').toContain('leaveInputDraft(leavingSessionId)')
  })

  it('restores the new session draft after identity resolves', () => {
    // The switch bypasses the session manager (which restores on a session
    // switch), so the restore has to happen here or a returning user loses
    // their draft.
    const initAt = body.indexOf('await sessionIdentity.initSessionFromAPI()')
    const restoreAt = body.indexOf('restoreAttachments(')
    expect(initAt).toBeGreaterThan(-1)
    expect(restoreAt, 'staged input is never restored after the switch').toBeGreaterThan(-1)
    expect(initAt).toBeLessThan(restoreAt)
  })
})

describe('the file-closed fallback watcher respects the switch guard', () => {
  it('is guarded by isPanelSwitchInFlight()', () => {
    const src = code
    const watcherAt = src.indexOf('watch(() => currentFile.value')
    expect(watcherAt).toBeGreaterThan(-1)
    // Take the watcher's own block and assert the guard sits in its condition.
    const conditionAt = src.indexOf('if (!file && prevFile', watcherAt)
    expect(conditionAt).toBeGreaterThan(-1)
    const lineEnd = src.indexOf('\n', conditionAt)
    expect(src.slice(conditionAt, lineEnd)).toContain('isPanelSwitchInFlight()')
  })
})

describe('panel persistence', () => {
  it('saves the current panel on change, guarded by the switch flag', () => {
    const at = code.indexOf('watch(currentPanelTab')
    expect(at, 'panel-save watcher missing').toBeGreaterThan(-1)
    const body = code.slice(at, code.indexOf('\n})', at))
    expect(body).toContain('isPanelSwitchInFlight()')
    expect(body).toContain('saveProjectPanel(store.state.projectRoot, tab)')
  })

  it('routes the restored panel through the layout that owns it', () => {
    const body = functionBody(code, 'applyPanelTab')
    // Wide screens use the dock; narrow ones use the plain tab. Both must be
    // reachable or one of the two layouts silently keeps the old behaviour.
    expect(body).toContain('switchLeftTab(tab)')
    expect(body).toContain('switchTab(tab)')
    expect(body).toContain('isWideScreen.value')
  })

  it('restores the panel on cold start too', () => {
    const body = functionBody(code, 'initializeApp')
    const restoreAt = body.indexOf('await restoreProjectWorkspace()')
    const applyAt = body.indexOf('applyProjectPanel(')
    expect(restoreAt).toBeGreaterThan(-1)
    expect(applyAt, 'cold start does not restore the remembered panel').toBeGreaterThan(-1)
    expect(restoreAt).toBeLessThan(applyAt)
  })
})

describe('the collapsed left column is remembered per project', () => {
  it('applies the collapsed flag AFTER the tab (switchLeftTab re-expands)', () => {
    // switchLeftTab unconditionally sets leftCollapsed = false. Applying the
    // collapsed flag first would be silently undone, so the order is the whole
    // reason this is one function.
    const body = functionBody(code, 'applyProjectPanel')
    const tabAt = body.indexOf('applyPanelTab(')
    const collapsedAt = body.indexOf('setLeftCollapsed(')
    expect(tabAt).toBeGreaterThan(-1)
    expect(collapsedAt, 'collapsed flag is never restored').toBeGreaterThan(-1)
    expect(tabAt).toBeLessThan(collapsedAt)
  })

  it('defaults an unrecorded project to expanded, not the previous state', () => {
    // loadProjectPanelCollapsed returns false (not null) for a project with no
    // record, so a first visit lands on the layout default instead of inheriting
    // the previous project's collapse.
    const body = functionBody(code, 'applyProjectPanel')
    expect(body).toContain('setLeftCollapsed(loadProjectPanelCollapsed(root))')
  })

  it('only reads/writes collapsed state on a wide screen', () => {
    // Collapsed is a wide-screen concept. On a narrow screen recording it would
    // park leftCollapsed = true and hide the column on the next widen.
    const applyBody = functionBody(code, 'applyProjectPanel')
    const guardAt = applyBody.indexOf('if (isWideScreen.value)')
    expect(guardAt, 'applyProjectPanel does not gate collapsed state on wide screen').toBeGreaterThan(-1)
    expect(applyBody.slice(guardAt)).toContain('setLeftCollapsed(')

    // The save watcher must carry the same gate.
    const watcherAt = code.indexOf('watch(leftCollapsed')
    expect(watcherAt, 'collapsed-save watcher missing').toBeGreaterThan(-1)
    const watcherBody = code.slice(watcherAt, code.indexOf('\n})', watcherAt))
    expect(watcherBody).toContain('isPanelSwitchInFlight()')
    expect(watcherBody).toContain('isWideScreen.value')
    expect(watcherBody).toContain('saveProjectPanelCollapsed(store.state.projectRoot, collapsed)')
  })

  it('keeps the left column visible when widening from a non-chat panel', () => {
    // With the column hidden by default, entering wide-screen while the single
    // narrow column was on a non-chat panel must NOT hide it — otherwise
    // widening from the file manager lands on chat and reads as "my panel
    // disappeared". `switchLeftTab` already expands when the tab changes; the
    // guard covers the case where leftTab already matched.
    const at = code.indexOf('watch(isWideScreen')
    expect(at).toBeGreaterThan(-1)
    const block = code.slice(at, code.indexOf('}, { immediate: true })', at))
    expect(block).toContain("if (enteredTab !== 'chat') setLeftCollapsed(false)")
  })
})

describe('the wide-screen layout keeps no global leftTab storage', () => {
  const layoutSrc = readFileSync(
    join(__dirname, '..', '..', 'composables', 'useWideScreenLayout.ts'),
    'utf8',
  )

  it('does not write leftTab to a global localStorage key', () => {
    // A second writer would race the per-project one and reintroduce the bug.
    const stripped = stripComments(layoutSrc)
    expect(stripped).not.toContain('clawbench-widescreen-left-tab')
    expect(stripped).not.toContain('WIDE_SCREEN_LEFT_TAB_KEY')
  })
})

describe('stripComments removes trailing comments too', () => {
  // The same-line `toContain` assertions above are only meaningful if a comment
  // on that line cannot satisfy them. A trailing `//` comment is the realistic
  // way that would happen, so guard the helper itself.
  it('strips a trailing comment so it cannot satisfy a same-line assertion', () => {
    const line = '    if (!file && prevFile) { // isPanelSwitchInFlight()'
    expect(line).toContain('isPanelSwitchInFlight()')
    expect(stripComments(line)).not.toContain('isPanelSwitchInFlight()')
  })

  it('strips whole-line comments', () => {
    expect(stripComments('// isPanelSwitchInFlight()')).not.toContain('isPanelSwitchInFlight()')
  })

  it('strips block comments', () => {
    expect(stripComments('/* isPanelSwitchInFlight() */')).not.toContain('isPanelSwitchInFlight()')
  })

  it('keeps real code intact', () => {
    const src = 'if (!file && prevFile && !isPanelSwitchInFlight()) {\n  x()\n}'
    expect(stripComments(src)).toContain('isPanelSwitchInFlight()')
  })

  it('does not let a trailing comment fake the re-entrancy guard', () => {
    // The exact shape the guard assertion would otherwise accept.
    const faked = 'if (true) { // store.state.projectRoot === resolvedProjectPath'
    expect(stripComments(faked)).not.toContain('store.state.projectRoot === resolvedProjectPath')
  })
})
