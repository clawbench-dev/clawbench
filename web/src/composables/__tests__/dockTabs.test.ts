import { describe, expect, it } from 'vitest'
import {
  DOCK_TABS,
  REORDERABLE_DOCK_TABS,
  mergeDockOrder,
  primaryDockTabs,
  secondaryDockTabs,
  type DockTabId,
} from '@/composables/dockTabs'

const defaultSecondary = DOCK_TABS.filter((t) => !t.primary).map((t) => t.id)

describe('secondaryDockTabs ordering and visibility', () => {
  it('defaults to the registry order with settings last', () => {
    expect(secondaryDockTabs()).toEqual(defaultSecondary)
    expect(secondaryDockTabs().at(-1)).toBe('settings')
  })

  it('applies the user order before the runtime gates', () => {
    const order: DockTabId[] = ['mail', 'forge', 'tasks']
    const result = secondaryDockTabs({ order })
    expect(result.slice(0, 3)).toEqual(['mail', 'forge', 'tasks'])
    expect(result.at(-1)).toBe('settings')
  })

  it('drops hidden tabs but keeps them out of the aggregate order', () => {
    const hidden = new Set<DockTabId>(['mail', 'tasks'])
    const result = secondaryDockTabs({ hidden })
    expect(result).not.toContain('mail')
    expect(result).not.toContain('tasks')
    expect(result).toContain('kb')
  })

  it('always keeps settings last even when it is reordered by a stale value', () => {
    const order: DockTabId[] = ['settings', 'mail', 'tasks']
    const result = secondaryDockTabs({ order })
    expect(result.at(-1)).toBe('settings')
    expect(result.slice(0, 2)).toEqual(['mail', 'tasks'])
  })

  it('still applies the capability and feature gates', () => {
    expect(secondaryDockTabs({ terminalDisabled: true })).not.toContain('terminal')
    expect(secondaryDockTabs({ portForwardUnavailable: true })).not.toContain('proxy')
    expect(secondaryDockTabs({ forgeHidden: true })).not.toContain('forge')
    expect(secondaryDockTabs({ mailHidden: true })).not.toContain('mail')
    expect(secondaryDockTabs()).toContain('live')
  })

  it('keeps a hidden tab out even if it is also feature-disabled', () => {
    const hidden = new Set<DockTabId>(['mail'])
    expect(secondaryDockTabs({ hidden, mailHidden: true })).not.toContain('mail')
  })
})

describe('primaryDockTabs', () => {
  it('returns browse / view / history by default', () => {
    expect(primaryDockTabs()).toEqual(['browse', 'view', 'history'])
  })

  it('filters history when the user hides it', () => {
    expect(primaryDockTabs({ hidden: new Set<DockTabId>(['history']) })).toEqual([
      'browse',
      'view',
    ])
  })

  it('never hides a core tab even if the stored set is corrupt', () => {
    const hidden = new Set<DockTabId>(['browse', 'view', 'settings'])
    expect(primaryDockTabs({ hidden })).toEqual(['browse', 'view', 'history'])
    expect(secondaryDockTabs({ hidden })).toContain('settings')
  })
})

describe('mergeDockOrder', () => {
  it('drops unknown and duplicate ids and appends missing ones', () => {
    const merged = mergeDockOrder([
      'mail',
      'mail',
      'not-a-tab' as DockTabId,
      'forge',
    ])
    expect(merged.slice(0, 2)).toEqual(['mail', 'forge'])
    expect(merged).toHaveLength(REORDERABLE_DOCK_TABS.length)
    expect(new Set(merged).size).toBe(merged.length)
    for (const id of REORDERABLE_DOCK_TABS) expect(merged).toContain(id)
  })

  it('never includes primary tabs or settings', () => {
    const merged = mergeDockOrder(['history', 'settings', 'browse'] as DockTabId[])
    expect(merged).not.toContain('history')
    expect(merged).not.toContain('settings')
    expect(merged).not.toContain('browse')
  })
})
