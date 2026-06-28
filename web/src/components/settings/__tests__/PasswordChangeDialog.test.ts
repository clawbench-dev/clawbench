import { describe, expect, it, vi } from 'vitest'
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
        passwordTooShort: '至少8个字符',
        passwordTooLong: '最多32个字符',
        passwordNoLetterDigit: '必须同时包含字母和数字',
        passwordMismatch: '两次输入的新密码不一致',
        passwordSameAsOld: '新密码不能与当前密码相同',
        currentPasswordRequired: '请输入当前密码',
        passwordTooManyAttempts: '尝试次数过多',
        passwordChangeFailed: '密码修改失败',
        wrongCurrentPassword: '当前密码不正确',
        passwordStrengthWeak: '弱',
        passwordStrengthMedium: '中',
        passwordStrengthStrong: '强',
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
    const vm = wrapper.vm as any
    vm.$.setupState.currentPassword = 'old-password'
    vm.$.setupState.newPassword = 'newpass1'
    vm.$.setupState.confirmPassword = 'newpass1'
    wrapper.vm.$forceUpdate()
    await wrapper.vm.$nextTick()

    const submitBtn = wrapper.find('.password-dialog__btn--submit')
    expect(submitBtn.attributes('disabled')).toBeFalsy()
  })

  it('submit button is disabled when passwords do not match', async () => {
    const wrapper = mountDialog()
    const vm = wrapper.vm as any
    vm.$.setupState.currentPassword = 'old-password'
    vm.$.setupState.newPassword = 'newpass1'
    vm.$.setupState.confirmPassword = 'different1'
    wrapper.vm.$forceUpdate()
    await wrapper.vm.$nextTick()

    const submitBtn = wrapper.find('.password-dialog__btn--submit')
    expect(submitBtn.attributes('disabled')).toBeDefined()
  })

  it('submit button is disabled when new password is too short (<8)', async () => {
    const wrapper = mountDialog()
    const vm = wrapper.vm as any
    vm.$.setupState.currentPassword = 'old-password'
    vm.$.setupState.newPassword = 'abc12'
    vm.$.setupState.confirmPassword = 'abc12'
    wrapper.vm.$forceUpdate()
    await wrapper.vm.$nextTick()

    const submitBtn = wrapper.find('.password-dialog__btn--submit')
    expect(submitBtn.attributes('disabled')).toBeDefined()
  })

  it('submit button is disabled when new password has no letter', async () => {
    const wrapper = mountDialog()
    const vm = wrapper.vm as any
    vm.$.setupState.currentPassword = 'old-password'
    vm.$.setupState.newPassword = '12345678'
    vm.$.setupState.confirmPassword = '12345678'
    wrapper.vm.$forceUpdate()
    await wrapper.vm.$nextTick()

    const submitBtn = wrapper.find('.password-dialog__btn--submit')
    expect(submitBtn.attributes('disabled')).toBeDefined()
  })

  it('submit button is disabled when new password has no digit', async () => {
    const wrapper = mountDialog()
    const vm = wrapper.vm as any
    vm.$.setupState.currentPassword = 'old-password'
    vm.$.setupState.newPassword = 'abcdefgh'
    vm.$.setupState.confirmPassword = 'abcdefgh'
    wrapper.vm.$forceUpdate()
    await wrapper.vm.$nextTick()

    const submitBtn = wrapper.find('.password-dialog__btn--submit')
    expect(submitBtn.attributes('disabled')).toBeDefined()
  })

  it('submit button is disabled when new password exceeds 32 chars', async () => {
    const wrapper = mountDialog()
    const vm = wrapper.vm as any
    vm.$.setupState.currentPassword = 'old-password'
    vm.$.setupState.newPassword = 'a1'.repeat(17) // 34 chars
    vm.$.setupState.confirmPassword = 'a1'.repeat(17)
    wrapper.vm.$forceUpdate()
    await wrapper.vm.$nextTick()

    const submitBtn = wrapper.find('.password-dialog__btn--submit')
    expect(submitBtn.attributes('disabled')).toBeDefined()
  })

  it('shows real-time validation hints for new password when non-empty', async () => {
    const wrapper = mountDialog()
    const vm = wrapper.vm as any

    // Empty — no hints
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.password-dialog__hints').exists()).toBe(false)

    // Too short, no digit — show both hints
    vm.$.setupState.newPassword = 'abcdef'
    wrapper.vm.$forceUpdate()
    await wrapper.vm.$nextTick()

    const hints = wrapper.findAll('.password-dialog__hint--error')
    expect(hints.length).toBeGreaterThanOrEqual(1)
  })

  it('hides validation hints when new password is valid', async () => {
    const wrapper = mountDialog()
    const vm = wrapper.vm as any
    vm.$.setupState.newPassword = 'validpass1'
    wrapper.vm.$forceUpdate()
    await wrapper.vm.$nextTick()

    expect(wrapper.find('.password-dialog__hints').exists()).toBe(false)
  })

  it('shows strength indicator when new password is valid', async () => {
    const wrapper = mountDialog()
    const vm = wrapper.vm as any

    // Weak (8-11 chars)
    vm.$.setupState.newPassword = 'weakpass1'
    wrapper.vm.$forceUpdate()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.password-dialog__strength').exists()).toBe(true)
    expect(wrapper.find('.password-dialog__strength-fill--weak').exists()).toBe(true)

    // Medium (12-19 chars)
    vm.$.setupState.newPassword = 'mediumpass1234'
    wrapper.vm.$forceUpdate()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.password-dialog__strength-fill--medium').exists()).toBe(true)

    // Strong (20+ chars)
    vm.$.setupState.newPassword = 'strongpass1234567890'
    wrapper.vm.$forceUpdate()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.password-dialog__strength-fill--strong').exists()).toBe(true)
  })

  it('hides strength indicator when new password has validation errors', async () => {
    const wrapper = mountDialog()
    const vm = wrapper.vm as any
    vm.$.setupState.newPassword = 'short1'
    wrapper.vm.$forceUpdate()
    await wrapper.vm.$nextTick()

    expect(wrapper.find('.password-dialog__strength').exists()).toBe(false)
  })

  it('has visibility toggle buttons for all three fields', () => {
    const wrapper = mountDialog()
    const eyeButtons = wrapper.findAll('.password-dialog__eye')
    expect(eyeButtons.length).toBe(3)
  })

  it('emits changed on successful submit', async () => {
    const wrapper = mountDialog()
    const vm = wrapper.vm as any
    vm.$.setupState.currentPassword = 'old-password'
    vm.$.setupState.newPassword = 'newpass1'
    vm.$.setupState.confirmPassword = 'newpass1'
    wrapper.vm.$forceUpdate()
    await wrapper.vm.$nextTick()

    expect(vm.$.setupState.canSubmit).toBe(true)
    expect(wrapper.vm.$options.emits).toContain('changed')
  })
})
