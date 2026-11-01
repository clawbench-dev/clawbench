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

import ProviderFormDialog from '@/components/settings/ProviderFormDialog.vue'
import ProviderPresetPicker from '@/components/settings/ProviderPresetPicker.vue'
import ProviderModelsPicker from '@/components/settings/ProviderModelsPicker.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        items: {
          providerAdd: '添加提供商',
          providerAddTitle: '添加模型提供商',
          providerEditTitle: '编辑模型提供商',
          providerSave: '保存',
          providerCancel: '取消',
          providerName: '名称',
          providerBaseUrl: '端点地址',
          providerKey: 'API Key',
          providerKeyKeep: '留空表示不修改',
          providerKeyReveal: '查看密钥',
          providerKeyHide: '隐藏密钥',
          providerKeyRevealFailed: '服务端没有返回已保存的密钥',
          providerAuthAuto: '自动',
          providerPresetPlaceholder: '从预设选择',
          providerClearKey: '清除密钥',
          providerModelsLabel: '可用模型',
          providerModelsChoose: '选择模型',
          providerModelsNone: '还没有选择模型',
          providerDefaultModel: '默认模型',
          providerWireLabel: '接口协议',
          providerWireResponses: 'Responses API',
          providerWireChat: 'Chat Completions',
          providerTestFailed: '连接失败',
          providerModelTestHint: '测试该模型',
          providerModelTestOk: '{model} · {ms} 毫秒',
          providerModelTestFail: '{model}：{reason}',
        },
      },
    },
  },
})

const presets = [
  { id: 'deepseek', name: 'DeepSeek', protocol: 'openai', baseUrl: 'https://api.deepseek.com/v1' },
]

const existing = {
  id: 7,
  name: 'Existing',
  protocol: 'openai',
  baseUrl: 'https://x/v1',
  authMode: 'bearer',
  presetId: 'deepseek',
  models: ['m-a', 'm-b'],
  defaultModel: 'm-a',
  hasKey: true,
  agentCount: 0,
}

function mountDialog(props: Record<string, unknown> = {}) {
  return mount(ProviderFormDialog, {
    props: { open: true, provider: null, presets, ...props },
    global: {
      plugins: [i18n],
      stubs: {
        AlertCircle: true,
        ChevronsUpDown: true,
        Layers: true,
        ModalDialog: {
          props: ['open', 'title'],
          template: '<div v-if="open" class="mock-dialog"><span class="mock-title">{{ title }}</span><slot name="header" /><slot /><slot name="footer" /></div>',
        },
        ProviderPresetPicker: true,
        ProviderModelsPicker: true,
      },
    },
  })
}

/** Only the real <input>s: the preset trigger is a <button> and the protocol /
 *  auth pickers are <select>s, so their positions shift between create (trigger
 *  present) and edit (trigger hidden). Order is name, baseUrl, apiKey. */
function inputs(wrapper: ReturnType<typeof mountDialog>) {
  return wrapper.findAll('input.provider-input')
}

