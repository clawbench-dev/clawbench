import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  _resetDockPluginsForTest,
  APPLICABLE_DOCK_PRESETS,
  DOCK_PRESET_PLANS,
  applyDockPreset,
  isDockTabHidden,
  markDockPresetCustom,
  resetDockPlugins,
  setDockTabOrder,
  setDockTabVisible,
  toggleDockTab,
  useDockPlugins,
  usePluginProfile,
} from '@/composables/useDockPlugins'
import { DOCK_TAB_IDS } from '@/composables/dockTabs'
import { DOCK_TAB_BY_ID } from '@/composables/dockPluginRuntime'
import { readPanelState } from '@/utils/panelState'

const PERMANENT = ['browse', 'view', 'settings']

describe('useDockPlugins defaults', () => {
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
    expect(state.order.value).toEqual(DOCK_TAB_IDS)
  })

  it('keeps the order list complete after a legacy partial order', () => {
    localStorage.setItem('clawbench-panel.dockOrder', JSON.stringify(['mail', 'forge']))
    _resetDockPluginsForTest()
    const state = useDockPlugins()
    expect(state.order.value).toHaveLength(DOCK_TAB_IDS.length)
    expect(state.order.value.slice(0, 3)).toEqual(['browse', 'view', 'history'])
  })
})

describe('presets', () => {
  beforeEach(() => {
    localStorage.clear()
    _resetDockPluginsForTest()
  })

  it('offers exactly the three applicable presets', () => {
    expect(APPLICABLE_DOCK_PRESETS).toEqual(['developer', 'standard', 'minimal'])
  })

  it('developer shows everything and turns every server feature on', async () => {
    await applyDockPreset('developer')
    const state = useDockPlugins()
    expect(state.preset.value).toBe('developer')
    expect(state.hidden.value.size).toBe(0)
    expect(state.order.value).toEqual(DOCK_TAB_IDS)
    expect(DOCK_PRESET_PLANS.developer.features.every((f) => f.value)).toBe(true)
  })

  it('office hides and switches off forge + terminal, keeps the rest on', async () => {
    await applyDockPreset('standard')
    const state = useDockPlugins()
    expect(state.preset.value).toBe('standard')
    expect(isDockTabHidden('forge')).toBe(true)
    expect(isDockTabHidden('terminal')).toBe(true)
    expect(isDockTabHidden('proxy')).toBe(true)
    expect(isDockTabHidden('mail')).toBe(false)
    expect(isDockTabHidden('tasks')).toBe(false)

    const features = Object.fromEntries(
      DOCK_PRESET_PLANS.standard.features.map((f) => [f.key, f.value]),
    )
    expect(features['forge.enabled']).toBe(false)
    expect(features['terminal.enabled']).toBe(false)
    expect(features['mail.enabled']).toBe(true)
    expect(features['kb.enabled']).toBe(true)
    expect(features['tasks.enabled']).toBe(true)
    expect(features['share.enabled']).toBe(true)
  })

  it('office order parks the hidden developer tabs before settings', async () => {
    await applyDockPreset('standard')
    expect(useDockPlugins().order.value).toEqual([
      'browse', 'view', 'history', 'tasks', 'stats', 'mail', 'kb', 'live',
      'share', 'notifications', 'forge', 'terminal', 'proxy', 'settings',
    ])
  })

  it('minimal keeps only the permanent tabs and turns every feature off', async () => {
    await applyDockPreset('minimal')
    const state = useDockPlugins()
    expect(state.preset.value).toBe('minimal')
    expect(state.visibleIds.value).toEqual(PERMANENT)
    for (const id of ['notifications', 'history', 'forge', 'tasks', 'terminal', 'stats', 'mail', 'kb', 'live', 'share', 'proxy']) {
      expect(isDockTabHidden(id), `${id} should be hidden`).toBe(true)
    }
    expect(DOCK_PRESET_PLANS.minimal.features.every((f) => !f.value)).toBe(true)
  })

  it('minimal hides notifications without adding a server switch for it', async () => {
    await applyDockPreset('minimal')
    // Display-only: the log keeps being written, so the tab must carry no
    // feature key and the plan must not try to switch one off.
    expect(DOCK_TAB_BY_ID.get('notifications')?.feature).toBeUndefined()
    expect(DOCK_PRESET_PLANS.minimal.features).toHaveLength(6)
  })

  it('parks the hidden tabs before settings so a re-enabled one lands in a stable slot', async () => {
    await applyDockPreset('minimal')
    const order = useDockPlugins().order.value
    const hidden = new Set(DOCK_PRESET_PLANS.minimal.hidden)
    // Only the permanent tabs are visible…
    expect(order.filter((id) => !hidden.has(id))).toEqual(['browse', 'view', 'settings'])
    // …and the hidden block keeps the DEFAULT relative order, parked right
    // before settings (the order is derived, never hand-written).
    expect(order.filter((id) => hidden.has(id))).toEqual(DOCK_TAB_IDS.filter((id) => hidden.has(id)))
    expect(order.at(-1)).toBe('settings')
  })

  it('commits every server switch in ONE call', async () => {
    const commit = vi.fn(async () => {})
    await applyDockPreset('minimal', commit)
    expect(commit).toHaveBeenCalledTimes(1)
    expect(commit).toHaveBeenCalledWith(DOCK_PRESET_PLANS.minimal.features)
  })

  it('applies NOTHING when the commit fails', async () => {
    const commit = vi.fn(async () => {
      throw new Error('network down')
    })
    await expect(applyDockPreset('minimal', commit)).rejects.toThrow('network down')
    const state = useDockPlugins()
    // Nothing local moved: the preset, the hidden set and the order are exactly
    // what they were before the failed attempt.
    expect(state.preset.value).toBe('developer')
    expect(state.hidden.value.size).toBe(0)
    expect(state.order.value).toEqual(DOCK_TAB_IDS)
  })

  it('does not leave a half-applied preset behind when the second attempt fails', async () => {
    await applyDockPreset('standard')
    await expect(
      applyDockPreset('minimal', async () => {
        throw new Error('boom')
      }),
    ).rejects.toThrow('boom')
    const state = useDockPlugins()
    expect(state.preset.value).toBe('standard')
    expect(isDockTabHidden('mail')).toBe(false)
    expect(state.order.value).toEqual(DOCK_PRESET_PLANS.standard.order)
  })
})

