import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent } from 'vue'
import DockPluginSetting from '@/components/settings/DockPluginSetting.vue'
import { DOCK_TABS } from '@/composables/dockTabs'
import {
  _resetDockPluginsForTest,
  isDockTabHidden,
  setDockTabVisible,
  useDockPlugins,
} from '@/composables/useDockPlugins'
import * as settingsMod from '@/composables/useSettingsConfig'
import { isDockPluginAvailable } from '@/composables/dockPluginRuntime'
import { syncFeatureConfig } from '@/composables/useFeatureFlags'
import { useTerminalStatus } from '@/composables/useTerminalStatus'
import { categoryItems } from '@/components/settings/settingsFieldMap'
import zh from '@/i18n/locales/zh'

// The real component manipulates DOM/Sortable; the row assertions only need
// the slot rendered. Reorder persistence is covered in useDockPlugins.test.ts.
vi.mock('vue-draggable-plus', () => ({
  VueDraggable: defineComponent({
    name: 'VueDraggable',
    props: ['modelValue', 'handle'],
    emits: ['update:modelValue', 'end'],
    template: `<div class="vdp-stub"><slot /></div>`,
  }),
}))

// The manager reads the server switches and writes them back. Mock the config
// module so the test owns both directions (and can make a write fail).
vi.mock('@/composables/useSettingsConfig', async () => {
  const { ref } = await import('vue')
  const { syncFeatureConfig } = await import('@/composables/useFeatureFlags')
  const serverConfig = ref<Record<string, unknown>>({})
  const setServerValue = vi.fn(async (path: string, value: unknown) => {
    const [section, key] = path.split('.')
    const group = (serverConfig.value[section] ??= {}) as Record<string, unknown>
    group[key] = value
    // The real implementation reloads the config after a PATCH, and loadConfig
    // is what pushes the snapshot into useFeatureFlags.
    syncFeatureConfig(serverConfig.value)
    return { needsRestart: false, changedColdFields: [], warnings: [] }
  })
  const patchConfig = vi.fn(async () => ({
    needsRestart: false,
    changedColdFields: [],
    warnings: [],
  }))
  function getServerValueWithDefault(path: string): unknown {
    const [section, key] = path.split('.')
    const group = serverConfig.value[section] as Record<string, unknown> | undefined
    const value = group?.[key]
    return value === undefined ? true : value
  }
  return {
    serverConfig,
    useSettingsConfig: () => ({
      serverConfig,
      localConfig: {},
      getServerValueWithDefault,
      setServerValue,
      patchConfig,
    }),
    __mocks: { serverConfig, setServerValue, patchConfig, getServerValueWithDefault },
  }
})

// The dock runtime imports every feature's dock.ts; the manager only asks two
// questions of it (is this usable here, and why not).
vi.mock('@/composables/dockPluginRuntime', () => ({
  isDockPluginAvailable: vi.fn(() => true),
  dockUnavailableReasonKey: vi.fn((id: string) =>
    id === 'terminal' ? 'settings.dockPlugins.unavailable.terminal' : undefined,
  ),
}))

// A real confirmation dialog would wait forever with no DialogOverlay mounted.
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: confirmMock }),
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: toastShow }),
}))

const confirmMock = vi.hoisted(() => vi.fn())
const toastShow = vi.hoisted(() => vi.fn())

const mocks = (settingsMod as unknown as {
  __mocks: {
    serverConfig: { value: Record<string, unknown> }
    setServerValue: ReturnType<typeof vi.fn>
    patchConfig: ReturnType<typeof vi.fn>
  }
}).__mocks

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: { zh },
})

function mountSetting() {
  return mount(DockPluginSetting, {
    global: {
      plugins: [i18n],
      stubs: {
        SettingsCard: { template: '<section><slot /></section>' },
      },
    },
  })
}

function row(wrapper: ReturnType<typeof mountSetting>, id: string) {
  return wrapper.find(`[data-dock-plugin="${id}"]`)
}

/**
 * Seed the server config. The manager reads switches through
 * `useFeatureFlags`, which keeps a snapshot pushed by `loadConfig`, so the test
 * has to push it too.
 */
function setServerConfig(cfg: Record<string, unknown>) {
  mocks.serverConfig.value = cfg
  syncFeatureConfig(cfg)
}

