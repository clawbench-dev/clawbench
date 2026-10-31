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
          providerModelCount: '{count} 个模型',
          providerNoModels: '未选择模型',
          providerDefaultModelValue: '默认 {model}',
          providerAgentsValue: '智能体 {agents}',
          providerAuthAutoShort: '自动认证',
          providerAuthAuto: '自动',
          providerPresetPlaceholder: '从预设选择',
          providerPresetTitle: '选择供应商',
          providerPresetSearch: '搜索',
          providerPresetNone: '不使用预设',
          providerPresetEmpty: '没有匹配的供应商',
          providerAdd: '添加提供商',
          providerAddTitle: '添加模型提供商',
          providerEditTitle: '编辑模型提供商',
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
      stubs: {
        AlertCircle: true,
        CheckCircle2: true,
        Loader2: true,
        PlugZap: true,
        Pencil: true,
        Plus: true,
        Trash2: true,
        // The real dialog teleports to <body>; render it inline so its fields
        // and buttons stay inside the wrapper (the pickers use it too).
        ModalDialog: {
          props: ['open', 'title'],
          template: '<div v-if="open" class="mock-dialog"><span class="mock-title">{{ title }}</span><slot name="header" /><slot /><slot name="footer" /></div>',
        },
      },
    },
  })
}

/** The dialog's own primary button — the toolbar's "add" button is also
 *  `.fbtn-primary`, so every form assertion has to be scoped to the dialog. */
