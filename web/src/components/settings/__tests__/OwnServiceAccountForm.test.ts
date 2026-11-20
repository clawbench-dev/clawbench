import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

const mockPost = vi.fn()
const mockPut = vi.fn()

vi.mock('@/utils/api', () => ({
  apiPost: (...args: unknown[]) => mockPost(...args),
  apiPut: (...args: unknown[]) => mockPut(...args),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import OwnServiceAccountForm from '@/components/settings/OwnServiceAccountForm.vue'
import type { ProviderRow } from '@/components/settings/ProviderFormDialog.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        items: {
          providerName: '名称',
          providerKey: 'API Key',
          providerKeyStateSaved: '已保存',
          providerKeyStateMissing: '未配置',
          providerKeyHide: '隐藏密钥',
          providerKeyReveal: '查看密钥',
          providerSave: '保存',
          providerAdd: '添加',
          codebuddyFormHint: 'CodeBuddy 账号说明',
          codebuddyProtocolType: '协议类型',
          codebuddyDeploymentHint: '版本说明',
          codebuddyDeploymentPublic: '公有云账号',
          codebuddyDeploymentInternal: '中国版',
          codebuddyDeploymentIOA: 'iOA 版',
          codebuddyDeploymentDedicated: '专享版（企业域名）',
          codebuddyDeploymentSelfHosted: '私有化部署（企业域名）',
          codebuddyDomain: '企业域名',
          codebuddyDomainPlaceholder: 'https://your-company.example.com',
          codebuddyDomainHint: '域名说明',
          codebuddyKeyKeep: '已保存，留空表示不修改',
          codebuddySaved: '已保存',
          accountApplyButton: '应用到现有智能体',
          accountKeyOnlyHint: '{name} 只需要密钥',
          accountNameCodeBuddy: 'CodeBuddy',
          accountNameCursor: 'Cursor',
          accountNameCommandCode: 'Command Code',
          accountNameAntigravity: 'Antigravity',
        },
      },
    },
  },
})

function row(overrides: Partial<ProviderRow> = {}): ProviderRow {
  return {
    id: 7,
    name: 'CodeBuddy',
    protocol: 'private',
    backend: 'codebuddy',
    baseUrl: '',
    authMode: 'auto',
    hasKey: true,
    agentCount: 0,
    ...overrides,
  }
}

function mountForm(backend = 'codebuddy', providers: ProviderRow[] = [], selectedId: number | null = null) {
  return mount(OwnServiceAccountForm, {
    props: { backend, providers, selectedId },
    global: { plugins: [i18n] },
  })
}

beforeEach(() => {
  mockPost.mockReset()
  mockPut.mockReset()
})

