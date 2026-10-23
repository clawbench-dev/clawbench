import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import {
  DEFAULT_FM_PREVIEW_RATIO,
  DEFAULT_LEFT_COLLAPSED,
  DEFAULT_SESSION_SIDEBAR_WIDTH,
  DEFAULT_TOC_DOCK_SIDE,
  DEFAULT_TOC_DOCK_WIDTH,
  PANEL_ENTRIES,
  PANEL_KEY_PREFIX,
  _resetLayoutScopeForTest,
  effectiveScope,
  getLayoutProjectRoot,
  getLayoutScopeMode,
  layoutEpoch,
  migrateLegacyKeys,
  physicalKey,
  readPanelState,
  seedProjectFromGlobal,
  setLayoutProjectRoot,
  setLayoutScopeMode,
  writePanelState,
} from '@/utils/panelState'
import type { PanelId } from '@/utils/panelState'
import { DEFAULT_RATIO } from '@/utils/splitRatio'

const ROOT_A = '/project/a'
const ROOT_B = '/project/b'

describe('panelState registry shape', () => {
  it('declares every panel state with a unique id and key', () => {
    const ids = Object.keys(PANEL_ENTRIES)
    expect(ids.length).toBeGreaterThan(0)
    expect(new Set(ids).size).toBe(ids.length)
    const keys = ids.map((id) => PANEL_ENTRIES[id].key)
    expect(new Set(keys).size).toBe(keys.length)
  })

  it('pins the PanelId union to the registry keys', () => {
    // PanelId is a hand-written union; this is what keeps it from drifting from
    // PANEL_ENTRIES (the compile-time typo guard depends on them matching).
    const declared: PanelId[] = [
      'leftCollapsed', 'chatCollapsed', 'splitRatio', 'sessionSidebar',
      'tocDockOpen', 'tocDockWidth', 'tocDockSide', 'fmPreviewRatio',
      'fmPreviewOpen', 'gitBranchCollapsed', 'gitWorktreeCollapsed',
      'gitManageTab', 'leftTab',
    ]
    expect(new Set(declared)).toEqual(new Set(Object.keys(PANEL_ENTRIES)))
  })

  it('marks only the left tab as pinned to the project', () => {
    // Every other entry follows the layout-memory switch. leftTab must not:
    // "remember layout globally" cannot make every project share one tab
    // (issue #474). Pinned by identity, not by count, so adding a new entry
    // cannot silently weaken the rule.
    const pinned = Object.values(PANEL_ENTRIES)
      .filter((e) => e.followsLayoutMemory === false)
      .map((e) => e.id)
    expect(pinned).toEqual(['leftTab'])
    expect(PANEL_ENTRIES.leftTab.scope).toBe('project')
  })
})

describe('physicalKey', () => {
  it('prefixes global keys and leaves them project-independent', () => {
    expect(physicalKey('chatCollapsed')).toBe(PANEL_KEY_PREFIX + 'chat-collapsed')
    expect(physicalKey('chatCollapsed', ROOT_A)).toBe(PANEL_KEY_PREFIX + 'chat-collapsed')
  })

  it('appends the project root for per-project keys', () => {
    expect(physicalKey('leftTab', ROOT_A)).toBe(PANEL_KEY_PREFIX + 'left-tab:' + ROOT_A)
  })

  it('returns null for a per-project key with no root', () => {
    expect(physicalKey('leftTab')).toBeNull()
    expect(physicalKey('leftTab', '')).toBeNull()
  })

  it('returns null for an unknown id', () => {
    expect(physicalKey('not-a-state')).toBeNull()
  })
})

