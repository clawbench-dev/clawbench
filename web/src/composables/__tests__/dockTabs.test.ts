import { describe, expect, it } from 'vitest'
import { DOCK_TAB_IDS, isDockTabId, mergeDockOrder, type DockTabId } from '@/composables/dockTabs'
import {
  DOCK_FEATURE_TABS,
  DOCK_TABS,
  DOCK_TAB_BY_ID,
  dockTabs,
} from '@/composables/dockPluginRuntime'

const PERMANENT = DOCK_TABS.filter((t) => !t.closable).map((t) => t.id)
const OPTIONAL = DOCK_TABS.filter((t) => t.closable).map((t) => t.id)

describe('dockTabs default order', () => {
  it('renders every left-column tab in registry order when nothing is hidden', () => {
    expect(dockTabs()).toEqual(DOCK_TAB_IDS)
  })

  it('keeps only browse / view / settings permanent', () => {
    // The fixed head is gone, but these three still cannot be hidden. Asserting
    // the set (not the order) keeps the test honest about what "permanent"
    // means now: no position is pinned any more.
    expect(new Set(PERMANENT)).toEqual(new Set(['browse', 'view', 'settings']))
  })

  it('lets the notification log be hidden without touching the server', () => {
    // The log is append-only and is still written when the entry is hidden, so
    // this must stay a display-only switch: no `feature`, and hiding it works.
    expect(OPTIONAL).toContain('notifications')
    expect(DOCK_TAB_BY_ID.get('notifications')?.feature).toBeUndefined()
    expect(
      dockTabs({ hidden: new Set<DockTabId>(['notifications']) }),
    ).not.toContain('notifications')
  })

  it('lets the sharing tab be hidden', () => {
    expect(OPTIONAL).toContain('share')
    expect(dockTabs({ hidden: new Set<DockTabId>(['share']) })).not.toContain('share')
  })
})

describe('dockTabs ordering', () => {
  it('follows a complete user order verbatim — settings included', () => {
    const order = [...DOCK_TAB_IDS].reverse()
    expect(dockTabs({ order })).toEqual(order)
    // settings is no longer forced to the end: it is just another row.
    expect(dockTabs({ order })[0]).toBe('settings')
  })

  it('keeps a tab that is not mentioned in its default slot', () => {
    // A partial order must never drop a tab — it just leaves the rest alone.
    const order: DockTabId[] = ['mail']
    const result = dockTabs({ order })
    expect(result).toHaveLength(DOCK_TAB_IDS.length)
    expect(new Set(result)).toEqual(new Set(DOCK_TAB_IDS))
  })

  it('drops hidden tabs but keeps their slot for when they come back', () => {
    const hidden = new Set<DockTabId>(['mail', 'tasks'])
    const result = dockTabs({ hidden })
    expect(result).not.toContain('mail')
    expect(result).not.toContain('tasks')
    expect(result).toContain('kb')
  })
})

describe('mergeDockOrder', () => {
  it('reproduces a complete stored order', () => {
    const stored: DockTabId[] = [
      'settings', 'browse', 'view', 'history', 'forge', 'tasks', 'terminal',
      'stats', 'mail', 'kb', 'live', 'share', 'proxy', 'notifications',
    ]
    expect(mergeDockOrder(stored)).toEqual(stored)
  })

  it('migrates the old movable-only order without moving the permanent tabs', () => {
    // Pre-upgrade `dockOrder` only ever held the reorderable tail. Appending it
    // (or starting the list from it) would push the file manager to the end;
    // the stored sequence must only reorder the slots those ids occupy.
    const legacy: DockTabId[] = [
      'mail', 'forge', 'tasks', 'terminal', 'stats', 'kb', 'live', 'share', 'proxy',
    ]
    const merged = mergeDockOrder(legacy)
    expect(merged.slice(0, 3)).toEqual(['browse', 'view', 'history'])
    expect(merged.at(-2)).toBe('notifications')
    expect(merged.at(-1)).toBe('settings')
    // The nine movable ids keep their slots but take the stored sequence.
    expect(merged.slice(3, 12)).toEqual(legacy)
    expect(merged).toHaveLength(DOCK_TAB_IDS.length)
  })

  it('drops unknown and duplicate ids', () => {
    const merged = mergeDockOrder([
      'mail',
      'mail',
      'not-a-tab' as DockTabId,
      'forge',
    ])
    expect(merged).toHaveLength(DOCK_TAB_IDS.length)
    expect(new Set(merged).size).toBe(merged.length)
    for (const id of DOCK_TAB_IDS) expect(merged).toContain(id)
  })

  it('falls back to the default order for an empty or absent value', () => {
    expect(mergeDockOrder([])).toEqual(DOCK_TAB_IDS)
    expect(mergeDockOrder()).toEqual(DOCK_TAB_IDS)
  })

  it('never drops a tab added after the order was saved', () => {
    const saved: DockTabId[] = ['mail', 'tasks']
    const merged = mergeDockOrder(saved)
    for (const id of DOCK_TAB_IDS) expect(merged).toContain(id)
  })
})

