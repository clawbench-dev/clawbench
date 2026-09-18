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

function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')   // block comments
    .replace(/^\s*\/\/.*$/gm, '')       // whole-line comments
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

  it('closes the suppression window on every exit path', () => {
    // Two exits: the setProject() failure early-return, and the normal
    // completion. Both must clear the flag — a leaked `true` silently disables
    // panel persistence for the rest of the session.
    const occurrences = body.match(/endPanelSwitch\(\)/g) ?? []
    expect(occurrences.length).toBe(2)
  })

  it('closes the window on the failed-switch early return', () => {
    // The catch block returns early; without ending the switch there, the flag
    // stays true forever and panel persistence dies silently.
    const catchAt = body.indexOf('catch (err)')
    const returnAt = body.indexOf('return', catchAt)
    const endAt = body.indexOf('endPanelSwitch()')
    expect(catchAt).toBeGreaterThan(-1)
    expect(endAt).toBeGreaterThan(catchAt)
    expect(endAt).toBeLessThan(returnAt)
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

  it('reads the target project\'s record and re-enables persistence before pending navigation', () => {
    const readAt = body.indexOf('loadProjectPanel(resolvedProjectPath)')
    // The LAST endPanelSwitch() is the normal-completion one; the first is the
    // failure early-return (asserted above).
    const endAt = body.lastIndexOf('endPanelSwitch()')
    expect(readAt, 'must read the resolved (normalized) project path').toBeGreaterThan(-1)
    expect(readAt).toBeLessThan(endAt)
    // Phase 7 switches to tasks / opens a session; that landing panel is what the
    // user should return to, so it must be recorded — hence endPanelSwitch first.
    // Located by searching AFTER endAt, since the parameter name also appears in
    // the signature.
    const phase7At = body.indexOf('if (pendingTaskNav)', endAt)
    expect(phase7At, 'Phase 7 must still follow the re-enable').toBeGreaterThan(-1)
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
