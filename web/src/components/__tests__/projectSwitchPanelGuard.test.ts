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
    const applyAt = body.indexOf('applyPanelTab(')
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
    const applyAt = body.indexOf('applyPanelTab(')
    const saveAt = body.indexOf('saveProjectPanel(resolvedProjectPath, currentPanelTab.value)')
    expect(applyAt).toBeGreaterThan(guardAt)
    expect(saveAt).toBeGreaterThan(guardAt)
  })

  it('writes the applied panel back to the target project explicitly', () => {
    // The save watcher is still suppressed at this point, so the write must be
    // explicit — otherwise the first switch to a project never records anything.
    expect(body).toContain('saveProjectPanel(resolvedProjectPath, currentPanelTab.value)')
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
    const applyAt = body.indexOf('applyPanelTab(')
    expect(restoreAt).toBeGreaterThan(-1)
    expect(applyAt, 'cold start does not restore the remembered panel').toBeGreaterThan(-1)
    expect(restoreAt).toBeLessThan(applyAt)
    expect(body).toContain('loadProjectPanel(store.state.projectRoot)')
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