describe('dockTabs gates', () => {
  it('hides tabs whose server feature is switched off', () => {
    const featureDisabled = new Set<DockTabId>(['forge', 'share'])
    const result = dockTabs({ featureDisabled })
    expect(result).not.toContain('forge')
    expect(result).not.toContain('share')
    expect(result).toEqual(DOCK_TAB_IDS.filter((id) => id !== 'forge' && id !== 'share'))
  })

  it('declares the six server-switch tabs', () => {
    expect(new Set(DOCK_FEATURE_TABS)).toEqual(
      new Set(['forge', 'mail', 'terminal', 'kb', 'tasks', 'share']),
    )
  })

  it('applies the capability gates through the one unavailable set', () => {
    expect(dockTabs({ unavailable: new Set<DockTabId>(['terminal']) })).not.toContain('terminal')
    expect(dockTabs({ unavailable: new Set<DockTabId>(['proxy']) })).not.toContain('proxy')
  })

  it('keeps 会议 on every host (records are readable without capture)', () => {
    expect(dockTabs()).toContain('live')
    expect(
      dockTabs({ unavailable: new Set<DockTabId>(['terminal', 'proxy']) }),
    ).toContain('live')
  })

  it('drops runtime-unavailable tabs', () => {
    const unavailable = new Set<DockTabId>(['mail'])
    expect(dockTabs({ unavailable })).not.toContain('mail')
  })

  it('applies gates to the user order, not the other way round', () => {
    const order = [...DOCK_TAB_IDS].reverse()
    const result = dockTabs({
      order,
      unavailable: new Set<DockTabId>(['terminal']),
      hidden: new Set<DockTabId>(['share']),
    })
    expect(result).not.toContain('terminal')
    expect(result).not.toContain('share')
    expect(result[0]).toBe('settings')
  })
})

describe('dockTabs hidden set safety', () => {
  it('never hides a permanent tab even if the stored set is corrupt', () => {
    const hidden = new Set<DockTabId>(['browse', 'view', 'settings'])
    const result = dockTabs({ hidden })
    for (const id of PERMANENT) expect(result).toContain(id)
  })

  it('ignores unknown ids in the stored order', () => {
    expect(dockTabs({ order: ['nope' as DockTabId] })).toEqual(DOCK_TAB_IDS)
  })
})

describe('registry integrity', () => {
  it('has unique ids and every id is reachable by the type guard', () => {
    expect(new Set(DOCK_TAB_IDS).size).toBe(DOCK_TAB_IDS.length)
    for (const id of DOCK_TAB_IDS) expect(isDockTabId(id)).toBe(true)
  })

  it('has no chat entry — chat is the right-hand pane', () => {
    expect(isDockTabId('chat')).toBe(false)
  })

  it('points the history tab at its own settings page', () => {
    // The manager renders a gear for every tab that declares a category. A tab
    // whose settings page exists but is not declared here is unreachable from
    // the manager (the "双向可达" rule), which is how git history was missed.
    expect(DOCK_TAB_BY_ID.get('history')?.settingsCategory).toBe('gitHistory')
  })

  it('only the settings tab itself has no settings page', () => {
    const withoutPage = DOCK_TABS.filter((t) => !t.settingsCategory).map((t) => t.id)
    expect(withoutPage).toEqual(['settings'])
  })
})
