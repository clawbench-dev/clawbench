import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

const mockGet = vi.fn()

vi.mock('@/utils/api', () => ({
  apiGet: (...args: unknown[]) => mockGet(...args),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import ProviderModelsPicker from '@/components/settings/ProviderModelsPicker.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        items: {
          providerModelsTitle: '选择可用模型',
          providerModelsSearch: '搜索模型',
          providerModelsLoading: '正在获取模型…',
          providerModelsFromLive: '来自端点实测',
          providerModelsFromCatalog: '来自权威目录',
          providerModelsEmpty: '未能获取模型列表',
          providerModelsSelected: '已选 {count} 个',
          providerModelsNoMatch: '没有匹配的模型',
          providerModelsCustom: '自定义',
          providerModelsAdd: '添加',
        },
      },
    },
  },
})

function mountPicker(selected: string[] = []) {
  return mount(ProviderModelsPicker, {
    props: { open: true, providerId: 1, selected },
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

describe('ProviderModelsPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGet.mockResolvedValue({ models: ['m-a', 'm-b'], source: 'live' })
  })

  it('loads the endpoint list and reports its source', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    expect(mockGet).toHaveBeenCalledWith('/api/providers/1/models')
    expect(wrapper.text()).toContain('来自端点实测')
    expect(wrapper.findAll('.models-row')).toHaveLength(2)
  })

  it('toggles a model on and off', async () => {
    const wrapper = mountPicker([])
    await flushPromises()

    await wrapper.find('.models-row').trigger('click')
    expect(wrapper.emitted('update:selected')?.[0]).toEqual([['m-a']])

    await wrapper.setProps({ selected: ['m-a'] })
    await wrapper.findAll('.models-row')[0].trigger('click')
    expect(wrapper.emitted('update:selected')?.[1]).toEqual([[]])
  })

  it('keeps a saved id that the endpoint no longer lists', async () => {
    const wrapper = mountPicker(['legacy-model'])
    await flushPromises()
    const ids = wrapper.findAll('.models-id').map(n => n.text())
    expect(ids).toContain('legacy-model')
  })

  it('adds a typed model id', async () => {
    const wrapper = mountPicker([])
    await flushPromises()

    await wrapper.find('.models-custom-input').setValue('custom-model')
    await wrapper.find('.models-custom .fbtn').trigger('click')
    expect(wrapper.emitted('update:selected')?.[0]).toEqual([['custom-model']])
  })

  it('filters the list', async () => {
    const wrapper = mountPicker([])
    await flushPromises()
    await wrapper.find('.models-search .search-pill input').setValue('m-b')
    expect(wrapper.findAll('.models-row')).toHaveLength(1)
    expect(wrapper.find('.models-id').text()).toBe('m-b')
  })
})
