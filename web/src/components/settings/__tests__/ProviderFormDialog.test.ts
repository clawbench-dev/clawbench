import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

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
          providerAuthAuto: '自动',
          providerPresetPlaceholder: '从预设选择',
          providerClearKey: '清除密钥',
          providerModelsLabel: '可用模型',
          providerModelsChoose: '选择模型',
          providerModelsNone: '还没有选择模型',
          providerDefaultModel: '默认模型',
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
  it('opens in create mode with blank fields and no edit-only sections', () => {
    const wrapper = mountDialog()
    expect(wrapper.find('.mock-title').text()).toBe('添加模型提供商')
    expect((inputs(wrapper)[0].element as HTMLInputElement).value).toBe('')
    expect((inputs(wrapper)[2].element as HTMLInputElement).value).toBe('')
    expect(wrapper.find('.provider-models').exists()).toBe(false)
    expect(wrapper.find('.provider-clear').exists()).toBe(false)
  })

  it('opens in edit mode prefilled and never echoes a stored key', () => {
    const wrapper = mountDialog({ provider: existing })
    expect(wrapper.find('.mock-title').text()).toBe('编辑模型提供商')
    expect((inputs(wrapper)[0].element as HTMLInputElement).value).toBe('Existing')
    expect((inputs(wrapper)[2].element as HTMLInputElement).value).toBe('')
    expect(wrapper.find('.provider-models').exists()).toBe(true)
    expect(wrapper.text()).toContain('m-a')
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
})
