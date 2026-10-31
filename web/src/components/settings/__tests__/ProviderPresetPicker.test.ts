import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ProviderPresetPicker from '@/components/settings/ProviderPresetPicker.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        items: {
          providerPresetTitle: '选择供应商',
          providerPresetSearch: '搜索',
          providerPresetNone: '不使用预设',
          providerPresetEmpty: '没有匹配的供应商',
          providerNoBaseUrl: '未设置端点',
        },
      },
    },
  },
})

const presets = [
  { id: 'anthropic', name: 'Anthropic', protocol: 'anthropic', baseUrl: 'https://api.anthropic.com' },
  { id: 'deepseek', name: 'DeepSeek', protocol: 'openai', baseUrl: 'https://api.deepseek.com/v1' },
  { id: 'obscure-gw', name: 'Obscure Gateway', protocol: 'openai', baseUrl: 'https://gw.obscure.example/v1' },
]

function mountPicker(selectedId = '') {
  return mount(ProviderPresetPicker, {
    props: { open: true, presets, selectedId },
    global: {
      plugins: [i18n],
      stubs: {
        ModalDialog: {
          props: ['open'],
          template: '<div v-if="open"><slot name="header" /><slot /></div>',
        },
      },
    },
  })
}

describe('ProviderPresetPicker', () => {
  it('lists every preset with its protocol and host', () => {
    const wrapper = mountPicker()
    const rows = wrapper.findAll('.preset-row[data-preset-id]')
    expect(rows).toHaveLength(3)
    expect(rows[0].text()).toContain('Anthropic')
    expect(rows[0].text()).toContain('api.anthropic.com')
    expect(rows[0].text()).toContain('anthropic')
  })

  it('falls back to the initial letter when no brand icon exists', () => {
    const wrapper = mountPicker()
    const obscure = wrapper.findAll('.preset-row').find(r => r.attributes('data-preset-id') === 'obscure-gw')
    expect(obscure).toBeDefined()
    expect(obscure!.find('.provider-icon-initial').text()).toBe('O')
  })

  it('filters by name, id and endpoint', async () => {
    const wrapper = mountPicker()
    const search = wrapper.find('.preset-search .search-pill input')

    await search.setValue('deep')
    expect(wrapper.findAll('.preset-row[data-preset-id]')).toHaveLength(1)
    expect(wrapper.find('.preset-row[data-preset-id="deepseek"]').exists()).toBe(true)

    // The host is part of the match: two gateways may share a display name.
    await search.setValue('gw.obscure')
    expect(wrapper.findAll('.preset-row[data-preset-id]')).toHaveLength(1)

    await search.setValue('no-such-provider')
    expect(wrapper.findAll('.preset-row[data-preset-id]')).toHaveLength(0)
    expect(wrapper.find('.preset-empty').exists()).toBe(true)
  })

  it('emits the chosen preset and closes', async () => {
    const wrapper = mountPicker()
    await wrapper.find('.preset-row[data-preset-id="deepseek"]').trigger('click')

    expect(wrapper.emitted('select')?.[0]).toEqual([presets[1]])
    expect(wrapper.emitted('update:open')?.[0]).toEqual([false])
  })

  it('offers "no preset" and emits null', async () => {
    const wrapper = mountPicker('anthropic')
    const none = wrapper.find('.preset-row--none')
    expect(none.exists()).toBe(true)
    await none.trigger('click')
    expect(wrapper.emitted('select')?.[0]).toEqual([null])
  })

  it('marks the current selection', () => {
    const wrapper = mountPicker('anthropic')
    expect(wrapper.find('.preset-row[data-preset-id="anthropic"]').classes()).toContain('preset-row--active')
    expect(wrapper.find('.preset-row[data-preset-id="anthropic"] .preset-check').exists()).toBe(true)
  })
})
