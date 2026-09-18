import { describe, expect, it, beforeEach } from 'vitest'
import {
  PROJECT_PANEL_PREFIX,
  _clearProjectPanelForTesting,
  beginPanelSwitch,
  endPanelSwitch,
  isPanelSwitchInFlight,
  loadProjectPanel,
  resolvePanelTab,
  saveProjectPanel,
} from '@/composables/useProjectPanel'

describe('useProjectPanel storage', () => {
  const PROJECT_A = '/project/a'
  const PROJECT_B = '/project/b'

  beforeEach(() => {
    localStorage.clear()
    _clearProjectPanelForTesting()
  })

  it('keeps each project\'s panel in its own key', () => {
    saveProjectPanel(PROJECT_A, 'view')
    saveProjectPanel(PROJECT_B, 'tasks')

    // The whole point of the fix: switching projects must not let one project's
    // panel overwrite another's (the old single global key did exactly that).
    expect(loadProjectPanel(PROJECT_A)).toBe('view')
    expect(loadProjectPanel(PROJECT_B)).toBe('tasks')
    expect(localStorage.getItem(PROJECT_PANEL_PREFIX + PROJECT_A)).toBe('view')
  })

  it('overwrites only the key it is given', () => {
    saveProjectPanel(PROJECT_A, 'view')
    saveProjectPanel(PROJECT_B, 'history')
    saveProjectPanel(PROJECT_A, 'settings')

    expect(loadProjectPanel(PROJECT_A)).toBe('settings')
    expect(loadProjectPanel(PROJECT_B)).toBe('history')
  })

  it('does not write for an empty project root', () => {
    saveProjectPanel('', 'view')
    expect(localStorage.getItem(PROJECT_PANEL_PREFIX)).toBeNull()
  })

  it('returns null for an unknown or empty project root', () => {
    expect(loadProjectPanel(PROJECT_A)).toBeNull()
    expect(loadProjectPanel('')).toBeNull()
  })

  it('a project switch does not dirty the target project\'s record before it is read', () => {
    // Mirrors hotSwitchProject's ordering: the record must survive the window in
    // which the switching watchers are suppressed but the save watcher is live.
    saveProjectPanel(PROJECT_B, 'view')
    beginPanelSwitch()
    // A stray write during the switch would land in whichever project is current.
    // The guard is what prevents it; here we assert the read stays intact.
    expect(loadProjectPanel(PROJECT_B)).toBe('view')
  })
})

describe('panel switch in-flight flag', () => {
  beforeEach(() => {
    _clearProjectPanelForTesting()
  })

  it('is false by default and true only between begin and end', () => {
    expect(isPanelSwitchInFlight()).toBe(false)
    beginPanelSwitch()
    expect(isPanelSwitchInFlight()).toBe(true)
    endPanelSwitch()
    expect(isPanelSwitchInFlight()).toBe(false)
  })

  it('is reset by the test helper', () => {
    // A leaked `true` flag silently disables panel persistence — the exact
    // failure mode this module exists to fix — so it must be resettable.
    beginPanelSwitch()
    _clearProjectPanelForTesting()
    expect(isPanelSwitchInFlight()).toBe(false)
  })

  it('tolerates a redundant end', () => {
    endPanelSwitch()
    expect(isPanelSwitchInFlight()).toBe(false)
  })

  it('stays in flight until the last of two overlapping switches ends', () => {
    // hotSwitchProject can be re-entered (picker, session-completion popup, push
    // handler), so a first-finished switch must not re-enable persistence while a
    // second is still running — that would let its watchers write the wrong panel.
    beginPanelSwitch()
    beginPanelSwitch()
    endPanelSwitch()
    expect(isPanelSwitchInFlight()).toBe(true)
    endPanelSwitch()
    expect(isPanelSwitchInFlight()).toBe(false)
  })
})

describe('resolvePanelTab', () => {
  const cases: Array<{
    name: string
    remembered: string | null
    wide: boolean
    hasFile: boolean
    expected: string
  }> = [
    { name: 'wide, no memory → file manager', remembered: null, wide: true, hasFile: false, expected: 'browse' },
    { name: 'narrow, no memory → chat', remembered: null, wide: false, hasFile: false, expected: 'chat' },
    { name: 'wide, remembered view with a file → view', remembered: 'view', wide: true, hasFile: true, expected: 'view' },
    { name: 'narrow, remembered view with a file → view', remembered: 'view', wide: false, hasFile: true, expected: 'view' },
    // Landing on an empty viewer is worse than landing on the file manager —
    // the one narrowing kept from the old "cold start must not jump to the
    // viewer" rule.
    { name: 'wide, remembered view without a file → browse', remembered: 'view', wide: true, hasFile: false, expected: 'browse' },
    { name: 'narrow, remembered view without a file → browse', remembered: 'view', wide: false, hasFile: false, expected: 'browse' },
    // Chat is the right-hand pane on a wide screen and can never be a left tab.
    { name: 'wide, remembered chat → browse', remembered: 'chat', wide: true, hasFile: false, expected: 'browse' },
    { name: 'narrow, remembered chat → chat', remembered: 'chat', wide: false, hasFile: false, expected: 'chat' },
    { name: 'wide, remembered tasks → tasks', remembered: 'tasks', wide: true, hasFile: false, expected: 'tasks' },
    { name: 'narrow, remembered terminal → terminal', remembered: 'terminal', wide: false, hasFile: false, expected: 'terminal' },
    { name: 'wide, remembered settings → settings', remembered: 'settings', wide: true, hasFile: false, expected: 'settings' },
    // Unknown ids (a tab removed from the registry, or corrupted storage) fall
    // back to the layout default rather than being applied and silently ignored.
    { name: 'wide, unknown tab → browse', remembered: 'not-a-tab', wide: true, hasFile: false, expected: 'browse' },
    { name: 'narrow, unknown tab → chat', remembered: 'not-a-tab', wide: false, hasFile: false, expected: 'chat' },
    { name: 'wide, empty string → browse', remembered: '', wide: true, hasFile: false, expected: 'browse' },
  ]

  for (const c of cases) {
    it(c.name, () => {
      expect(resolvePanelTab(c.remembered, c.wide, c.hasFile)).toBe(c.expected)
    })
  }

  it('never returns chat on a wide screen', () => {
    // Exhaustive over the dock registry: a `chat` left tab would render nothing
    // (the left column has no chat panel).
    for (const tab of ['chat', 'browse', 'view', 'history', 'forge', 'tasks', 'terminal', 'proxy', 'stats', 'settings']) {
      expect(resolvePanelTab(tab, true, true)).not.toBe('chat')
    }
  })

  it('only returns a registered tab for a wide screen', () => {
    const wideTabs = ['browse', 'view', 'history', 'forge', 'tasks', 'terminal', 'proxy', 'stats', 'settings']
    for (const tab of [...wideTabs, 'chat', 'nope', null, '']) {
      expect(wideTabs).toContain(resolvePanelTab(tab, true, true))
    }
  })
})
