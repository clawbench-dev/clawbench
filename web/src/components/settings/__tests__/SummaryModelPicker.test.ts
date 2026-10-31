import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref } from 'vue'

const mockGet = vi.fn()
const mockPatch = vi.fn()
const mockLoadConfig = vi.fn()
const mockToastShow = vi.fn()
const mockServerConfig = ref<Record<string, unknown>>({})

vi.mock('@/utils/api', () => ({
  apiGet: (...args: unknown[]) => mockGet(...args),
  apiPatch: (...args: unknown[]) => mockPatch(...args),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({ serverConfig: mockServerConfig, loadConfig: mockLoadConfig }),
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

import SummaryModelPicker from '@/components/settings/SummaryModelPicker.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        items: {
          summaryModelTitle: '选择摘要模型',
          summaryModelSearch: '搜索模型…',
          summaryModelNoProviders: '还没有可用的提供商，请先在「模型提供商」里添加。',
          summaryModelNoCompatibleProviders: '已配置的提供商都不是 openai / anthropic 协议，无法用于摘要。',
          summaryModelManualHint: '该提供商未提供模型清单，可直接输入模型 ID。',
          summaryModelProviderNoEndpoint: '该提供商没有 API 端点（仅注入密钥），不能用于摘要。',
          summaryModelProbeFailed: '未能获取模型清单（{reason}），可直接输入模型 ID。',
          summaryModelUse: '使用',
          providerModelsLoading: '正在获取模型…',
          providerModelsNoMatch: '没有匹配的模型',
          providerModelsCustom: '输入模型 id 后回车添加',
        },
      },
    },
  },
})

/** Route GETs by URL so one mount can serve both the provider list and the
 *  per-provider model resolution. */
function routeGet(routes: Record<string, unknown>) {
  mockGet.mockImplementation((url: string) => {
    if (!(url in routes)) return Promise.reject(new Error(`unexpected GET ${url}`))
    return Promise.resolve(routes[url])
  })
}

function mountPicker() {
  return mount(SummaryModelPicker, {
    props: { open: true },
    global: {
      plugins: [i18n],
      stubs: {
        BottomSheet: {
          props: ['open'],
          template: '<div v-if="open"><slot name="header" /><slot /></div>',
        },
      },
    },
  })
}

