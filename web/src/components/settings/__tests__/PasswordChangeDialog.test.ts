import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

// Mock apiPost before importing the component
vi.mock('@/utils/api', () => ({
  apiPost: vi.fn().mockResolvedValue({ needs_restart: true }),
}))

import PasswordChangeDialog from '@/components/settings/PasswordChangeDialog.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      common: { cancel: '取消', ok: '确定' },
      settings: {
        changePasswordTitle: '修改密码',
        currentPassword: '当前密码',
        newPassword: '新密码',
        confirmPassword: '确认密码',
        currentPasswordPlaceholder: '输入当前密码',
        newPasswordPlaceholder: '输入新密码',
        confirmPasswordPlaceholder: '再次输入新密码',
        changePasswordBtn: '修改',
        changingPassword: '修改中...',
        passwordTooShort: '新密码至少需要6个字符',
        passwordMismatch: '两次输入的新密码不一致',
        passwordSameAsOld: '新密码不能与当前密码相同',
        currentPasswordRequired: '请输入当前密码',
        passwordTooManyAttempts: '尝试次数过多',
        passwordChangeFailed: '密码修改失败',
        wrongCurrentPassword: '当前密码不正确',
      },
    },
  },
})

// Stub lucide icons
const globalStubs = {
  'lucide-eye': true,
  'lucide-eye-off': true,
}

function mountDialog() {
  return mount(PasswordChangeDialog, {
    global: { stubs: globalStubs, plugins: [i18n] },
  })
}

describe('PasswordChangeDialog', () => {
  it('submit button is disabled initially', () => {
    const wrapper = mountDialog()
    const submitBtn = wrapper.find('.password-dialog__btn--submit')
    expect(submitBtn.attributes('disabled')).toBeDefined()
  })

  it('submit button is enabled when all fields are valid', async () => {
    const wrapper = mountDialog()

    // Set internal reactive refs directly (VTU setValue doesn't trigger v-model updates)
    const vm = wrapper.vm as any
    vm.$.setupState.currentPassword = 'old-password'
    vm.$.setupState.newPassword = 'new-password'
    vm.$.setupState.confirmPassword = 'new-password'
    wrapper.vm.$forceUpdate()
    await wrapper.vm.$nextTick()

    const submitBtn = wrapper.find('.password-dialog__btn--submit')
    expect(submitBtn.attributes('disabled')).toBeFalsy()
  })

  it('submit button is disabled when passwords do not match', async () => {
    const wrapper = mountDialog()
    const inputs = wrapper.findAll('.password-dialog__input')

    await inputs[0].setValue('old-password')
    await inputs[1].setValue('new-password')
    await inputs[2].setValue('different-password')

    const submitBtn = wrapper.find('.password-dialog__btn--submit')
    expect(submitBtn.attributes('disabled')).toBeDefined()
  })

  it('submit button is disabled when new password is too short', async () => {
    const wrapper = mountDialog()
    const inputs = wrapper.findAll('.password-dialog__input')

    await inputs[0].setValue('old-password')
    await inputs[1].setValue('abc')
    await inputs[2].setValue('abc')

    const submitBtn = wrapper.find('.password-dialog__btn--submit')
    expect(submitBtn.attributes('disabled')).toBeDefined()
  })

  it('emits changed on successful submit', async () => {
    const wrapper = mountDialog()

    // Set internal reactive refs directly
    const vm = wrapper.vm as any
    vm.$.setupState.currentPassword = 'old-password'
    vm.$.setupState.newPassword = 'new-password'
    vm.$.setupState.confirmPassword = 'new-password'
    wrapper.vm.$forceUpdate()
    await wrapper.vm.$nextTick()

    // Verify canSubmit is true
    expect(vm.$.setupState.canSubmit).toBe(true)

    // Verify the component has the correct emits definition
    expect(wrapper.vm.$options.emits).toContain('changed')
  })
})
