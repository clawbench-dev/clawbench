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
      common: { back: '返回', close: '关闭' },
      settings: {
        items: {
          providerAdd: '添加提供商',
          providerAddTitle: '添加模型提供商',
          providerEditTitle: '编辑模型提供商',
          providerSave: '保存',
          providerCancel: '取消',
          providerName: '名称',
          providerNamePlaceholder: '例如 MiniMax',
          providerProtocol: '协议',
          providerBaseUrl: '端点地址',
          providerBaseUrlPlaceholder: 'https://api.example.com/v1',
          providerBaseUrlHint: '可留空',
          providerAuthLabel: '认证方式',
          providerAuthAuto: '自动（推荐）',
          providerKey: 'API Key',
          providerKeyKeep: '留空表示不修改',
          providerKeyReveal: '查看密钥',
          providerKeyHide: '隐藏密钥',
          providerKeyRevealFailed: '服务端没有返回已保存的密钥',
          providerKeyStateSaved: '已保存',
          providerKeyStateMissing: '未配置',
          providerClearKey: '清除已保存的密钥',
          providerClearKeyPending: '保存后将删除已保存的密钥',
          providerUndo: '撤销',
          providerSectionConn: '连接',
          providerSectionCreds: '凭据',
          providerSectionModels: '模型',
          providerSectionCredsLabel: '凭据',
          providerPresetPlaceholder: '从预设选择',
          providerPresetTitle: '选择供应商',
          providerModelsLabel: '模型',
          providerModelsManage: '管理',
          providerModelsNone: '还没有选择模型',
          providerModelsSummary: '已选 {count} 个',
          providerModelsTitle: '选择可用模型',
          providerModelsDone: '完成',
          providerDefaultModelValue: '默认 {model}',
          providerWireSection: 'Codex 调用方式',
          providerWireResponses: 'Responses API',
          providerWireChat: 'Chat Completions',
          providerWireHint: '只影响 Codex，已连接的会话需重启',
          providerAuthUnsupported: '「{mode}」不适用于 {protocol}',
          providerTestConn: '测试连接',
          providerTestingConn: '测试中…',
          providerTestOk: '连接正常，返回 {count} 个模型',
          providerTestFailed: '连接失败',
          providerCreatedHint: '已创建，正在配置模型',
          providerRemove: '删除',
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
        ModalDialog: {
          props: ['open', 'title'],
          template: '<div v-if="open" class="mock-dialog"><span class="mock-title">{{ title }}</span><slot name="header" /><slot /><slot name="footer" /></div>',
        },
      },
    },
  })
}

/** Only the real <input>s: the preset trigger is a <button>, the protocol is a
 *  segmented control and the auth picker is a <select>, so their positions
 *  shift between create (trigger present) and edit (trigger hidden).
 *  Order is name, baseUrl, apiKey. */
function inputs(wrapper: ReturnType<typeof mountDialog>) {
  return wrapper.findAll('input.provider-input')
}

const save = (wrapper: ReturnType<typeof mountDialog>) => wrapper.find('[data-action="provider-save"]')
const protocolBtn = (wrapper: ReturnType<typeof mountDialog>, p: string) => wrapper.find(`[data-protocol="${p}"]`)