function dialogSave(wrapper: ReturnType<typeof mountSetting>) {
  return wrapper.find('.mock-dialog .fbtn-primary')
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

  it('shows the whole configured provider on its card', async () => {
    const wrapper = mountSetting([
      {
        id: 1, name: 'MiniMax', protocol: 'anthropic', baseUrl: 'https://api.minimax.io/anthropic',
        authMode: 'bearer', presetId: 'minimax', models: ['MiniMax-M2', 'MiniMax-M3'],
        defaultModel: 'MiniMax-M2', hasKey: true, agentCount: 1, agents: ['Writer'],
      },
    ])
    await flushPromises()
    const row = wrapper.find('.provider-row[data-provider-id="1"]')

    // A card is only "complete" if it answers: where does it point, how does it
    // authenticate, what can it serve, what is the default, and who uses it.
    expect(row.text()).toContain('MiniMax')
    expect(row.text()).toContain('anthropic')
    expect(row.text()).toContain('bearer')
    expect(row.text()).toContain('https://api.minimax.io/anthropic')
    expect(row.text()).toContain('2 个模型')
    expect(row.text()).toContain('默认 MiniMax-M2')
    expect(row.text()).toContain('智能体 Writer')
    expect(row.text()).toContain('已配置密钥')
    expect(row.text()).toContain('1 个智能体在用')
    // The brand tile is what makes a long list scannable.
    expect(row.find('.provider-icon-tile').exists()).toBe(true)
  })

  it('says so when a provider has no models picked yet', async () => {
    const wrapper = mountSetting([
      { id: 2, name: 'Bare', protocol: 'openai', baseUrl: '', authMode: 'auto', hasKey: false, agentCount: 0, models: [] },
    ])
    await flushPromises()
    const row = wrapper.find('.provider-row[data-provider-id="2"]')
    expect(row.text()).toContain('未设置端点')
    expect(row.text()).toContain('未选择模型')
    expect(row.text()).toContain('自动认证')
    expect(row.text()).toContain('未配置密钥')
  })

  it('keeps the configuration form out of the list until it is opened', async () => {
    const wrapper = mountSetting([])
    await flushPromises()
    // The list page must not carry the form inline (that is what made the page
    // read as "list glued to a form").
    expect(wrapper.find('.mock-dialog').exists()).toBe(false)
    expect(wrapper.find('.provider-preset-trigger').exists()).toBe(false)

    await wrapper.find('.provider-toolbar .fbtn-primary').trigger('click')
    await flushPromises()
    expect(wrapper.find('.mock-dialog').exists()).toBe(true)
    expect(wrapper.find('.mock-dialog').text()).toContain('添加模型提供商')
  })

  it('creates a provider with the values typed in the dialog', async () => {
    const wrapper = mountSetting([])
    await flushPromises()

    await wrapper.find('.provider-toolbar .fbtn-primary').trigger('click')
    await flushPromises()

    const dialog = wrapper.find('.mock-dialog')
    const inputs = dialog.findAll('input.provider-input')
    // [0] name, [1] baseUrl, [2] apiKey (selects are excluded by `input.`)
    await inputs[0].setValue('My Provider')
    await inputs[1].setValue('https://api.example.com/v1')
    await inputs[2].setValue('sk-secret')

    await dialogSave(wrapper).trigger('click')
    await flushPromises()

    expect(mockPost).toHaveBeenCalledWith('/api/providers', expect.objectContaining({
      name: 'My Provider',
      protocol: 'anthropic',
      baseUrl: 'https://api.example.com/v1',
      apiKey: 'sk-secret',
    }))
    // A successful save closes the dialog and reloads the list.
    expect(wrapper.find('.mock-dialog').exists()).toBe(false)
    expect(mockGet).toHaveBeenCalledWith('/api/providers')
  })

  it('applies a preset into the dialog and remembers its catalogue id', async () => {
    const wrapper = mountSetting([])
    await flushPromises()

    await wrapper.find('.provider-toolbar .fbtn-primary').trigger('click')
    await flushPromises()

    await wrapper.find('.provider-preset-trigger').trigger('click')
    await flushPromises()
    const row = wrapper.findAll('.preset-row').find(r => r.attributes('data-preset-id') === 'p1')
    expect(row).toBeDefined()
    await row!.trigger('click')
    await flushPromises()

    const inputs = wrapper.find('.mock-dialog').findAll('input.provider-input')
    expect((inputs[0].element as HTMLInputElement).value).toBe('Preset One')
    expect((inputs[1].element as HTMLInputElement).value).toBe('https://p1/v1')

    await inputs[2].setValue('sk-1')
    await dialogSave(wrapper).trigger('click')
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

  it('editing opens the dialog prefilled and sends an empty apiKey (keep) unless clear is ticked', async () => {
    const wrapper = mountSetting([
      { id: 7, name: 'Existing', protocol: 'openai', baseUrl: 'https://x/v1', authMode: 'bearer', hasKey: true, agentCount: 0 },
    ])
    await flushPromises()

    await wrapper.findAll('.fbtn').find((b) => b.text().includes('编辑'))!.trigger('click')
    await flushPromises()

    const dialog = wrapper.find('.mock-dialog')
    expect(dialog.exists()).toBe(true)
    expect(dialog.text()).toContain('编辑模型提供商')
    const inputs = dialog.findAll('input.provider-input')
    expect((inputs[0].element as HTMLInputElement).value).toBe('Existing')
    expect((inputs[1].element as HTMLInputElement).value).toBe('https://x/v1')

    await dialogSave(wrapper).trigger('click')
    await flushPromises()

    expect(mockPut).toHaveBeenCalledWith('/api/providers/7', expect.objectContaining({
      apiKey: '',
      clearApiKey: false,
    }))
  })

  it('keeps the dialog open with the server error when saving fails', async () => {
    const wrapper = mountSetting([])
    await flushPromises()

    mockPost.mockRejectedValueOnce(new Error('name already exists'))
    await wrapper.find('.provider-toolbar .fbtn-primary').trigger('click')
    await flushPromises()
    await wrapper.find('.mock-dialog').findAll('input.provider-input')[0].setValue('Dup')
    await dialogSave(wrapper).trigger('click')
    await flushPromises()

    expect(wrapper.find('.mock-dialog').exists()).toBe(true)
    expect(wrapper.find('.mock-dialog').text()).toContain('name already exists')
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