describe('readPanelState / writePanelState round-trip', () => {
  beforeEach(() => localStorage.clear())

  it('round-trips a boolean encoded as 1/0', () => {
    expect(readPanelState('chatCollapsed')).toBe(false)
    writePanelState('chatCollapsed', true)
    expect(localStorage.getItem(PANEL_KEY_PREFIX + 'chat-collapsed')).toBe('1')
    expect(readPanelState('chatCollapsed')).toBe(true)
    writePanelState('chatCollapsed', false)
    expect(readPanelState('chatCollapsed')).toBe(false)
  })

  it('round-trips a number', () => {
    writePanelState('splitRatio', 0.62)
    expect(localStorage.getItem(PANEL_KEY_PREFIX + 'split-ratio')).toBe('0.62')
    expect(readPanelState('splitRatio')).toBe(0.62)
  })

  it('round-trips a string', () => {
    writePanelState('tocDockSide', 'left')
    expect(readPanelState('tocDockSide')).toBe('left')
  })

  it('round-trips the session sidebar JSON blob', () => {
    writePanelState('sessionSidebar', { open: false, width: 340 })
    expect(readPanelState('sessionSidebar')).toEqual({ open: false, width: 340 })
  })

  it('returns the default when nothing is stored', () => {
    expect(readPanelState('chatCollapsed')).toBe(false)
    expect(readPanelState('leftCollapsed')).toBe(DEFAULT_LEFT_COLLAPSED)
    expect(readPanelState('splitRatio')).toBe(DEFAULT_RATIO)
    expect(readPanelState('tocDockWidth')).toBe(DEFAULT_TOC_DOCK_WIDTH)
    expect(readPanelState('tocDockSide')).toBe(DEFAULT_TOC_DOCK_SIDE)
    expect(readPanelState('fmPreviewRatio')).toBe(DEFAULT_FM_PREVIEW_RATIO)
    expect(readPanelState('sessionSidebar')).toEqual({ open: true, width: DEFAULT_SESSION_SIDEBAR_WIDTH })
  })

  it('falls back to the default on a corrupted value', () => {
    localStorage.setItem(PANEL_KEY_PREFIX + 'split-ratio', 'not-a-number')
    expect(readPanelState('splitRatio')).toBe(DEFAULT_RATIO)

    localStorage.setItem(PANEL_KEY_PREFIX + 'session-sidebar', '{not-json')
    expect(readPanelState('sessionSidebar')).toEqual({ open: true, width: DEFAULT_SESSION_SIDEBAR_WIDTH })
  })

  it('parses a non-1 boolean as false, NOT as the default', () => {
    // The bool codec is `raw === '1'`, so garbage yields false even for an entry
    // whose default is true. Asserting on a default-false entry (chatCollapsed)
    // cannot tell the two apart — use leftCollapsed, whose default is true, so
    // the assertion actually distinguishes "coerced to false" from "fell back".
    expect(DEFAULT_LEFT_COLLAPSED).toBe(true)
    localStorage.setItem(PANEL_KEY_PREFIX + 'left-collapsed', 'yes')
    expect(readPanelState('leftCollapsed')).toBe(false)
    localStorage.setItem(PANEL_KEY_PREFIX + 'left-collapsed', '0')
    expect(readPanelState('leftCollapsed')).toBe(false)
  })

  it('round-trips the tri-state boolean, including the "never chosen" value', () => {
    // The nullable codec exists so "the user never chose" stays distinguishable
    // from "the user chose false" — the component's own `initialCollapsed` prop
    // only applies in the former case. serialize(null) writes '', so parse('')
    // must come back as the default, not as a recorded `false`.
    for (const value of [true, false, null] as const) {
      writePanelState('gitBranchCollapsed', value)
      expect(readPanelState<boolean | null>('gitBranchCollapsed')).toBe(value)
    }
    // An explicitly empty value means "no record".
    localStorage.setItem(PANEL_KEY_PREFIX + 'git-branch-collapsed', '')
    expect(readPanelState('gitBranchCollapsed')).toBeNull()
  })

  it('returns an independent default object for the JSON entry', () => {
    // parse must not hand out the registry's own constant, or a caller mutating
    // the result would corrupt every later default read.
    const a = readPanelState<{ open: boolean; width: number }>('sessionSidebar')
    a.open = false
    a.width = 999
    expect(readPanelState('sessionSidebar')).toEqual({ open: true, width: DEFAULT_SESSION_SIDEBAR_WIDTH })
  })
})

describe('writePanelState result', () => {
  beforeEach(() => localStorage.clear())

  it('reports true on a landed write and false when there is no key', () => {
    expect(writePanelState('chatCollapsed', true)).toBe(true)
    // A per-project entry with no root has no physical key → nothing written.
    expect(writePanelState('leftTab', 'tasks', '')).toBe(false)
  })

  it('reports false when storage throws', () => {
    const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('quota exceeded')
    })
    try {
      expect(writePanelState('chatCollapsed', true)).toBe(false)
    } finally {
      spy.mockRestore()
    }
  })
})

