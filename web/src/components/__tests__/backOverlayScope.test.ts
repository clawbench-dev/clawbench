import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Regression guard for the nested-drawer back gesture.
 *
 * Reported symptom: open the session drawer, tap "+" to open the agent
 * selector inside it, then swipe in from the screen edge. One swipe appeared to
 * do nothing; two swipes closed *both* drawers.
 *
 * Root cause: App.vue's overlay-closer list is consulted before the
 * priority-ordered handlers that BottomSheet registers for itself. The list
 * enumerated the session drawer but not the agent selector opened inside it, so
 * the first press closed the *lower* drawer — hidden behind the selector's own
 * scrim, hence "nothing happened" — and the second press then closed the
 * selector. The same shadowing silently disabled every drill-down handler that
 * sits above a listed drawer (git history "back to commits", session-search
 * "back to results").
 *
 * The invariant: no BottomSheet is enumerated in App.vue at all. Drawers are
 * dispatched by BottomSheet's own registration, ranked by instance creation
 * order so the topmost one always wins. What remains in App.vue is split into
 * "above the drawers" (share modal) and "below the drawers" (in-flow panels a
 * drawer's scrim covers), so a press always lands on whatever is topmost.
 *
 * This asserts on the source because App.vue is a single large component with
 * no mount-level test (same approach as projectSwitchPanelGuard.test.ts).
 * Comments are stripped so an assertion cannot be satisfied by the prose that
 * documents the rule.
 */

const APP_VUE = readWebFile('src/App.vue')

function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')   // block comments
    .replace(/\/\/.*$/gm, '')           // line comments, whole-line or trailing
}

/** Extract the array literal assigned to `name`, by bracket matching. */
function arrayLiteral(src: string, name: string): string {
  const at = src.indexOf(`const ${name} = [`)
  if (at === -1) throw new Error(`${name} not found`)
  const open = src.indexOf('[', at)
  let depth = 0
  for (let i = open; i < src.length; i++) {
    if (src[i] === '[') depth++
    else if (src[i] === ']') {
      depth--
      if (depth === 0) return src.slice(open, i + 1)
    }
  }
  throw new Error(`unbalanced brackets in ${name}`)
}

