import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

const mockGet = vi.fn()
const mockPost = vi.fn()
const mockPut = vi.fn()
const mockDelete = vi.fn()

vi.mock('@/utils/api', () => ({
  apiGet: (...args: unknown[]) => mockGet(...args),
  apiPost: (...args: unknown[]) => mockPost(...args),
  apiPut: (...args: unknown[]) => mockPut(...args),
  apiDelete: (...args: unknown[]) => mockDelete(...args),
}))

const mockConfirm = vi.fn().mockResolvedValue(true)
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: mockConfirm, alert: vi.fn(), prompt: vi.fn(), state: { value: {} } }),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import ProviderSetting from '@/components/settings/ProviderSetting.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        items: {
          providerDesc: '模型提供商',
          providerEmpty: '还没有配置模型提供商',
          providerName: '名称',
          providerBaseUrl: '端点地址',
          providerKey: 'API Key',
          providerKeyKeep: '留空表示不修改',
          providerKeySet: '已配置密钥',
          providerNoKey: '未配置密钥',
          providerNoBaseUrl: '未设置端点',
          providerUsedBy: '{count} 个智能体在用',
          providerAuthAuto: '自动',
          providerPresetPlaceholder: '从预设选择',
          providerAdd: '添加提供商',
          providerSave: '保存',
          providerCancel: '取消',
          providerEdit: '编辑',
          providerRemove: '删除',
          providerClearKey: '清除密钥',
          providerDeleteConfirm: '删除 {name}？',
          providerTest: '测试',
          providerTesting: '测试中',
          providerTestHint: '诊断',
          providerTestOk: '连接正常，返回 {count} 个模型',
          providerTestFailed: '连接失败',
          providerCatalogUpdated: '权威模型目录：{date} · {count} 个供应商',
          providerCatalogMissing: '尚未获取权威模型目录',
          providerCatalogStale: '（可能已过期）',
          providerCatalogRefresh: '刷新目录',
          providerCatalogRefreshing: '刷新中',
        },
      },
    },
  },
})

function mountSetting(providers: unknown[], catalog: unknown = {}) {
  mockGet.mockImplementation((url: string) => {
    if (url === '/api/providers') return Promise.resolve({ providers })
    if (url === '/api/providers/presets') return Promise.resolve({ presets: [{ id: 'p1', name: 'Preset One', protocol: 'openai', baseUrl: 'https://p1/v1' }] })
    if (url === '/api/catalog') return Promise.resolve(catalog)
    return Promise.resolve({})
  })
  return mount(ProviderSetting, {
    global: {
      plugins: [i18n],
      stubs: { AlertCircle: true, CheckCircle2: true, Loader2: true, PlugZap: true, Pencil: true, Trash2: true },
    },
  })
}

describe('ProviderSetting', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockConfirm.mockResolvedValue(true)
    mockPost.mockResolvedValue({})
    mockPut.mockResolvedValue({})
    mockDelete.mockResolvedValue(undefined)
  })

  it('renders the empty hint and the key state of each row', async () => {
    const wrapper = mountSetting([])
    await flushPromises()
    expect(wrapper.text()).toContain('还没有配置模型提供商')
  })

  it('never renders a stored key, only the "key set" badge', async () => {
    const wrapper = mountSetting([
      { id: 1, name: 'MiniMax', protocol: 'anthropic', baseUrl: 'https://api.minimax.io/anthropic', authMode: 'auto', hasKey: true, agentCount: 2 },
    ])
    await flushPromises()
    expect(wrapper.text()).toContain('MiniMax')
    expect(wrapper.text()).toContain('已配置密钥')
    expect(wrapper.text()).toContain('2 个智能体在用')
    expect(wrapper.html()).not.toContain('sk-')
  })

  it('creates a provider with the typed values', async () => {
    const wrapper = mountSetting([])
    await flushPromises()

    const inputs = wrapper.findAll('.provider-input')
    // [0] preset select, [1] name, [2] protocol, [3] baseUrl, [4] apiKey, [5] authMode
    await inputs[1].setValue('My Provider')
    await inputs[3].setValue('https://api.example.com/v1')
    await inputs[4].setValue('sk-secret')

    await wrapper.find('.fbtn-primary').trigger('click')
    await flushPromises()

    expect(mockPost).toHaveBeenCalledWith('/api/providers', expect.objectContaining({
      name: 'My Provider',
      protocol: 'anthropic',
      baseUrl: 'https://api.example.com/v1',
      apiKey: 'sk-secret',
    }))
  })

  it('applies a preset into the form and remembers its catalogue id', async () => {
    const wrapper = mountSetting([])
    await flushPromises()

    const presetSelect = wrapper.find('.provider-preset-row select')
    await presetSelect.setValue('p1')
    await presetSelect.trigger('change')

    const inputs = wrapper.findAll('.provider-input')
    expect((inputs[1].element as HTMLInputElement).value).toBe('Preset One')
    expect((inputs[3].element as HTMLInputElement).value).toBe('https://p1/v1')

    await inputs[4].setValue('sk-1')
    await wrapper.find('.fbtn-primary').trigger('click')
    await flushPromises()
    expect(mockPost).toHaveBeenCalledWith('/api/providers', expect.objectContaining({
      name: 'Preset One',
      presetId: 'p1',
    }))
  })

  it('shows the catalogue age and count, and refreshes on demand', async () => {
    const wrapper = mountSetting([], {
      fetchedAt: '2026-10-31T00:00:00Z',
      providers: [{ id: 'anthropic' }, { id: 'openai' }],
      stale: false,
    })
    await flushPromises()

    expect(wrapper.text()).toContain('2')
    const refresh = wrapper.findAll('.fbtn').find(b => b.text().includes('刷新目录'))
    expect(refresh).toBeDefined()

    mockPost.mockResolvedValueOnce({})
    await refresh!.trigger('click')
    await flushPromises()
    expect(mockPost).toHaveBeenCalledWith('/api/catalog', {})
  })

  it('editing sends an empty apiKey (keep) unless clear is ticked', async () => {
    const wrapper = mountSetting([
      { id: 7, name: 'Existing', protocol: 'openai', baseUrl: 'https://x/v1', authMode: 'bearer', hasKey: true, agentCount: 0 },
    ])
    await flushPromises()

    await wrapper.findAll('.fbtn').find((b) => b.text().includes('编辑'))!.trigger('click')
    await flushPromises()
    await wrapper.find('.fbtn-primary').trigger('click')
    await flushPromises()

    expect(mockPut).toHaveBeenCalledWith('/api/providers/7', expect.objectContaining({
      apiKey: '',
      clearApiKey: false,
    }))
  })

  it('deletes only after confirmation', async () => {
    const wrapper = mountSetting([
      { id: 3, name: 'Gone', protocol: 'openai', baseUrl: 'https://x', authMode: 'auto', hasKey: false, agentCount: 0 },
    ])
    await flushPromises()

    mockConfirm.mockResolvedValueOnce(false)
    await wrapper.findAll('.fbtn').find((b) => b.text().includes('删除'))!.trigger('click')
    await flushPromises()
    expect(mockDelete).not.toHaveBeenCalled()

    mockConfirm.mockResolvedValueOnce(true)
    await wrapper.findAll('.fbtn').find((b) => b.text().includes('删除'))!.trigger('click')
    await flushPromises()
    expect(mockDelete).toHaveBeenCalledWith('/api/providers/3')
  })
})