describe('per-project isolation', () => {
  beforeEach(() => localStorage.clear())

  it('keeps each project\'s value in its own key', () => {
    writePanelState('leftTab', 'view', ROOT_A)
    writePanelState('leftTab', 'tasks', ROOT_B)
    expect(readPanelState('leftTab', ROOT_A)).toBe('view')
    expect(readPanelState('leftTab', ROOT_B)).toBe('tasks')
  })

  it('returns the default and writes nothing when the root is empty', () => {
    // store.state.projectRoot is '' before the project loads, so a write must
    // not land in a key like '...left-tab:' that no reader would look up.
    // The per-project default is null — "no record" — which resolvePanelTab
    // turns into the layout default.
    expect(readPanelState('leftTab', '')).toBeNull()
    writePanelState('leftTab', 'tasks', '')
    expect(localStorage.getItem(PANEL_KEY_PREFIX + 'left-tab:')).toBeNull()
    expect(Object.keys(localStorage).filter((k) => k.startsWith(PANEL_KEY_PREFIX))).toHaveLength(0)
  })
})

describe('runtime scope (layout memory switch)', () => {
  beforeEach(() => {
    localStorage.clear()
    _resetLayoutScopeForTest()
  })
  afterEach(() => _resetLayoutScopeForTest())

  it('defaults to global scope', () => {
    expect(getLayoutScopeMode()).toBe('global')
    expect(getLayoutProjectRoot()).toBe('')
  })

  it('keeps every following entry global under the default mode', () => {
    setLayoutProjectRoot(ROOT_A)
    for (const entry of Object.values(PANEL_ENTRIES)) {
      if (entry.id === 'leftTab') continue
      expect(effectiveScope(entry), entry.id).toBe('global')
    }
    // leftTab is pinned and needs a root — which we now have.
    expect(physicalKey('leftTab')).toBe(PANEL_KEY_PREFIX + 'left-tab:' + ROOT_A)
  })

  it('switches every following entry to the project key', () => {
    setLayoutScopeMode('project')
    setLayoutProjectRoot(ROOT_A)
    expect(physicalKey('splitRatio')).toBe(PANEL_KEY_PREFIX + 'split-ratio:' + ROOT_A)
    expect(physicalKey('sessionSidebar')).toBe(PANEL_KEY_PREFIX + 'session-sidebar:' + ROOT_A)
  })

  it('leaves leftTab per-project in BOTH modes', () => {
    setLayoutProjectRoot(ROOT_A)
    expect(physicalKey('leftTab')).toBe(PANEL_KEY_PREFIX + 'left-tab:' + ROOT_A)
    setLayoutScopeMode('project')
    expect(physicalKey('leftTab')).toBe(PANEL_KEY_PREFIX + 'left-tab:' + ROOT_A)
  })

  it('returns no key for project-scoped entries while the root is unknown', () => {
    // Before the workspace loads there is no root. Reads must fall back to
    // defaults rather than writing to a key like '...split-ratio:'.
    setLayoutScopeMode('project')
    expect(physicalKey('splitRatio')).toBeNull()
    expect(readPanelState<number>('splitRatio')).toBe(DEFAULT_RATIO)
    expect(writePanelState('splitRatio', 0.7)).toBe(false)
    expect(Object.keys(localStorage).filter((k) => k.startsWith(PANEL_KEY_PREFIX))).toHaveLength(0)
  })

  it('bumps the epoch on a scope switch and on a root change', () => {
    const start = layoutEpoch.value
    setLayoutScopeMode('project')
    expect(layoutEpoch.value).toBe(start + 1)
    setLayoutProjectRoot(ROOT_A)
    expect(layoutEpoch.value).toBe(start + 2)
  })

  it('does not bump the epoch for a no-op call', () => {
    // Redundant bumps would re-read every layout composable on every project
    // switch notification; the root watcher fires more often than it changes.
    const start = layoutEpoch.value
    setLayoutScopeMode('global')
    setLayoutProjectRoot('')
    expect(layoutEpoch.value).toBe(start)
  })

  it('seeds the project key from the global value when switching to project', () => {
    // The user's choice: flipping to per-project must not reset every project
    // to factory defaults — the layout on screen is the one to start from.
    writePanelState('splitRatio', 0.62)
    writePanelState('chatCollapsed', true)
    setLayoutProjectRoot(ROOT_A)
    setLayoutScopeMode('project')

    expect(readPanelState<number>('splitRatio')).toBe(0.62)
    expect(readPanelState<boolean>('chatCollapsed')).toBe(true)
    // The global record survives as the seed for projects visited later.
    expect(localStorage.getItem(PANEL_KEY_PREFIX + 'split-ratio')).toBe('0.62')
  })

  it('does not overwrite a project value that already exists', () => {
    writePanelState('splitRatio', 0.62)
    setLayoutProjectRoot(ROOT_A)
    setLayoutScopeMode('project')
    // The user then drags the split to 0.3 in project A.
    writePanelState('splitRatio', 0.3)
    // Back to global, change it, and return to project mode.
    setLayoutScopeMode('global')
    writePanelState('splitRatio', 0.8)
    setLayoutScopeMode('project')
    // Project A keeps its own choice; it must not be re-seeded from global.
    expect(readPanelState<number>('splitRatio')).toBe(0.3)
  })

  it('seeds each project independently as it is opened', () => {
    writePanelState('splitRatio', 0.62)
    setLayoutScopeMode('project')
    setLayoutProjectRoot(ROOT_A)
    expect(readPanelState<number>('splitRatio')).toBe(0.62)
    writePanelState('splitRatio', 0.2)
    setLayoutProjectRoot(ROOT_B)
    expect(readPanelState<number>('splitRatio')).toBe(0.62)
    setLayoutProjectRoot(ROOT_A)
    expect(readPanelState<number>('splitRatio')).toBe(0.2)
  })

  it('does not seed entries that are pinned to the project', () => {
    // leftTab's global key is not a meaningful seed: the per-project default
    // (null → layout default) must not be replaced by some other project's tab.
    localStorage.setItem(PANEL_KEY_PREFIX + 'left-tab', 'tasks')
    setLayoutProjectRoot(ROOT_A)
    setLayoutScopeMode('project')
    expect(readPanelState<string | null>('leftTab')).toBeNull()
  })

  it('does not seed when there is no root yet', () => {
    writePanelState('splitRatio', 0.62)
    setLayoutScopeMode('project')
    expect(Object.keys(localStorage).filter((k) => k.includes(':') && k.startsWith(PANEL_KEY_PREFIX))).toHaveLength(0)
  })

  it('seeds when the root arrives after the switch', () => {
    // Startup order: the mode is restored from settings before the workspace
    // loads, so the root always arrives second. Seeding must still happen.
    writePanelState('splitRatio', 0.62)
    setLayoutScopeMode('project')
    setLayoutProjectRoot(ROOT_A)
    expect(readPanelState<number>('splitRatio')).toBe(0.62)
  })

  it('survives a storage write failure while seeding', () => {
    writePanelState('splitRatio', 0.62)
    const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('quota exceeded')
    })
    try {
      expect(() => {
        setLayoutScopeMode('project')
        setLayoutProjectRoot(ROOT_A)
      }).not.toThrow()
    } finally {
      spy.mockRestore()
    }
  })

  it('keeps seedProjectFromGlobal a no-op under global scope', () => {
    writePanelState('splitRatio', 0.62)
    setLayoutProjectRoot(ROOT_A)
    seedProjectFromGlobal()
    expect(localStorage.getItem(PANEL_KEY_PREFIX + 'split-ratio:' + ROOT_A)).toBeNull()
  })

  it('reads a different value per project in project mode', () => {
    // The end-to-end property: one shared setting, two independent layouts.
    writePanelState('sessionSidebar', { open: true, width: 280 })
    setLayoutProjectRoot(ROOT_A)
    setLayoutScopeMode('project')
    writePanelState('sessionSidebar', { open: false, width: 400 })
    setLayoutProjectRoot(ROOT_B)
    expect(readPanelState('sessionSidebar')).toEqual({ open: true, width: 280 })
    setLayoutProjectRoot(ROOT_A)
    expect(readPanelState('sessionSidebar')).toEqual({ open: false, width: 400 })
    // ...and switching back to global ignores both.
    setLayoutScopeMode('global')
    expect(readPanelState('sessionSidebar')).toEqual({ open: true, width: 280 })
  })
})