describe('SummaryModelPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockServerConfig.value = { ai_summary: { provider_id: 1, model: '' } }
    mockPatch.mockResolvedValue({})
  })

  it('offers a provider that curates no model list instead of hiding it', async () => {
    routeGet({
      '/api/providers': { providers: [{ id: 1, name: 'AI Summary', protocol: 'anthropic', baseUrl: 'https://gw.example/v1', models: [] }] },
      '/api/providers/1/models': { models: [] },
    })

    const wrapper = mountPicker()
    await flushPromises()

    expect(wrapper.text()).not.toContain('还没有可用的提供商')
    expect(wrapper.text()).toContain('AI Summary')
    expect(wrapper.text()).toContain('该提供商未提供模型清单')
    expect(wrapper.find('.summary-manual-input').exists()).toBe(true)
  })

  it('lists the models resolved for an uncatalogued provider', async () => {
    routeGet({
      '/api/providers': { providers: [{ id: 1, name: 'AI Summary', protocol: 'anthropic', baseUrl: 'https://gw.example/v1', models: [] }] },
      '/api/providers/1/models': { models: ['MiniMax-M2.7'], source: 'catalog' },
    })

    const wrapper = mountPicker()
    await flushPromises()

    const rows = wrapper.findAll('.summary-row').map((n) => n.text())
    expect(rows).toEqual(['MiniMax-M2.7'])
  })

  it('saves the provider/model pair when a row is picked', async () => {
    routeGet({
      '/api/providers': { providers: [{ id: 3, name: 'MiniMax', protocol: 'anthropic', baseUrl: 'https://gw.example/v1', models: ['m1'] }] },
    })

    const wrapper = mountPicker()
    await flushPromises()

    await wrapper.find('.summary-row').trigger('click')
    await flushPromises()

    expect(mockPatch).toHaveBeenCalledWith('/api/config', {
      ai_summary: { provider_id: 3, model: 'm1' },
    })
    expect(mockLoadConfig).toHaveBeenCalled()
    expect(wrapper.emitted('update:open')?.[0]).toEqual([false])
  })

  it('does not probe a provider that already curates its list', async () => {
    routeGet({
      '/api/providers': { providers: [{ id: 3, name: 'MiniMax', protocol: 'anthropic', baseUrl: 'https://gw.example/v1', models: ['m1'] }] },
    })

    const wrapper = mountPicker()
    await flushPromises()

    expect(mockGet).toHaveBeenCalledTimes(1)
    expect(mockGet).toHaveBeenCalledWith('/api/providers')
    expect(wrapper.find('.summary-manual').exists()).toBe(false)
  })

  it('accepts a typed model id for a provider with no list', async () => {
    routeGet({
      '/api/providers': { providers: [{ id: 1, name: 'AI Summary', protocol: 'anthropic', baseUrl: 'https://gw.example/v1', models: [] }] },
      '/api/providers/1/models': { models: [], error: '404 Not Found' },
    })

    const wrapper = mountPicker()
    await flushPromises()

    expect(wrapper.text()).toContain('404 Not Found')
    await wrapper.find('.summary-manual-input').setValue('MiniMax-M2.7')
    await wrapper.find('.summary-manual-use').trigger('click')
    await flushPromises()

    expect(mockPatch).toHaveBeenCalledWith('/api/config', {
      ai_summary: { provider_id: 1, model: 'MiniMax-M2.7' },
    })
  })

  it('keeps the saved model visible even when the provider no longer lists it', async () => {
    mockServerConfig.value = { ai_summary: { provider_id: 1, model: 'legacy-model' } }
    routeGet({
      '/api/providers': { providers: [{ id: 1, name: 'AI Summary', protocol: 'anthropic', baseUrl: 'https://gw.example/v1', models: [] }] },
      '/api/providers/1/models': { models: [] },
    })

    const wrapper = mountPicker()
    await flushPromises()

    const active = wrapper.find('.summary-row--active')
    expect(active.exists()).toBe(true)
    expect(active.text()).toContain('legacy-model')
  })

  it('stays open and reports the reason when the save is rejected', async () => {
    routeGet({
      '/api/providers': { providers: [{ id: 3, name: 'MiniMax', protocol: 'anthropic', baseUrl: 'https://gw.example/v1', models: ['m1'] }] },
    })
    mockPatch.mockRejectedValue(new Error('ai_summary.model: "m1" is not in the model list'))

    const wrapper = mountPicker()
    await flushPromises()

    await wrapper.find('.summary-row').trigger('click')
    await flushPromises()

    expect(mockToastShow).toHaveBeenCalledWith(
      expect.stringContaining('not in the model list'),
      expect.objectContaining({ type: 'error' }),
    )
    expect(wrapper.emitted('update:open')).toBeUndefined()
  })

  it('says so when no provider can back the summary at all', async () => {
    routeGet({
      '/api/providers': { providers: [{ id: 4, name: 'Gemini only', protocol: 'gemini', baseUrl: 'https://gem.example', models: ['g1'] }] },
    })

    const wrapper = mountPicker()
    await flushPromises()

    expect(wrapper.text()).toContain('都不是 openai / anthropic 协议')
    expect(wrapper.find('.summary-row').exists()).toBe(false)
  })

  // A provider with no base_url only injects a key into an agent's environment
  // (docs/spec/features/model-providers.md). It has no endpoint to summarize
  // against, and the server rejects binding it — so it stays visible with an
  // explanation but must not be selectable.
  it('shows a provider without an endpoint but does not let it be picked', async () => {
    routeGet({
      '/api/providers': {
        providers: [
          { id: 5, name: 'Key only', protocol: 'openai', baseUrl: '', models: ['gpt-4o-mini'] },
          { id: 6, name: 'With endpoint', protocol: 'openai', baseUrl: 'https://api.example/v1', models: ['gpt-4o-mini'] },
        ],
      },
    })

    const wrapper = mountPicker()
    await flushPromises()

    expect(wrapper.text()).toContain('该提供商没有 API 端点')
    const rows = wrapper.findAll('.summary-row')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('gpt-4o-mini')
    expect(wrapper.findAll('.summary-manual')).toHaveLength(0)

    await rows[0].trigger('click')
    await flushPromises()
    expect(mockPatch).toHaveBeenCalledWith('/api/config', {
      ai_summary: { provider_id: 6, model: 'gpt-4o-mini' },
    })
  })
})
