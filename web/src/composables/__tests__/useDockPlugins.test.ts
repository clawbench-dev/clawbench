import { beforeEach, describe, expect, it } from 'vitest'
import {
  _resetDockPluginsForTest,
  applyDockPreset,
  isDockTabHidden,
  resetDockPlugins,
  setDockTabOrder,
  setDockTabVisible,
  toggleDockTab,
  useDockPlugins,
  usePluginProfile,
} from '@/composables/useDockPlugins'
import { readPanelState } from '@/utils/panelState'

describe('useDockPlugins', () => {
  beforeEach(() => {
    localStorage.clear()
    _resetDockPluginsForTest()
  })

  it('defaults to the developer preset with nothing hidden', () => {
    const state = useDockPlugins()
    expect(state.preset.value).toBe('developer')
    expect(state.hidden.value.size).toBe(0)
    expect(state.visibleIds.value).toContain('terminal')
    expect(state.visibleIds.value).toContain('forge')
  })

  it('standard hides exactly the dev-related tabs', () => {
    applyDockPreset('standard')
    const state = useDockPlugins()
    expect(state.preset.value).toBe('standard')
    expect(isDockTabHidden('forge')).toBe(true)
    expect(isDockTabHidden('terminal')).toBe(true)
    expect(isDockTabHidden('proxy')).toBe(true)
    expect(isDockTabHidden('mail')).toBe(false)
    expect(isDockTabHidden('tasks')).toBe(false)
  })

  it('minimal keeps only the permanent tabs visible', () => {
    applyDockPreset('minimal')
    const state = useDockPlugins()
    expect(state.preset.value).toBe('minimal')
    // Left dock: browse / view / notifications / settings stay; every closable
    // tab is hidden. Notifications is a permanent module now.
    expect(state.visibleIds.value).toEqual(['browse', 'view', 'notifications', 'settings'])
    expect(isDockTabHidden('history')).toBe(true)
    expect(isDockTabHidden('notifications')).toBe(false)
  })

  it('ignores stale hidden values for tabs that are now core', () => {
    // Notifications used to be closable; a user who hid it then must not lose
    // the tab (and with it every event log) after the upgrade.
    localStorage.setItem(
      'clawbench-panel.dockHidden',
      JSON.stringify(['notifications', 'browse', 'settings']),
    )
    _resetDockPluginsForTest()
    expect(isDockTabHidden('notifications')).toBe(false)
    expect(isDockTabHidden('browse')).toBe(false)
    expect(isDockTabHidden('settings')).toBe(false)
    expect(useDockPlugins().visibleIds.value).toContain('notifications')
  })

  it('manual visibility changes switch the profile to custom', () => {
    setDockTabVisible('mail', false)
    expect(isDockTabHidden('mail')).toBe(true)
    expect(useDockPlugins().preset.value).toBe('custom')
    setDockTabVisible('mail', true)
    expect(isDockTabHidden('mail')).toBe(false)
    expect(useDockPlugins().preset.value).toBe('custom')
  })

  it('refuses to hide a core tab', () => {
    setDockTabVisible('browse', false)
    expect(isDockTabHidden('browse')).toBe(false)
    // Core changes are no-ops, so the preset is untouched too.
    expect(useDockPlugins().preset.value).toBe('developer')
  })

  it('toggle flips the current state', () => {
    toggleDockTab('tasks')
    expect(isDockTabHidden('tasks')).toBe(true)
    toggleDockTab('tasks')
    expect(isDockTabHidden('tasks')).toBe(false)
  })

  it('replaces the order wholesale after a drag', () => {
    const state = useDockPlugins()
    const ids = [...state.order.value]
    ids.splice(ids.indexOf('mail'), 1)
    ids.push('mail')
    setDockTabOrder(ids)
    expect(state.order.value.at(-1)).toBe('mail')
    expect(state.preset.value).toBe('custom')

    // A partial list (one tab) must not drop the others.
    setDockTabOrder(['mail'])
    expect(state.order.value[0]).toBe('mail')
    expect(state.order.value).toHaveLength(state.reorderableIds.value.length)
  })

  it('persists visibility, order and preset', () => {
    applyDockPreset('standard')
    setDockTabOrder(['mail'])
    setDockTabVisible('tasks', false)

    expect(readPanelState<string[]>('dockHidden')).toContain('tasks')
    expect(readPanelState<string>('dockPreset')).toBe('custom')
    expect(readPanelState<string[]>('dockOrder').length).toBeGreaterThan(0)

    // Re-read as a fresh page load would.
    _resetDockPluginsForTest()
    expect(isDockTabHidden('tasks')).toBe(true)
    expect(useDockPlugins().preset.value).toBe('custom')
  })

  it('reset restores the developer preset', () => {
    applyDockPreset('minimal')
    resetDockPlugins()
    const state = useDockPlugins()
    expect(state.preset.value).toBe('developer')
    expect(state.hidden.value.size).toBe(0)
    expect(state.order.value).toEqual(state.reorderableIds.value)
  })

  it('exposes the preset to plugins through usePluginProfile', () => {
    applyDockPreset('standard')
    const profile = usePluginProfile()
    expect(profile.profile.value).toBe('standard')
    expect(profile.isStandard.value).toBe(true)
    expect(profile.isDeveloper.value).toBe(false)
  })
})