describe('legacy key migration', () => {
  beforeEach(() => {
    localStorage.clear()
    _resetLayoutScopeForTest()
  })
  it('copies every legacy global key to its new key and drops the old one', () => {
    const cases: Array<[string, string, string]> = [
      ['clawbench-widescreen-chat-collapsed', '1', PANEL_KEY_PREFIX + 'chat-collapsed'],
      ['clawbench-widescreen-split-ratio', '0.55', PANEL_KEY_PREFIX + 'split-ratio'],
      ['clawbench-session-sidebar', '{"open":false,"width":340}', PANEL_KEY_PREFIX + 'session-sidebar'],
      ['clawbench.tocDock.open', '1', PANEL_KEY_PREFIX + 'toc-dock-open'],
      ['clawbench.tocDock.width', '320', PANEL_KEY_PREFIX + 'toc-dock-width'],
      ['clawbench.tocDock.side', 'left', PANEL_KEY_PREFIX + 'toc-dock-side'],
      ['clawbench-fm-preview-split-ratio', '0.42', PANEL_KEY_PREFIX + 'fm-preview-ratio'],
    ]
    for (const [oldKey, raw] of cases) localStorage.setItem(oldKey, raw)

    migrateLegacyKeys()

    for (const [oldKey, raw, newKey] of cases) {
      expect(localStorage.getItem(newKey), newKey).toBe(raw)
      expect(localStorage.getItem(oldKey), oldKey).toBeNull()
    }
    // And the registry reads the migrated values back through its decoders.
    expect(readPanelState('chatCollapsed')).toBe(true)
    expect(readPanelState('splitRatio')).toBe(0.55)
    expect(readPanelState('sessionSidebar')).toEqual({ open: false, width: 340 })
    expect(readPanelState('tocDockSide')).toBe('left')
    expect(readPanelState('fmPreviewRatio')).toBe(0.42)
  })

  it('migrates the bare git keys into the registry', () => {
    // These three lived outside the registry (and the git collapse keys used
    // 'true'/'false' rather than the registry's '1'/'0'), so they are the one
    // migration where the encoding actually differs and a naive copy would
    // store "collapsed: false" for a collapsed panel.
    localStorage.setItem('git-branch-collapsed', 'true')
    localStorage.setItem('git-worktree-collapsed', 'false')
    localStorage.setItem('git-manage-active-tab', 'tags')

    migrateLegacyKeys()

    expect(readPanelState('gitBranchCollapsed')).toBe(true)
    expect(readPanelState('gitWorktreeCollapsed')).toBe(false)
    expect(readPanelState('gitManageTab')).toBe('tags')
    expect(localStorage.getItem('git-branch-collapsed')).toBeNull()
    expect(localStorage.getItem('git-worktree-collapsed')).toBeNull()
    expect(localStorage.getItem('git-manage-active-tab')).toBeNull()
  })

  it('leaves the git collapse unrecorded when the legacy key was absent', () => {
    // null is "the user never chose", which is what lets the component's own
    // `initialCollapsed` prop decide. A migration that defaulted to false would
    // permanently override it.
    migrateLegacyKeys()
    expect(readPanelState('gitBranchCollapsed')).toBeNull()
    expect(readPanelState('gitWorktreeCollapsed')).toBeNull()
  })

  it('does not overwrite a new key that already exists', () => {    localStorage.setItem('clawbench-widescreen-chat-collapsed', '1')
    localStorage.setItem(PANEL_KEY_PREFIX + 'chat-collapsed', '0')

    migrateLegacyKeys()

    // The new value wins; the stale legacy key is still cleaned up.
    expect(localStorage.getItem(PANEL_KEY_PREFIX + 'chat-collapsed')).toBe('0')
    expect(localStorage.getItem('clawbench-widescreen-chat-collapsed')).toBeNull()
  })

  it('migrates the per-project tab key and drops the legacy key', () => {
    localStorage.setItem('clawbench-project-panel:' + ROOT_A, 'history')
    localStorage.setItem('clawbench-project-panel:' + ROOT_B, 'tasks')

    migrateLegacyKeys()

    expect(readPanelState('leftTab', ROOT_A)).toBe('history')
    expect(readPanelState('leftTab', ROOT_B)).toBe('tasks')
    expect(localStorage.getItem('clawbench-project-panel:' + ROOT_A)).toBeNull()
    expect(localStorage.getItem('clawbench-project-panel:' + ROOT_B)).toBeNull()
  })

  it('preserves the legacy key when the new write fails', () => {
    // The highest-value migration property: a failed copy (quota / disabled
    // storage) must NOT delete the only remaining copy of the user's setting.
    localStorage.setItem('clawbench-widescreen-chat-collapsed', '1')
    const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('quota exceeded')
    })
    try {
      migrateLegacyKeys()
    } finally {
      spy.mockRestore()
    }
    // The write never landed, so the legacy key must survive for a later retry.
    expect(localStorage.getItem('clawbench-widescreen-chat-collapsed')).toBe('1')
  })

  it('drops a bare legacy per-project key with an empty root', () => {
    // `clawbench-project-panel:` with nothing after it has no root, so it can
    // never be migrated — it must be cleaned up rather than re-scanned forever.
    localStorage.setItem('clawbench-project-panel:', 'browse')

    migrateLegacyKeys()

    expect(localStorage.getItem('clawbench-project-panel:')).toBeNull()
  })

  it('adopts the legacy per-project collapse when every record agrees', () => {
    // A user who had one project (the common case) keeps the column state they
    // last saw instead of silently reverting to the default.
    localStorage.setItem('clawbench-project-panel-collapsed:' + ROOT_A, '0')

    migrateLegacyKeys()

    expect(readPanelState('leftCollapsed')).toBe(false)
    expect(localStorage.getItem('clawbench-project-panel-collapsed:' + ROOT_A)).toBeNull()
  })

  it('does not adopt conflicting per-project collapse records', () => {
    // No correct global answer exists, so fall back to the default rather than
    // inventing one (the old code always deleted these). The records are chosen
    // so that EITHER one, if wrongly adopted, differs from the default —
    // otherwise the assertion would pass even with the conflict check removed.
    expect(DEFAULT_LEFT_COLLAPSED).toBe(true)
    localStorage.setItem('clawbench-project-panel-collapsed:' + ROOT_A, '0')
    localStorage.setItem('clawbench-project-panel-collapsed:' + ROOT_B, '1')

    migrateLegacyKeys()

    expect(readPanelState('leftCollapsed')).toBe(DEFAULT_LEFT_COLLAPSED)
    expect(localStorage.getItem('clawbench-project-panel-collapsed:' + ROOT_A)).toBeNull()
    expect(localStorage.getItem('clawbench-project-panel-collapsed:' + ROOT_B)).toBeNull()
  })

  it('does not overwrite an existing global collapse with a legacy record', () => {
    localStorage.setItem(PANEL_KEY_PREFIX + 'left-collapsed', '1')
    localStorage.setItem('clawbench-project-panel-collapsed:' + ROOT_A, '0')

    migrateLegacyKeys()

    expect(readPanelState('leftCollapsed')).toBe(true)
  })

  it('drops the dead per-project collapsed keys without migrating them', () => {
    // The collapse flag is global now and these per-project records have no
    // unambiguous global source, so they are deleted rather than promoted.
    localStorage.setItem('clawbench-project-panel-collapsed:' + ROOT_A, '1')

    migrateLegacyKeys()

    expect(localStorage.getItem('clawbench-project-panel-collapsed:' + ROOT_A)).toBeNull()
    expect(readPanelState('leftCollapsed')).toBe(DEFAULT_LEFT_COLLAPSED)
  })

  it('is idempotent', () => {
    localStorage.setItem('clawbench-widescreen-chat-collapsed', '1')
    migrateLegacyKeys()
    const after = localStorage.getItem(PANEL_KEY_PREFIX + 'chat-collapsed')
    migrateLegacyKeys()
    expect(localStorage.getItem(PANEL_KEY_PREFIX + 'chat-collapsed')).toBe(after)
  })
})
