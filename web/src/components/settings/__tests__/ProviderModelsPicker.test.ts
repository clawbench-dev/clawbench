import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

const mockGet = vi.fn()
const mockPost = vi.fn()

vi.mock('@/utils/api', () => ({
  apiGet: (...args: unknown[]) => mockGet(...args),
  apiPost: (...args: unknown[]) => mockPost(...args),
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
          providerModelsAll: '全选',
          providerModelsClear: '清空',
          providerModelsStar: '设为默认模型',
          providerModelsUnstar: '取消默认',
          providerModelTestHint: '测试该模型',
          providerModelTestOk: '{model} · {ms} 毫秒',
          providerModelTestFail: '{model}：{reason}',
          providerTestFailed: '连接失败',
        },
      },
    },
  },
})

const probe = { protocol: 'openai', baseUrl: 'https://x/v1', apiKey: 'sk-draft', authMode: 'bearer' }

function mountPicker(props: Record<string, unknown> = {}) {
  return mount(ProviderModelsPicker, {
    props: {
      active: true,
      providerId: 1,
      selected: [],
      defaultModel: '',
      probe,
      ...props,
    },
    global: {
      plugins: [i18n],
    },
  })
}

describe('ProviderModelsPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGet.mockResolvedValue({ models: ['m-a', 'm-b'], source: 'live' })
    mockPost.mockResolvedValue({ ok: true, status: 200, latencyMs: 320 })
  })

  it('loads the endpoint list and reports its source', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    expect(mockGet).toHaveBeenCalledWith('/api/providers/1/models')
    expect(wrapper.text()).toContain('来自端点实测')
    expect(wrapper.findAll('.models-row')).toHaveLength(2)
  })

  it('toggles a model on and off', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    await wrapper.find('.models-check').trigger('click')
    expect(wrapper.emitted('update:selected')?.[0]).toEqual([['m-a']])

    await wrapper.setProps({ selected: ['m-a'] })
    await wrapper.findAll('.models-check')[0].trigger('click')
    expect(wrapper.emitted('update:selected')?.[1]).toEqual([[]])
  })

  it('keeps a saved id that the endpoint no longer lists', async () => {
    const wrapper = mountPicker({ selected: ['legacy-model'] })
    await flushPromises()
    const ids = wrapper.findAll('.models-id').map(n => n.text())
    expect(ids).toContain('legacy-model')
  })

  it('renders a price tag for ids the catalog knows (issue #643)', async () => {
    mockGet.mockResolvedValue({
      models: ['glm-5.2', 'unknown-model'],
      prices: { 'glm-5.2': { input: 1.4, output: 4.4 } },
      source: 'live',
    })
    const wrapper = mountPicker()
    await flushPromises()

    const tags = wrapper.findAll('.models-price')
    expect(tags).toHaveLength(1)
    expect(tags[0].text()).toBe('$1.4 / $4.4')
  })

  it('marks a reseller consensus price with a tilde', async () => {
    mockGet.mockResolvedValue({
      models: ['qmodel'],
      prices: { qmodel: { input: 0.4, output: 1.6, approx: true } },
      source: 'live',
    })
    const wrapper = mountPicker()
    await flushPromises()

    expect(wrapper.find('.models-price').text()).toBe('~$0.4 / $1.6')
  })

  it('shows no tag when the response carries no prices', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    expect(wrapper.findAll('.models-price')).toHaveLength(0)
  })

  it('adds a typed model id', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    await wrapper.find('.models-custom-input').setValue('custom-model')
    await wrapper.find('.models-custom .fbtn').trigger('click')
    expect(wrapper.emitted('update:selected')?.[0]).toEqual([['custom-model']])
  })

  it('filters the list', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    await wrapper.find('.models-search .search-pill input').setValue('m-b')
    expect(wrapper.findAll('.models-row')).toHaveLength(1)
    expect(wrapper.find('.models-id').text()).toBe('m-b')
  })

  it('selects everything visible and clears the whole set', async () => {
    const wrapper = mountPicker({ selected: ['m-a'] })
    await flushPromises()

    const bulk = wrapper.findAll('.models-bulk')
    await bulk[0].trigger('click')
    expect(wrapper.emitted('update:selected')?.[0]).toEqual([['m-a', 'm-b']])

    await bulk[1].trigger('click')
    expect(wrapper.emitted('update:selected')?.[1]).toEqual([[]])
  })

  it('sets the default model from the row, and unsets it on a second tap', async () => {
    const wrapper = mountPicker({ selected: ['m-a', 'm-b'], defaultModel: 'm-a' })
    await flushPromises()

    const stars = wrapper.findAll('.models-star')
    await stars[1].trigger('click')
    expect(wrapper.emitted('update:defaultModel')?.[0]).toEqual(['m-b'])

    await wrapper.setProps({ defaultModel: 'm-b' })
    await stars[1].trigger('click')
    expect(wrapper.emitted('update:defaultModel')?.[1]).toEqual([''])
  })

  it('only lets a picked model be the default', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    expect(wrapper.findAll('.models-star').every((b) => b.attributes('disabled') !== undefined)).toBe(true)
  })

  it('probes one model with the values on screen and reports the latency', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    await wrapper.find('[data-model-test="m-a"]').trigger('click')
    await flushPromises()

    // The unsaved draft rides along: the endpoint/key being tested is the one
    // on screen, not necessarily the one already stored.
    expect(mockPost).toHaveBeenCalledWith(
      '/api/providers/test',
      expect.objectContaining({
        id: 1,
        model: 'm-a',
        protocol: 'openai',
        baseUrl: 'https://x/v1',
        apiKey: 'sk-draft',
        authMode: 'bearer',
      }),
      expect.objectContaining({ timeoutMs: 45_000 }),
    )
    expect(wrapper.find('.models-result--ok').text()).toContain('320')
  })

  it('shows the upstream reason when a probe fails', async () => {
    mockPost.mockResolvedValue({ ok: false, status: 404, error: 'HTTP 404: model not found' })
    const wrapper = mountPicker()
    await flushPromises()

    await wrapper.find('[data-model-test="m-b"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('.models-result--fail').text()).toContain('model not found')
  })

  it('drops a verdict when its model is deselected', async () => {
    const wrapper = mountPicker({ selected: ['m-a'] })
    await flushPromises()

    await wrapper.find('[data-model-test="m-a"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('.models-result').exists()).toBe(true)

    await wrapper.setProps({ selected: [] })
    await flushPromises()
    expect(wrapper.find('.models-result').exists()).toBe(false)
  })

  it('clears verdicts on re-entry (a stale result cannot describe a new draft)', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    await wrapper.find('[data-model-test="m-a"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('.models-result').exists()).toBe(true)

    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true })
    await flushPromises()

    expect(wrapper.find('.models-result').exists()).toBe(false)
  })

  it('discards a probe that answers after the view was re-entered', async () => {
    const first = deferred<{ ok: boolean; latencyMs: number }>()
    mockPost.mockReturnValueOnce(first.promise)

    const wrapper = mountPicker()
    await flushPromises()
    await wrapper.find('[data-model-test="m-a"]').trigger('click')

    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true })
    await flushPromises()

    first.resolve({ ok: true, latencyMs: 999 })
    await flushPromises()

    expect(wrapper.find('.models-result').exists()).toBe(false)
    // And the panel is not left believing a probe is still running.
    expect(wrapper.find('[data-model-test="m-a"]').attributes('disabled')).toBeUndefined()
  })

  it('hides the probe when the form has nothing to probe with (create mode)', async () => {
    // Rows still render from the endpoint list; only the probe is unavailable,
    // so the assertion must have rows to be about.
    const wrapper = mountPicker({ probe: null })
    await flushPromises()
    expect(wrapper.findAll('.models-row')).toHaveLength(2)
    expect(wrapper.findAll('[data-model-test]').every((b) => b.attributes('disabled') !== undefined)).toBe(true)
  })
})

/** A promise plus its resolver, for holding one probe open across a re-entry. */
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}