describe('ProviderFormDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('opens in create mode with blank fields and no edit-only sections', () => {
    const wrapper = mountDialog()
    expect(wrapper.find('.mock-title').text()).toBe('添加模型提供商')
    expect((inputs(wrapper)[0].element as HTMLInputElement).value).toBe('')
    expect((inputs(wrapper)[2].element as HTMLInputElement).value).toBe('')
    // The model section and the clear-key action only exist once the provider
    // exists — they act on something a create does not have yet.
    expect(wrapper.find('[data-section="models"]').exists()).toBe(false)
    expect(wrapper.find('.provider-clear').exists()).toBe(false)
  })

  it('labels every field, so a filled form still says what each box is', () => {
    const wrapper = mountDialog({ provider: existing })
    const text = wrapper.find('.provider-form').text()
    for (const label of ['名称', '协议', '端点地址', '认证方式', 'API Key']) {
      expect(text).toContain(label)
    }
  })

  it('offers the Codex wire API only for openai providers', async () => {
    // It is a codex-only sub-option of the openai protocol, not a peer of the
    // protocol control — showing it for anthropic/gemini providers made it read
    // as a fourth protocol.
    const created = mountDialog()
    expect(created.find('[data-section="wire"]').exists()).toBe(false)

    await protocolBtn(created, 'openai').trigger('click')
    const wire = created.find('[data-role="wire-api"]')
    expect(wire.exists()).toBe(true)
    expect((wire.element as HTMLSelectElement).value).toBe('responses')

    const anthropic = mountDialog({ provider: { ...existing, protocol: 'anthropic' } })
    expect(anthropic.find('[data-section="wire"]').exists()).toBe(false)

    // codex's managed block is rendered from this value; a chat-only gateway
    // that keeps "responses" fails every turn.
    const edited = mountDialog({ provider: { ...existing, wireApi: 'chat' } })
    expect((edited.find('[data-role="wire-api"]').element as HTMLSelectElement).value).toBe('chat')
  })

  it('says a wire-API change only reaches a Codex session that is restarted', async () => {
    const created = mountDialog()
    expect(created.text()).not.toContain('已连接的会话需重启')

    await protocolBtn(created, 'openai').trigger('click')
    expect(created.text()).toContain('已连接的会话需重启')
  })

  it('only offers the auth modes the chosen protocol can express', async () => {
    const wrapper = mountDialog()
    const authSelect = () => wrapper.find('[data-role="auth-mode"]')
    const optionValues = () =>
      authSelect().findAll('option').map((o) => (o.element as HTMLOptionElement).value)

    // anthropic backends read both ANTHROPIC_API_KEY and ANTHROPIC_AUTH_TOKEN.
    expect(optionValues()).toEqual(['auto', 'x-api-key', 'bearer'])

    // openai/gemini have no auth-token variable: picking bearer there made
    // BuildProviderEnv refuse every turn, so it must not be offered at all.
    await protocolBtn(wrapper, 'openai').trigger('click')
    expect(optionValues()).toEqual(['auto', 'x-api-key'])

    await protocolBtn(wrapper, 'gemini').trigger('click')
    expect(optionValues()).toEqual(['auto', 'x-api-key'])
  })

  it('flags a stored auth mode the protocol cannot express instead of silently rewriting it', async () => {
    // `existing` is an openai provider saved with authMode=bearer — exactly the
    // pair that fails on every turn. It stays selected (so the user sees what is
    // stored) and the form explains what to change.
    const wrapper = mountDialog({ provider: existing })
    const warning = wrapper.find('[data-role="auth-mode-warning"]')
    expect(warning.exists()).toBe(true)
    expect(warning.text()).toContain('bearer')
    expect(warning.text()).toContain('openai')
    expect((wrapper.find('[data-role="auth-mode"]').element as HTMLSelectElement).value).toBe('bearer')

    // The stored value must survive an unrelated save untouched: rewriting it
    // silently would change the user's configuration behind their back.
    await save(wrapper).trigger('click')
    expect(wrapper.emitted('submit')?.[0]).toEqual([expect.objectContaining({ authMode: 'bearer' })])
  })

  it('opens in edit mode prefilled and keeps the stored key hidden', () => {
    const wrapper = mountDialog({ provider: existing })
    expect(wrapper.find('.mock-title').text()).toBe('编辑模型提供商')
    expect((inputs(wrapper)[0].element as HTMLInputElement).value).toBe('Existing')
    // A stored key reads as dots, not as an empty box: an empty field looks
    // like the key was lost, and the value is never fetched just to fill it.
    expect((inputs(wrapper)[2].element as HTMLInputElement).value).toMatch(/^•+$/)
    expect(inputs(wrapper)[2].attributes('type')).toBe('password')
    expect(mockGet).not.toHaveBeenCalled()
    expect(wrapper.find('[data-section="models"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('m-a')
  })

  it('submits an empty apiKey while the mask is untouched, so the stored key is kept', async () => {
    const wrapper = mountDialog({ provider: existing })
    await save(wrapper).trigger('click')
    expect(wrapper.emitted('submit')?.[0]).toEqual([expect.objectContaining({ apiKey: '' })])
  })

  it('replaces the mask with what the user types instead of appending to it', async () => {
    // Typing over the mask (the field selects it on focus).
    const typed = mountDialog({ provider: existing })
    await inputs(typed)[2].setValue('sk-new')
    expect((inputs(typed)[2].element as HTMLInputElement).value).toBe('sk-new')

    // Clicking into the end of the mask and typing there.
    const appended = mountDialog({ provider: existing })
    await inputs(appended)[2].setValue('••••••••••••tail')
    expect((inputs(appended)[2].element as HTMLInputElement).value).toBe('tail')
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

    expect(wrapper.find('[data-role="key-error"]').text()).toContain('forbidden')
    expect(inputs(wrapper)[2].attributes('type')).toBe('password')
  })

  it('treats an empty reveal answer as a failure, not an empty key', async () => {
    // An older server ignores `reveal` and answers without apiKey; rendering
    // that as "" would look like the stored key had been wiped.
    mockGet.mockResolvedValue({ hasKey: true })
    const wrapper = mountDialog({ provider: existing })

    await wrapper.find('.provider-key-toggle').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-role="key-error"]').text()).toContain('服务端没有返回已保存的密钥')
    expect(inputs(wrapper)[2].attributes('type')).toBe('password')
  })

  it('clears the key only after an explicit action, and a typed key cancels it', async () => {
    const wrapper = mountDialog({ provider: existing })

    // Merely opening the form must not ask for a clear.
    await save(wrapper).trigger('click')
    expect(wrapper.emitted('submit')?.[0]).toEqual([expect.objectContaining({ clearApiKey: false })])

    await wrapper.find('.provider-clear-btn').trigger('click')
    // The dots must not survive the decision to remove the key.
    expect((inputs(wrapper)[2].element as HTMLInputElement).value).toBe('')
    expect(wrapper.find('.provider-clear-pending').exists()).toBe(true)

    // Typing a replacement contradicts the clear: the server applies
    // clearApiKey first and would drop the new secret.
    await inputs(wrapper)[2].setValue('sk-brand-new')
    expect(wrapper.find('.provider-clear-pending').exists()).toBe(false)

    await save(wrapper).trigger('click')
    expect(wrapper.emitted('submit')?.[1]).toEqual([
      expect.objectContaining({ apiKey: 'sk-brand-new', clearApiKey: false }),
    ])
  })

  it('offers undo for a clear that has not been saved yet', async () => {
    const wrapper = mountDialog({ provider: existing })
    await wrapper.find('.provider-clear-btn').trigger('click')
    await wrapper.find('.provider-clear-undo').trigger('click')

    // The mask comes back: the stored key is no longer about to be deleted.
    expect((inputs(wrapper)[2].element as HTMLInputElement).value).toMatch(/^•+$/)
    await save(wrapper).trigger('click')
    expect(wrapper.emitted('submit')?.[0]).toEqual([expect.objectContaining({ clearApiKey: false })])
  })

  it('emits the trimmed values, and blocks submit without a name', async () => {
    const wrapper = mountDialog()

    await save(wrapper).trigger('click')
    expect(wrapper.emitted('submit')).toBeUndefined()
    expect(save(wrapper).attributes('disabled')).toBeDefined()

    await inputs(wrapper)[0].setValue('  My Provider  ')
    await inputs(wrapper)[1].setValue(' https://api.example.com/v1 ')
    await inputs(wrapper)[2].setValue('sk-secret')
    await save(wrapper).trigger('click')

    expect(wrapper.emitted('submit')?.[0]).toEqual([expect.objectContaining({
      name: 'My Provider',
      protocol: 'anthropic',
      baseUrl: 'https://api.example.com/v1',
      apiKey: 'sk-secret',
      authMode: 'auto',
      clearApiKey: false,
    })])
  })

  it('picks a preset from its own view and fills the form in place', async () => {
    const wrapper = mountDialog()
    await wrapper.find('.provider-preset-trigger').trigger('click')

    // A view swap, not a second dialog on top: the form is hidden, the picker
    // is the body, and the footer offers only a way back.
    expect(wrapper.find('.provider-form').exists()).toBe(false)
    expect(wrapper.findComponent(ProviderPresetPicker).exists()).toBe(true)

    await wrapper.find('.preset-row[data-preset-id="deepseek"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('.provider-form').exists()).toBe(true)
    expect((inputs(wrapper)[0].element as HTMLInputElement).value).toBe('DeepSeek')
    expect((inputs(wrapper)[1].element as HTMLInputElement).value).toBe('https://api.deepseek.com/v1')

    await save(wrapper).trigger('click')
    expect(wrapper.emitted('submit')?.[0]).toEqual([expect.objectContaining({ presetId: 'deepseek' })])
  })

  it('opens the model view from the form and keeps the default in range', async () => {
    mockGet.mockResolvedValue({ models: ['m-a', 'm-b', 'm-c'], source: 'live' })
    const wrapper = mountDialog({ provider: existing })
    await wrapper.find('[data-action="provider-models-open"]').trigger('click')
    await flushPromises()

    expect(wrapper.findComponent(ProviderModelsPicker).exists()).toBe(true)
    expect(wrapper.find('.provider-form').exists()).toBe(false)

    // Drop 'm-a' (the current default) and add 'm-c'.
    await wrapper.findAll('.models-check')[0].trigger('click')
    await flushPromises()
    await wrapper.findAll('.models-check')[2].trigger('click')
    await flushPromises()

    await wrapper.find('[data-action="provider-models-done"]').trigger('click')

    // 'm-a' is gone from the set, so the default falls back to the first pick.
    await save(wrapper).trigger('click')
    expect(wrapper.emitted('submit')?.[0]).toEqual([expect.objectContaining({
      models: ['m-b', 'm-c'],
      defaultModel: 'm-b',
    })])
  })

  it('summarizes the selected models and marks the default one', () => {
    const wrapper = mountDialog({ provider: existing })
    const section = wrapper.find('[data-section="models"]')
    expect(section.text()).toContain('已选 2 个')
    expect(section.text()).toContain('默认 m-a')
    expect(section.find('.provider-model-chip--default').text()).toContain('m-a')
  })

  it('drops a model from the summary chip row', async () => {
    const wrapper = mountDialog({ provider: existing })
    await wrapper.findAll('.provider-model-chip-x')[0].trigger('click')

    await save(wrapper).trigger('click')
    expect(wrapper.emitted('submit')?.[0]).toEqual([expect.objectContaining({
      models: ['m-b'],
      defaultModel: 'm-b',
    })])
  })

  it('tests the connection with the values on screen, not just the saved ones', async () => {
    mockPost.mockResolvedValue({ ok: true, status: 200, models: ['a', 'b', 'c'] })
    const wrapper = mountDialog({ provider: existing })
    await inputs(wrapper)[1].setValue('https://typed.example/v1')

    await wrapper.find('[data-action="provider-test-conn"]').trigger('click')
    await flushPromises()

    expect(mockPost).toHaveBeenCalledWith(
      '/api/providers/test',
      expect.objectContaining({ id: 7, protocol: 'openai', baseUrl: 'https://typed.example/v1' }),
      expect.objectContaining({ timeoutMs: 45_000 }),
    )
    expect(wrapper.find('.provider-note--ok').text()).toContain('3')
  })

  it('drops the connection verdict as soon as the endpoint changes', async () => {
    // A green "connection OK" over an edited endpoint claims the new values
    // were tested. Same rule as the list card.
    mockPost.mockResolvedValue({ ok: true, models: ['a'] })
    const wrapper = mountDialog({ provider: existing })
    await wrapper.find('[data-action="provider-test-conn"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('.provider-note--ok').exists()).toBe(true)

    await inputs(wrapper)[1].setValue('https://other.example/v1')
    expect(wrapper.find('.provider-note').exists()).toBe(false)
  })

  it('continues on the model view right after a create', async () => {
    const wrapper = mountDialog()
    await wrapper.setProps({ provider: { ...existing, id: 9, name: 'Fresh', models: [] }, justCreated: true })
    await flushPromises()

    expect(wrapper.find('.provider-created').text()).toContain('已创建')
    expect(wrapper.find('.models-picker').exists()).toBe(true)
    // The picker view only offers a way back; the form's own footer then says
    // "close" instead of "cancel", because the provider already exists.
    await wrapper.find('.provider-back').trigger('click')
    expect(wrapper.find('.provider-form').exists()).toBe(true)
    expect(wrapper.find('[data-action="provider-cancel"]').text()).toBe('关闭')
  })

  it('returns from a picker view with the header back button', async () => {
    const wrapper = mountDialog({ provider: existing })
    await wrapper.find('[data-action="provider-models-open"]').trigger('click')
    expect(wrapper.findComponent(ProviderModelsPicker).exists()).toBe(true)

    await wrapper.find('.provider-back').trigger('click')
    expect(wrapper.findComponent(ProviderModelsPicker).exists()).toBe(false)
    expect(wrapper.find('.provider-form').exists()).toBe(true)
  })

  it('re-seeds from the provider on every open (a cancelled edit cannot leak)', async () => {
    const wrapper = mountDialog({ provider: existing })
    await inputs(wrapper)[0].setValue('Typed but cancelled')
    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    await flushPromises()

    expect((inputs(wrapper)[0].element as HTMLInputElement).value).toBe('Existing')
    // The key field falls back to the stored-key mask, not to a typed value.
    expect((inputs(wrapper)[2].element as HTMLInputElement).value).toMatch(/^•+$/)
  })

  it('shows the server error passed down by the caller', () => {
    const wrapper = mountDialog({ errorMessage: 'name already exists' })
    expect(wrapper.find('.provider-error').text()).toContain('name already exists')
  })

  it('disables the primary button while a save is in flight', () => {
    const wrapper = mountDialog({ saving: true, provider: existing })
    expect(save(wrapper).attributes('disabled')).toBeDefined()
  })

  it('emits close from the cancel button', async () => {
    const wrapper = mountDialog()
    await wrapper.find('[data-action="provider-cancel"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
