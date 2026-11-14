import { beforeEach, describe, expect, it } from 'vitest'
import {
  _resetDockPluginsForTest,
  applyDockPreset,
  isDockTabHidden,
  moveDockTab,
  resetDockPlugins,
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

  it('minimal keeps only the four core tabs visible', () => {
    applyDockPreset('minimal')
    const state = useDockPlugins()
    expect(state.preset.value).toBe('minimal')
    // Left dock: browse / view / settings stay; every closable tab is hidden.
    expect(state.visibleIds.value).toEqual(['browse', 'view', 'settings'])
    expect(isDockTabHidden('history')).toBe(true)
    expect(isDockTabHidden('notifications')).toBe(true)
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

  it('moves a reorderable tab and marks the profile custom', () => {
    const state = useDockPlugins()
    const before = [...state.order.value]
    moveDockTab('mail', -1)
    const after = state.order.value
    expect(after.indexOf('mail')).toBe(before.indexOf('mail') - 1)
    expect(state.preset.value).toBe('custom')
  })

  it('does not move the first tab up or the last tab down', () => {
    const state = useDockPlugins()
    const first = state.order.value[0]
    const last = state.order.value[state.order.value.length - 1]
    moveDockTab(first, -1)
    moveDockTab(last, 1)
    expect(state.order.value[0]).toBe(first)
    expect(state.order.value[state.order.value.length - 1]).toBe(last)
  })

  it('persists visibility, order and preset', () => {
    applyDockPreset('standard')
    moveDockTab('mail', -1)
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