/** Extract a top-level function body by brace matching. */
function functionBody(src: string, name: string): string {
  const start = src.indexOf(`function ${name}(`)
  if (start === -1) throw new Error(`function ${name} not found`)
  const open = src.indexOf('{', start)
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
const above = arrayLiteral(code, 'overlayClosersAboveDrawers')
const below = arrayLiteral(code, 'overlayClosersBelowDrawers')

/**
 * Each entry is a BottomSheet that registers its own priority handler in
 * BottomSheet.vue. Enumerating any of them reintroduces the bug: the list is
 * consulted first and closes by array position rather than by which drawer is
 * actually on top.
 */
const bottomSheetDrawers: Array<[string, string]> = [
  ['session drawer', 'sessionDrawer'],
  ['session search drawer', 'sessionSearchDrawer'],
  ['ACP session drawer', 'acpSessionDrawer'],
  ['file search drawer', 'searchDrawer'],
  ['file history drawer', 'fileHistoryDrawer'],
  ['file details drawer', 'detailsDrawer'],
]

describe('no BottomSheet drawer is enumerated in App.vue', () => {
  for (const [label, identifier] of bottomSheetDrawers) {
    it(`leaves the ${label} to its own registration`, () => {
      expect(above, `${identifier} must not be in the above-drawers list`).not.toContain(identifier)
      expect(below, `${identifier} must not be in the below-drawers list`).not.toContain(identifier)
    })
  }

  it('does not close the narrow-screen TOC drawer from either list', () => {
    // TocDrawer is a BottomSheet: closing it here would shadow its registration
    // exactly like the reported bug.
    expect(above).not.toContain('tocDrawer')
    expect(below).not.toContain('tocDrawer')
  })

  it('does not reference the session drawer through sessionIdentity', () => {
    // The original bug enumerated it as
    // `sessionIdentity.sessionDrawer.effectiveOpen.value`.
    expect(above).not.toContain('sessionIdentity')
    expect(below).not.toContain('sessionIdentity')
  })
})

describe('what remains is layered around the drawers', () => {
  it('keeps the share modal above the drawers', () => {
    // ModalDialog renders at z-index 2500, over the BottomSheet scrim (1000),
    // so it must absorb the press before any drawer.
    expect(above).toContain('shareLinkOpen')
  })

  it('keeps the in-flow file panels below the drawers', () => {
    // The inline search bar / TOC dock live inside the file view content and
    // are covered by an open drawer's scrim. Closing them first would be the
    // same "swipe once does nothing" symptom.
    expect(below).toContain('viewSearchActive')
    expect(below).toContain('tocDockPref')
  })

  it('selects the TOC dock branch only on wide screens', () => {
    // `effectiveTocOpen` is true for both the dock (wide) and the TocDrawer
    // (narrow). Without the isWideScreen guard, closing here would also fire on
    // narrow screens and shadow the TocDrawer's own registration.
    expect(below).toContain('isWideScreen.value')
  })
})

describe('the drawer dispatch sits between the two layers', () => {
  it('hasTopmostOverlay checks above, then drawers, then below', () => {
    const body = functionBody(code, 'hasTopmostOverlay')
    const aboveAt = body.indexOf('overlayClosersAboveDrawers')
    const drawersAt = body.indexOf('canNavigateBackOverlay()')
    const belowAt = body.indexOf('overlayClosersBelowDrawers')
    expect(aboveAt, 'above-drawers check missing').toBeGreaterThan(-1)
    expect(drawersAt, 'registered drawer check missing').toBeGreaterThan(-1)
    expect(belowAt, 'below-drawers check missing').toBeGreaterThan(-1)
    // A drawer's scrim covers the in-flow panels, so the drawer check must come
    // first or the press would close something the user cannot even see.
    expect(aboveAt).toBeLessThan(drawersAt)
    expect(drawersAt).toBeLessThan(belowAt)
  })

  it('closeTopmostOverlay closes in the same order it probes', () => {
    const body = functionBody(code, 'closeTopmostOverlay')
    const aboveAt = body.indexOf('overlayClosersAboveDrawers')
    const drawersAt = body.indexOf('handleBackNavigationOverlay()')
    const belowAt = body.indexOf('overlayClosersBelowDrawers')
    expect(aboveAt).toBeGreaterThan(-1)
    expect(drawersAt, 'must fall through to the priority dispatch').toBeGreaterThan(-1)
    expect(belowAt).toBeGreaterThan(-1)
    expect(aboveAt).toBeLessThan(drawersAt)
    expect(drawersAt).toBeLessThan(belowAt)
  })
})

describe('the drawers the scope rule assumes really are BottomSheet-backed', () => {
  /**
   * The rule "BottomSheets stay out of these lists" is only correct while these
   * components keep registering their own handler. If one is ever reimplemented
   * without BottomSheet it must be added back, so pin the premise.
   */
  const sources: Array<[string, string]> = [
    ['session/SessionDrawer.vue', 'src/components/session/SessionDrawer.vue'],
    ['session/SessionSearchDrawer.vue', 'src/components/session/SessionSearchDrawer.vue'],
    ['chat/AcpSessionDrawer.vue', 'src/components/chat/AcpSessionDrawer.vue'],
    ['common/SearchDrawer.vue', 'src/components/common/SearchDrawer.vue'],
    ['git/GitHistoryDrawer.vue', 'src/components/git/GitHistoryDrawer.vue'],
    ['file/FileDetailsDrawer.vue', 'src/components/file/FileDetailsDrawer.vue'],
  ]

  for (const [label, rel] of sources) {
    it(`${label} renders a BottomSheet`, () => {
      expect(readWebFile(rel)).toContain('<BottomSheet')
    })
  }

  it('BottomSheet registers a back handler ordered by instance sequence', () => {
    const src = readWebFile('src/components/common/BottomSheet.vue')
    // The ranking is what makes "topmost drawer wins" work; the scope rule
    // depends on it, so guard the mechanism itself.
    expect(src).toContain('registerDrawerBackHandler')
    expect(src).toContain('instanceSeq')
    expect(src).toContain('PRIORITY_OVERLAY')
  })
})
