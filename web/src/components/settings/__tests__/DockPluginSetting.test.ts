import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import DockPluginSetting from '@/components/settings/DockPluginSetting.vue'
import { DOCK_TABS } from '@/composables/dockTabs'
import {
  _resetDockPluginsForTest,
  isDockTabHidden,
  useDockPlugins,
} from '@/composables/useDockPlugins'
import zh from '@/i18n/locales/zh'

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

describe('DockPluginSetting', () => {
  beforeEach(() => {
    localStorage.clear()
    _resetDockPluginsForTest()
  })

  it('renders the three presets and every left-dock plugin', () => {
    const wrapper = mountSetting()
    expect(wrapper.findAll('.preset-card')).toHaveLength(3)
    for (const tab of DOCK_TABS) {
      expect(wrapper.find(`[data-dock-plugin="${tab.id}"]`).exists()).toBe(true)
    }
  })

  it('applies the standard preset', async () => {
    const wrapper = mountSetting()
    // Preset order: developer, standard, minimal.
    await wrapper.findAll('.preset-card')[1].trigger('click')
    expect(useDockPlugins().preset.value).toBe('standard')
    expect(isDockTabHidden('terminal')).toBe(true)
    expect(isDockTabHidden('forge')).toBe(true)
  })

  it('hides a plugin through its switch and marks the profile custom', async () => {
    const wrapper = mountSetting()
    const row = wrapper.find('[data-dock-plugin="mail"]')
    await row.find('input[type="checkbox"]').trigger('change')
    expect(isDockTabHidden('mail')).toBe(true)
    expect(useDockPlugins().preset.value).toBe('custom')
  })

  it('moves a reorderable plugin with the arrow buttons', async () => {
    const wrapper = mountSetting()
    const state = useDockPlugins()
    const before = [...state.order.value]
    const row = wrapper.find('[data-dock-plugin="mail"]')
    const buttons = row.findAll('.plugin-icon-btn')
    await buttons[0].trigger('click')
    expect(state.order.value.indexOf('mail')).toBe(before.indexOf('mail') - 1)
  })

  it('emits navigate for a plugin with its own settings page', async () => {
    const wrapper = mountSetting()
    const row = wrapper.find('[data-dock-plugin="mail"]')
    await row.find('.plugin-link').trigger('click')
    expect(wrapper.emitted('navigate')).toEqual([['mail']])
  })

  it('does not offer a visibility switch for a core tab', () => {
    const wrapper = mountSetting()
    const row = wrapper.find('[data-dock-plugin="browse"]')
    expect(row.find('input[type="checkbox"]').exists()).toBe(false)
    expect(row.find('.plugin-locked').exists()).toBe(true)
  })
})