describe('ProviderFormDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('opens in create mode with blank fields and no edit-only sections', () => {
    const wrapper = mountDialog()
    expect(wrapper.find('.mock-title').text()).toBe('添加模型提供商')
    expect((inputs(wrapper)[0].element as HTMLInputElement).value).toBe('')
    expect((inputs(wrapper)[2].element as HTMLInputElement).value).toBe('')
    expect(wrapper.find('.provider-models').exists()).toBe(false)
    expect(wrapper.find('.provider-clear').exists()).toBe(false)
  })

  it('defaults the wire API to responses and follows the provider on edit', () => {
    // codex's managed block is rendered from this value; a chat-only gateway
    // that keeps "responses" fails every turn.
    const created = mountDialog()
    expect((created.find('[data-role="wire-api"]').element as HTMLSelectElement).value).toBe('responses')

    const edited = mountDialog({ provider: { ...existing, wireApi: 'chat' } })
    expect((edited.find('[data-role="wire-api"]').element as HTMLSelectElement).value).toBe('chat')
  })

  it('opens in edit mode prefilled and keeps the stored key hidden', () => {
    const wrapper = mountDialog({ provider: existing })
    expect(wrapper.find('.mock-title').text()).toBe('编辑模型提供商')
    expect((inputs(wrapper)[0].element as HTMLInputElement).value).toBe('Existing')
    expect((inputs(wrapper)[2].element as HTMLInputElement).value).toBe('')
    expect(inputs(wrapper)[2].attributes('type')).toBe('password')
    expect(mockGet).not.toHaveBeenCalled()
    expect(wrapper.find('.provider-models').exists()).toBe(true)
    expect(wrapper.text()).toContain('m-a')
  })

  it('fetches the stored key only when the eye is clicked', async () => {
    mockGet.mockResolvedValue({ apiKey: 'sk-stored' })
    const wrapper = mountDialog({ provider: existing })

    await wrapper.find('.provider-key-toggle').trigger('click')
    await flushPromises()

    expect(mockGet).toHaveBeenCalledWith('/api/providers/7?reveal=1')
    const key = () => inputs(wrapper)[2]
    expect(key().attributes('type')).toBe('text')
    expect((key().element as HTMLInputElement).value).toBe('sk-stored')

    // Second click hides it again without another round trip.
    await wrapper.find('.provider-key-toggle').trigger('click')
    expect(key().attributes('type')).toBe('password')
    expect(mockGet).toHaveBeenCalledTimes(1)
  })

  it('never replaces a key the user already typed with the stored one', async () => {
    mockGet.mockResolvedValue({ apiKey: 'sk-stored' })
    const wrapper = mountDialog({ provider: existing })
    await inputs(wrapper)[2].setValue('sk-typed')

    await wrapper.find('.provider-key-toggle').trigger('click')
    await flushPromises()

    expect(mockGet).not.toHaveBeenCalled()
    expect((inputs(wrapper)[2].element as HTMLInputElement).value).toBe('sk-typed')
    expect(inputs(wrapper)[2].attributes('type')).toBe('text')
  })

  it('surfaces a failed reveal instead of showing an empty field', async () => {
    mockGet.mockRejectedValue(new Error('forbidden'))
    const wrapper = mountDialog({ provider: existing })

    await wrapper.find('.provider-key-toggle').trigger('click')
    await flushPromises()

    expect(wrapper.find('.provider-error').text()).toContain('forbidden')
    expect(inputs(wrapper)[2].attributes('type')).toBe('password')
  })

  it('treats an empty reveal answer as a failure, not an empty key', async () => {
    // An older server ignores `reveal` and answers without apiKey; rendering
    // that as "" would look like the stored key had been wiped.
    mockGet.mockResolvedValue({ hasKey: true })
    const wrapper = mountDialog({ provider: existing })

    await wrapper.find('.provider-key-toggle').trigger('click')
    await flushPromises()

    expect(wrapper.find('.provider-error').text()).toContain('服务端没有返回已保存的密钥')
    expect(inputs(wrapper)[2].attributes('type')).toBe('password')
  })

  it('emits the trimmed values, and blocks submit without a name', async () => {
    const wrapper = mountDialog()
    const save = wrapper.find('.mock-dialog .fbtn-primary')

    await save.trigger('click')
    expect(wrapper.emitted('submit')).toBeUndefined()
    expect(save.attributes('disabled')).toBeDefined()

    await inputs(wrapper)[0].setValue('  My Provider  ')
    await inputs(wrapper)[1].setValue(' https://api.example.com/v1 ')
    await inputs(wrapper)[2].setValue('sk-secret')
    await save.trigger('click')

    expect(wrapper.emitted('submit')?.[0]).toEqual([expect.objectContaining({
      name: 'My Provider',
      protocol: 'anthropic',
      baseUrl: 'https://api.example.com/v1',
      apiKey: 'sk-secret',
      authMode: 'auto',
      clearApiKey: false,
    })])
  })

  it('fills the form from a chosen preset and keeps its catalogue id', async () => {
    const wrapper = mountDialog()
    await wrapper.findComponent(ProviderPresetPicker).vm.$emit('select', presets[0])
    await flushPromises()

    expect((inputs(wrapper)[0].element as HTMLInputElement).value).toBe('DeepSeek')
    expect((inputs(wrapper)[1].element as HTMLInputElement).value).toBe('https://api.deepseek.com/v1')

    await wrapper.find('.mock-dialog .fbtn-primary').trigger('click')
    expect(wrapper.emitted('submit')?.[0]).toEqual([expect.objectContaining({ presetId: 'deepseek' })])
  })

  it('sends clearApiKey only when the box is ticked', async () => {
    const wrapper = mountDialog({ provider: existing })
    const box = wrapper.find('.provider-clear input')
    expect(box.exists()).toBe(true)

    await wrapper.find('.mock-dialog .fbtn-primary').trigger('click')
    expect(wrapper.emitted('submit')?.[0]).toEqual([expect.objectContaining({ clearApiKey: false })])

    await box.setValue(true)
    await wrapper.find('.mock-dialog .fbtn-primary').trigger('click')
    expect(wrapper.emitted('submit')?.[1]).toEqual([expect.objectContaining({ clearApiKey: true })])
  })

  it('takes the picked model set and keeps the default model in range', async () => {
    const wrapper = mountDialog({ provider: existing })
    await wrapper.findComponent(ProviderModelsPicker).vm.$emit('update:selected', ['m-b', 'm-c'])
    await flushPromises()

    // 'm-a' is gone from the set, so the default falls back to the first pick.
    await wrapper.find('.mock-dialog .fbtn-primary').trigger('click')
    expect(wrapper.emitted('submit')?.[0]).toEqual([expect.objectContaining({
      models: ['m-b', 'm-c'],
      defaultModel: 'm-b',
    })])
  })

  it('re-seeds from the provider on every open (a cancelled edit cannot leak)', async () => {
    const wrapper = mountDialog({ provider: existing })
    await inputs(wrapper)[0].setValue('Typed but cancelled')
    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    await flushPromises()

    expect((inputs(wrapper)[0].element as HTMLInputElement).value).toBe('Existing')
    expect((inputs(wrapper)[2].element as HTMLInputElement).value).toBe('')
  })

  it('shows the server error passed down by the caller', () => {
    const wrapper = mountDialog({ errorMessage: 'name already exists' })
    expect(wrapper.find('.provider-error').text()).toContain('name already exists')
  })

  it('disables the primary button while a save is in flight', () => {
    const wrapper = mountDialog({ saving: true, provider: existing })
    expect(wrapper.find('.mock-dialog .fbtn-primary').attributes('disabled')).toBeDefined()
  })

  it('emits close from the cancel button', async () => {
    const wrapper = mountDialog()
    const cancel = wrapper.findAll('.mock-dialog .fbtn').find(b => b.text().includes('取消'))
    await cancel!.trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('tests one model with the draft values and reports how fast it answered', async () => {
    mockPost.mockResolvedValue({ ok: true, status: 200, latencyMs: 320, reply: 'ok' })
    const wrapper = mountDialog({ provider: existing })

    await wrapper.find('[data-model-test="m-a"]').trigger('click')
    await flushPromises()

    // The unsaved draft rides along: the endpoint/key being tested is the one
    // on screen, not necessarily the one already stored.
    expect(mockPost).toHaveBeenCalledWith(
      '/api/providers/test',
      expect.objectContaining({
        id: 7,
        model: 'm-a',
        protocol: 'openai',
        baseUrl: 'https://x/v1',
        authMode: 'bearer',
      }),
      expect.objectContaining({ timeoutMs: 45_000 }),
    )
    const result = wrapper.find('.provider-model-result--ok')
    expect(result.text()).toContain('m-a')
    expect(result.text()).toContain('320')
    expect(wrapper.findAll('.provider-model-chip')[0].classes()).toContain('provider-model-chip--ok')
  })

  it('shows the upstream reason when a model test fails', async () => {
    mockPost.mockResolvedValue({ ok: false, status: 404, error: 'HTTP 404: model not found' })
    const wrapper = mountDialog({ provider: existing })

    await wrapper.find('[data-model-test="m-b"]').trigger('click')
    await flushPromises()

    const result = wrapper.find('.provider-model-result--fail')
    expect(result.text()).toContain('m-b')
    expect(result.text()).toContain('model not found')
    expect(wrapper.findAll('.provider-model-chip')[1].classes()).toContain('provider-model-chip--fail')
  })

  it('drops a model verdict when that model is removed', async () => {
    mockPost.mockResolvedValue({ ok: true, status: 200, latencyMs: 12 })
    const wrapper = mountDialog({ provider: existing })

    await wrapper.find('[data-model-test="m-a"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('.provider-model-result').exists()).toBe(true)

    await wrapper.findAll('.provider-model-chip')[0].find('.provider-model-chip-x').trigger('click')
    expect(wrapper.find('.provider-model-result').exists()).toBe(false)
  })

  it('clears model verdicts on reopen (a stale result cannot describe a new draft)', async () => {
    mockPost.mockResolvedValue({ ok: true, status: 200, latencyMs: 12 })
    const wrapper = mountDialog({ provider: existing })

    await wrapper.find('[data-model-test="m-a"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('.provider-model-result').exists()).toBe(true)

    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    await flushPromises()

    expect(wrapper.find('.provider-model-result').exists()).toBe(false)
  })
})