describe('DockPluginSetting', () => {
  beforeEach(() => {
    localStorage.clear()
    _resetDockPluginsForTest()
    setServerConfig({})
    mocks.setServerValue.mockClear()
    mocks.patchConfig.mockClear()
    confirmMock.mockReset()
    confirmMock.mockResolvedValue(true)
    toastShow.mockReset()
  })

  it('renders the three presets and all 14 rows', () => {
    const wrapper = mountSetting()
    expect(wrapper.findAll('.preset-card')).toHaveLength(3)
    for (const tab of DOCK_TABS) {
      expect(row(wrapper, tab.id).exists(), `${tab.id} row missing`).toBe(true)
    }
    expect(DOCK_TABS).toHaveLength(14)
  })

  it('gives EVERY row a drag handle — permanent tabs and settings included', () => {
    const wrapper = mountSetting()
    for (const tab of DOCK_TABS) {
      expect(
        row(wrapper, tab.id).find('.plugin-drag-handle').exists(),
        `${tab.id} has no drag handle`,
      ).toBe(true)
    }
  })

  it('greys out the four permanent switches and labels them 常驻', () => {
    const wrapper = mountSetting()
    for (const id of ['browse', 'view', 'notifications', 'settings']) {
      const input = row(wrapper, id).find('input[type="checkbox"]')
      expect((input.element as HTMLInputElement).disabled, `${id} must be inert`).toBe(true)
      expect((input.element as HTMLInputElement).checked).toBe(true)
      expect(row(wrapper, id).find('.plugin-switch-label').text()).toBe('常驻')
    }
  })

  it('labels the two switch kinds differently', () => {
    const wrapper = mountSetting()
    // Server-backed feature → 启用.
    expect(row(wrapper, 'mail').find('.plugin-switch-label').text()).toBe('启用')
    // Display-only → 在 Dock 中显示.
    expect(row(wrapper, 'history').find('.plugin-switch-label').text()).toBe('在 Dock 中显示')
  })

  it('hides a display-only tab locally and marks the profile custom', async () => {
    const wrapper = mountSetting()
    await row(wrapper, 'history').find('input[type="checkbox"]').trigger('change')
    expect(isDockTabHidden('history')).toBe(true)
    expect(useDockPlugins().preset.value).toBe('custom')
    expect(mocks.setServerValue).not.toHaveBeenCalled()
  })

  it('writes the server switch for a feature tab', async () => {
    const wrapper = mountSetting()
    await row(wrapper, 'mail').find('input[type="checkbox"]').trigger('change')
    expect(mocks.setServerValue).toHaveBeenCalledWith('mail.enabled', false)
    // Turning mail off is reversible, so it must not ask.
    expect(confirmMock).not.toHaveBeenCalled()
    expect(useDockPlugins().preset.value).toBe('custom')
  })

  it('turns a feature back on and clears a local hide in the same action', async () => {
    setServerConfig({ mail: { enabled: false } })
    _resetDockPluginsForTest()
    // Hidden locally as well — the single switch has to undo both halves.
    setDockTabVisible('mail', false)
    const wrapper = mountSetting()
    await row(wrapper, 'mail').find('input[type="checkbox"]').trigger('change')
    expect(mocks.setServerValue).toHaveBeenCalledWith('mail.enabled', true)
    expect(isDockTabHidden('mail')).toBe(false)
  })

  it('asks before switching off a destructive feature and does nothing on cancel', async () => {
    confirmMock.mockResolvedValueOnce(false)
    const wrapper = mountSetting()
    await row(wrapper, 'forge').find('input[type="checkbox"]').trigger('change')
    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(mocks.setServerValue).not.toHaveBeenCalled()
  })

  it('confirms every destructive feature, including tasks and sharing', async () => {
    const wrapper = mountSetting()
    for (const id of ['terminal', 'tasks', 'share']) {
      confirmMock.mockClear()
      await row(wrapper, id).find('input[type="checkbox"]').trigger('change')
      expect(confirmMock, `${id} must ask first`).toHaveBeenCalledTimes(1)
    }
  })

  it('keeps the switch on and toasts when the server write fails', async () => {
    mocks.setServerValue.mockRejectedValueOnce(new Error('offline'))
    const wrapper = mountSetting()
    await row(wrapper, 'mail').find('input[type="checkbox"]').trigger('change')
    expect(toastShow).toHaveBeenCalled()
    // The cached config is unchanged, so the row still reads as enabled.
    expect(isDockTabHidden('mail')).toBe(false)
    expect((row(wrapper, 'mail').find('input[type="checkbox"]').element as HTMLInputElement).checked).toBe(true)
  })

  it('disables the switch and explains why when the host cannot run the feature', () => {
    vi.mocked(isDockPluginAvailable).mockReturnValue(false)
    try {
      const wrapper = mountSetting()
      const input = row(wrapper, 'terminal').find('input[type="checkbox"]')
      expect((input.element as HTMLInputElement).disabled).toBe(true)
      expect(row(wrapper, 'terminal').find('.plugin-row__reason').text()).toContain('终端')
    } finally {
      vi.mocked(isDockPluginAvailable).mockReturnValue(true)
    }
  })

  it('also greys terminal when the OS has no PTY but the server reports it enabled', () => {
    // Windows with `terminal.enabled: true`: the runtime says available, the
    // dock deliberately keeps the tab (its panel shows an "unsupported" state),
    // and only the platform flag reveals that the switch cannot work.
    const { platformSupported } = useTerminalStatus()
    platformSupported.value = false
    try {
      const wrapper = mountSetting()
      const input = row(wrapper, 'terminal').find('input[type="checkbox"]')
      expect((input.element as HTMLInputElement).disabled).toBe(true)
      expect(row(wrapper, 'terminal').find('.plugin-row__reason').text()).toContain('终端')
    } finally {
      platformSupported.value = null
    }
  })

  it('applies the office preset in ONE request after confirmation', async () => {
    const wrapper = mountSetting()
    await wrapper.findAll('.preset-card')[1].trigger('click')
    await vi.waitFor(() => expect(mocks.patchConfig).toHaveBeenCalledTimes(1))
    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(mocks.patchConfig).toHaveBeenCalledWith({
      forge: { enabled: false },
      mail: { enabled: true },
      terminal: { enabled: false },
      kb: { enabled: true },
      tasks: { enabled: true },
      share: { enabled: true },
    })
    expect(useDockPlugins().preset.value).toBe('standard')
    expect(isDockTabHidden('forge')).toBe(true)
  })

  it('applies nothing when the preset confirmation is cancelled', async () => {
    confirmMock.mockResolvedValueOnce(false)
    const wrapper = mountSetting()
    await wrapper.findAll('.preset-card')[1].trigger('click')
    expect(mocks.patchConfig).not.toHaveBeenCalled()
    expect(useDockPlugins().preset.value).toBe('developer')
    expect(isDockTabHidden('forge')).toBe(false)
  })

  it('rolls back the whole preset when the request fails', async () => {
    mocks.patchConfig.mockRejectedValueOnce(new Error('offline'))
    const wrapper = mountSetting()
    await wrapper.findAll('.preset-card')[2].trigger('click')
    await vi.waitFor(() => expect(toastShow).toHaveBeenCalled())
    const state = useDockPlugins()
    expect(state.preset.value).toBe('developer')
    expect(state.hidden.value.size).toBe(0)
    expect(isDockTabHidden('mail')).toBe(false)
  })

  it('applies the developer preset from the reset button without a confirmation', async () => {
    const wrapper = mountSetting()
    await wrapper.findAll('.preset-card')[2].trigger('click') // minimal, confirmed
    await vi.waitFor(() => expect(useDockPlugins().preset.value).toBe('minimal'))
    confirmMock.mockClear()

    // The preset write is still in flight for a tick after the local state
    // changes; the buttons stay disabled until it settles.
    const reset = wrapper.findAll('button.fbtn')[0]
    await vi.waitFor(() => expect(reset.attributes('disabled')).toBeUndefined())
    await reset.trigger('click')
    await vi.waitFor(() => expect(useDockPlugins().preset.value).toBe('developer'))
    expect(confirmMock).not.toHaveBeenCalled()
  })

  it('emits navigate from the gear button of an enabled feature', async () => {
    const wrapper = mountSetting()
    const gear = row(wrapper, 'mail').find('.plugin-gear')
    expect(gear.attributes('aria-label')).toBe('功能设置')
    await gear.trigger('click')
    expect(wrapper.emitted('navigate')).toEqual([['mail']])
  })

  it('only offers gears that lead to a real settings category', () => {
    // A category id that does not exist in settingsFieldMap renders an empty
    // page, so the gear would look live and do nothing. The id must be declared
    // there; an empty item list is legitimate (notifications is a dedicated
    // panel, not a field list), so only presence is asserted.
    const wrapper = mountSetting()
    for (const tab of DOCK_TABS) {
      if (!tab.settingsCategory) continue
      expect(
        categoryItems[tab.settingsCategory],
        `${tab.id} → ${tab.settingsCategory} is not a settings category`,
      ).toBeTruthy()
      expect(
        row(wrapper, tab.id).find('.plugin-gear').exists(),
        `${tab.id} declares a settings page but has no gear`,
      ).toBe(true)
    }
  })

  it('reaches the git history page from its row', async () => {
    const wrapper = mountSetting()
    await row(wrapper, 'history').find('.plugin-gear').trigger('click')
    expect(wrapper.emitted('navigate')).toEqual([['gitHistory']])
  })

  it('hides the config entry when the feature is switched off', () => {
    setServerConfig({ mail: { enabled: false } })
    const wrapper = mountSetting()
    expect(row(wrapper, 'mail').find('.plugin-gear').exists()).toBe(false)
  })
})