describe('manual changes', () => {
  beforeEach(() => {
    localStorage.clear()
    _resetDockPluginsForTest()
  })

  it('ignores stale hidden values for permanent tabs but honours notifications', () => {
    // The real key is `dock-hidden` (panelState prefixes `clawbench-panel.`);
    // writing `dockHidden` would store nothing and make this test vacuous.
    localStorage.setItem(
      'clawbench-panel.dock-hidden',
      JSON.stringify(['notifications', 'browse', 'settings']),
    )
    _resetDockPluginsForTest()
    expect(isDockTabHidden('browse')).toBe(false)
    expect(isDockTabHidden('settings')).toBe(false)
    expect(isDockTabHidden('notifications')).toBe(true)
    expect(useDockPlugins().visibleIds.value).not.toContain('notifications')
  })

  it('switches the profile to custom on a manual visibility change', () => {
    setDockTabVisible('mail', false)
    expect(isDockTabHidden('mail')).toBe(true)
    expect(useDockPlugins().preset.value).toBe('custom')
    setDockTabVisible('mail', true)
    expect(isDockTabHidden('mail')).toBe(false)
    expect(useDockPlugins().preset.value).toBe('custom')
  })

  it('refuses to hide a permanent tab', () => {
    setDockTabVisible('browse', false)
    expect(isDockTabHidden('browse')).toBe(false)
    expect(useDockPlugins().preset.value).toBe('developer')
  })

  it('toggle flips the current state', () => {
    toggleDockTab('tasks')
    expect(isDockTabHidden('tasks')).toBe(true)
    toggleDockTab('tasks')
    expect(isDockTabHidden('tasks')).toBe(false)
  })

  it('replaces the order wholesale after a drag, settings included', () => {
    const state = useDockPlugins()
    const ids = [...state.order.value]
    ids.splice(ids.indexOf('settings'), 1)
    ids.unshift('settings')
    setDockTabOrder(ids)
    expect(state.order.value[0]).toBe('settings')
    expect(state.preset.value).toBe('custom')
  })

  it('keeps the other tabs when the dragged list is partial', () => {
    setDockTabOrder(['mail'])
    const order = useDockPlugins().order.value
    expect(order).toHaveLength(DOCK_TAB_IDS.length)
    expect(order.slice(0, 3)).toEqual(['browse', 'view', 'history'])
  })

  it('marks the profile custom without touching visibility or order', () => {
    markDockPresetCustom()
    const state = useDockPlugins()
    expect(state.preset.value).toBe('custom')
    expect(state.hidden.value.size).toBe(0)
    expect(state.order.value).toEqual(DOCK_TAB_IDS)
  })
})

describe('persistence', () => {
  beforeEach(() => {
    localStorage.clear()
    _resetDockPluginsForTest()
  })

  it('persists visibility, order and preset', async () => {
    await applyDockPreset('standard')
    setDockTabOrder(['mail', ...DOCK_TAB_IDS.filter((id) => id !== 'mail')])
    setDockTabVisible('tasks', false)

    expect(readPanelState<string[]>('dockHidden')).toContain('tasks')
    expect(readPanelState<string>('dockPreset')).toBe('custom')
    expect(readPanelState<string[]>('dockOrder').length).toBeGreaterThan(0)

    // Re-read as a fresh page load would.
    _resetDockPluginsForTest()
    expect(isDockTabHidden('tasks')).toBe(true)
    expect(useDockPlugins().preset.value).toBe('custom')
  })

  it('reset restores the developer preset through the same commit path', async () => {
    const commit = vi.fn(async () => {})
    await applyDockPreset('minimal')
    await resetDockPlugins(commit)
    const state = useDockPlugins()
    expect(state.preset.value).toBe('developer')
    expect(state.hidden.value.size).toBe(0)
    expect(state.order.value).toEqual(DOCK_TAB_IDS)
    expect(commit).toHaveBeenCalledTimes(1)
  })
})

describe('usePluginProfile', () => {
  beforeEach(() => {
    localStorage.clear()
    _resetDockPluginsForTest()
  })

  it('exposes the preset to plugins', async () => {
    await applyDockPreset('standard')
    const profile = usePluginProfile()
    expect(profile.profile.value).toBe('standard')
    expect(profile.isStandard.value).toBe(true)
    expect(profile.isDeveloper.value).toBe(false)
  })
})