describe('OwnServiceAccountForm', () => {
  it('shows the hosted edition by default and hides the domain box', () => {
    const wrapper = mountForm()
    const select = wrapper.find<HTMLSelectElement>('[data-role="account-deployment"]')
    expect(select.exists()).toBe(true)
    expect(select.element.value).toBe('')
    expect(wrapper.find('[data-role="account-domain"]').exists()).toBe(false)
  })

  it('asks for the company domain only for the enterprise editions', async () => {
    const wrapper = mountForm()
    const select = wrapper.find('[data-role="account-deployment"]')

    await select.setValue('cloudhosted')
    expect(wrapper.find('[data-role="account-domain"]').exists()).toBe(true)

    await select.setValue('selfhosted')
    expect(wrapper.find('[data-role="account-domain"]').exists()).toBe(true)

    await select.setValue('internal')
    expect(wrapper.find('[data-role="account-domain"]').exists()).toBe(false)
  })

  it('creates a private-protocol provider owned by the panel backend', async () => {
    mockPost.mockResolvedValue({ id: 12 })
    const wrapper = mountForm()

    await wrapper.find('[data-role="account-name"]').setValue('公司 CodeBuddy')
    await wrapper.find('[data-role="account-deployment"]').setValue('selfhosted')
    await wrapper.find('[data-role="account-domain"]').setValue('https://cb.example.com')
    await wrapper.find('[data-role="account-key"]').setValue('ck-1')
    await wrapper.find('[data-action="account-save"]').trigger('click')
    await flushPromises()

    expect(mockPost).toHaveBeenCalledWith('/api/providers', {
      name: '公司 CodeBuddy',
      protocol: 'private',
      backend: 'codebuddy',
      deployment: 'selfhosted',
      baseUrl: 'https://cb.example.com',
      apiKey: 'ck-1',
    })
    // The id travels with the event: the parent keeps that row selected so the
    // panel does not jump to another account after the list reloads.
    expect(wrapper.emitted('saved')).toEqual([[12]])
  })

  it('cannot save an enterprise edition without a usable address', async () => {
    const wrapper = mountForm()
    await wrapper.find('[data-role="account-deployment"]').setValue('cloudhosted')
    await wrapper.find('[data-role="account-key"]').setValue('ck-1')

    const save = wrapper.find('[data-action="account-save"]')
    expect(save.attributes('disabled')).toBeDefined()

    // A bare host is not a URL; the CLI needs a scheme.
    await wrapper.find('[data-role="account-domain"]').setValue('cb.example.com')
    expect(save.attributes('disabled')).toBeDefined()

    await wrapper.find('[data-role="account-domain"]').setValue('https://cb.example.com')
    expect(save.attributes('disabled')).toBeUndefined()
  })

  it('edits the existing account and keeps the stored key when the box is empty', async () => {
    mockPut.mockResolvedValue({ id: 7 })
    const wrapper = mountForm('codebuddy', [row({ deployment: 'cloudhosted', baseUrl: 'https://cb.example.com' })], 7)

    expect(wrapper.find<HTMLInputElement>('[data-role="account-name"]').element.value).toBe('CodeBuddy')
    expect(wrapper.find<HTMLSelectElement>('[data-role="account-deployment"]').element.value).toBe('cloudhosted')

    await wrapper.find('[data-action="account-save"]').trigger('click')
    await flushPromises()

    expect(mockPut).toHaveBeenCalledWith('/api/providers/7', {
      name: 'CodeBuddy',
      protocol: 'private',
      backend: 'codebuddy',
      deployment: 'cloudhosted',
      baseUrl: 'https://cb.example.com',
      apiKey: '',
    })
  })

  it('drops the address when switching back to a fixed-host edition', async () => {
    mockPut.mockResolvedValue({ id: 7 })
    const wrapper = mountForm('codebuddy', [row({ deployment: 'selfhosted', baseUrl: 'https://cb.example.com' })], 7)

    await wrapper.find('[data-role="account-deployment"]').setValue('internal')
    await wrapper.find('[data-action="account-save"]').trigger('click')
    await flushPromises()

    expect(mockPut).toHaveBeenCalledWith('/api/providers/7', expect.objectContaining({
      deployment: 'internal',
      baseUrl: '',
    }))
  })

  // Cursor / Command Code / Antigravity have nothing beyond the key, so their
  // panel has no edition dropdown at all.
  it('is only a key box for a backend without editions', async () => {
    const wrapper = mountForm('cursor')
    expect(wrapper.find('[data-role="account-deployment"]').exists()).toBe(false)
    expect(wrapper.find('[data-role="account-domain"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Cursor 只需要密钥')

    await wrapper.find('[data-role="account-key"]').setValue('cursor-key')
    await wrapper.find('[data-action="account-save"]').trigger('click')
    await flushPromises()

    expect(mockPost).toHaveBeenCalledWith('/api/providers', expect.objectContaining({
      protocol: 'private',
      backend: 'cursor',
      deployment: '',
      baseUrl: '',
    }))
  })

  it('ignores another backend\'s account', () => {
    // A Cursor key must not show up in the CodeBuddy panel (and vice versa).
    const wrapper = mountForm('codebuddy', [row({ id: 3, name: 'Cursor key', backend: 'cursor' })])
    expect(wrapper.find<HTMLInputElement>('[data-role="account-name"]').element.value).toBe('CodeBuddy')
  })

  it('offers the apply button only for an account that already exists', async () => {
    // Nothing to bind before the row exists — the button would have no id.
    expect(mountForm().find('[data-action="account-apply"]').exists()).toBe(false)

    const wrapper = mountForm('codebuddy', [row()], 7)
    const apply = wrapper.find('[data-action="account-apply"]')
    expect(apply.exists()).toBe(true)

    await apply.trigger('click')
    expect(wrapper.emitted('apply')).toEqual([[7]])
  })
})
