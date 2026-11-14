import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent } from 'vue'
import DockPluginSetting from '@/components/settings/DockPluginSetting.vue'
import { DOCK_TABS } from '@/composables/dockTabs'
import {
  _resetDockPluginsForTest,
  isDockTabHidden,
  useDockPlugins,
} from '@/composables/useDockPlugins'
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

  it('exposes a drag handle only on reorderable plugins', () => {
    const wrapper = mountSetting()
    expect(wrapper.find('[data-dock-plugin="mail"] .plugin-drag-handle').exists()).toBe(true)
    expect(wrapper.find('[data-dock-plugin="browse"] .plugin-drag-handle').exists()).toBe(false)
    expect(wrapper.find('[data-dock-plugin="settings"] .plugin-drag-handle').exists()).toBe(false)
    // The old up/down buttons are gone.
    expect(wrapper.find('.plugin-icon-btn').exists()).toBe(false)
  })

  it('emits navigate from the gear button for a plugin with its own settings page', async () => {
    const wrapper = mountSetting()
    const row = wrapper.find('[data-dock-plugin="mail"]')
    const gear = row.find('.plugin-gear')
    expect(gear.text()).toBe('')
    expect(gear.attributes('aria-label')).toBe('功能设置')
    await gear.trigger('click')
    expect(wrapper.emitted('navigate')).toEqual([['mail']])
  })

  it('hides the config entry when the plugin is disabled', async () => {
    const wrapper = mountSetting()
    const row = wrapper.find('[data-dock-plugin="mail"]')
    expect(row.find('.plugin-gear').exists()).toBe(true)

    await row.find('input[type="checkbox"]').trigger('change')
    expect(wrapper.find('[data-dock-plugin="mail"] .plugin-gear').exists()).toBe(false)
  })

  it('keeps a disabled switch on core tabs so the row shape stays uniform', () => {
    const wrapper = mountSetting()
    const row = wrapper.find('[data-dock-plugin="browse"]')
    const input = row.find('input[type="checkbox"]')
    expect(input.exists()).toBe(true)
    expect((input.element as HTMLInputElement).disabled).toBe(true)
    expect((input.element as HTMLInputElement).checked).toBe(true)
    expect(row.find('.plugin-locked').exists()).toBe(false)
  })
})
