import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

// Mock apiPost before importing the component
vi.mock('@/utils/api', () => ({
  apiPost: vi.fn().mockResolvedValue({ needs_restart: true }),
}))

import PasswordChangeDialog from '@/components/settings/PasswordChangeDialog.vue'
import { apiPost } from '@/utils/api'

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

function getState(wrapper: ReturnType<typeof mount>) {
  return (wrapper.vm as any).$.setupState
}

function fillValidFields(wrapper: ReturnType<typeof mount>) {
  const s = getState(wrapper)
  s.currentPassword = 'old-password'
  s.newPassword = 'newpass1'
  s.confirmPassword = 'newpass1'
}

async function refresh(wrapper: ReturnType<typeof mount>) {
  wrapper.vm.$forceUpdate()
  await wrapper.vm.$nextTick()
}

describe('PasswordChangeDialog', () => {
  it('submit button is disabled initially', () => {
    const wrapper = mountDialog()
    const submitBtn = wrapper.find('.password-dialog__btn--submit')
    expect(submitBtn.attributes('disabled')).toBeDefined()
  })

  it('submit button is enabled when all fields are valid', async () => {
    const wrapper = mountDialog()
    fillValidFields(wrapper)
    await refresh(wrapper)

    const submitBtn = wrapper.find('.password-dialog__btn--submit')
    expect(submitBtn.attributes('disabled')).toBeFalsy()
  })

  it('submit button is disabled when passwords do not match', async () => {
    const wrapper = mountDialog()
    const s = getState(wrapper)
    s.currentPassword = 'old-password'
    s.newPassword = 'newpass1'
    s.confirmPassword = 'different1'
    await refresh(wrapper)

    expect(wrapper.find('.password-dialog__btn--submit').attributes('disabled')).toBeDefined()
  })

  it('submit button is disabled when new password is too short (<8)', async () => {
    const wrapper = mountDialog()
    const s = getState(wrapper)
    s.currentPassword = 'old-password'
    s.newPassword = 'abc12'
    s.confirmPassword = 'abc12'
    await refresh(wrapper)

    expect(wrapper.find('.password-dialog__btn--submit').attributes('disabled')).toBeDefined()
  })

  it('submit button is disabled when new password has no letter', async () => {
    const wrapper = mountDialog()
    const s = getState(wrapper)
    s.currentPassword = 'old-password'
    s.newPassword = '12345678'
    s.confirmPassword = '12345678'
    await refresh(wrapper)

    expect(wrapper.find('.password-dialog__btn--submit').attributes('disabled')).toBeDefined()
  })

  it('submit button is disabled when new password has no digit', async () => {
    const wrapper = mountDialog()
    const s = getState(wrapper)
    s.currentPassword = 'old-password'
    s.newPassword = 'abcdefgh'
    s.confirmPassword = 'abcdefgh'
    await refresh(wrapper)

    expect(wrapper.find('.password-dialog__btn--submit').attributes('disabled')).toBeDefined()
  })

  it('submit button is disabled when new password exceeds 32 chars', async () => {
    const wrapper = mountDialog()
    const s = getState(wrapper)
    s.currentPassword = 'old-password'
    s.newPassword = 'a1'.repeat(17)
    s.confirmPassword = 'a1'.repeat(17)
    await refresh(wrapper)

    expect(wrapper.find('.password-dialog__btn--submit').attributes('disabled')).toBeDefined()
  })

  it('shows real-time validation hints for new password when non-empty', async () => {
    const wrapper = mountDialog()
    const s = getState(wrapper)

    await wrapper.vm.$nextTick()
    expect(wrapper.find('.password-dialog__hints').exists()).toBe(false)

    s.newPassword = 'abcdef'
    await refresh(wrapper)

    const hints = wrapper.findAll('.password-dialog__hint--error')
    expect(hints.length).toBeGreaterThanOrEqual(1)
  })

  it('hides validation hints when new password is valid', async () => {
    const wrapper = mountDialog()
    const s = getState(wrapper)
    s.newPassword = 'validpass1'
    await refresh(wrapper)

    expect(wrapper.find('.password-dialog__hints').exists()).toBe(false)
  })

  it('shows strength indicator when new password is valid', async () => {
    const wrapper = mountDialog()
    const s = getState(wrapper)

    s.newPassword = 'weakpass1'
    await refresh(wrapper)
    expect(wrapper.find('.password-dialog__strength').exists()).toBe(true)
    expect(wrapper.find('.password-dialog__strength-fill--weak').exists()).toBe(true)

    s.newPassword = 'mediumpass1234'
    await refresh(wrapper)
    expect(wrapper.find('.password-dialog__strength-fill--medium').exists()).toBe(true)

    s.newPassword = 'strongpass1234567890'
    await refresh(wrapper)
    expect(wrapper.find('.password-dialog__strength-fill--strong').exists()).toBe(true)
  })

  it('hides strength indicator when new password has validation errors', async () => {
    const wrapper = mountDialog()
    const s = getState(wrapper)
    s.newPassword = 'short1'
    await refresh(wrapper)

    expect(wrapper.find('.password-dialog__strength').exists()).toBe(false)
  })

  it('has visibility toggle buttons for all three fields', () => {
    const wrapper = mountDialog()
    const eyeButtons = wrapper.findAll('.password-dialog__eye')
    expect(eyeButtons.length).toBe(3)
  })

  it('emits changed on successful submit', async () => {
    const wrapper = mountDialog()
    fillValidFields(wrapper)
    await refresh(wrapper)

    const s = getState(wrapper)
    expect(s.canSubmit).toBe(true)
    expect(wrapper.vm.$options.emits).toContain('changed')
  })

  // --- Submit flow tests ---

  it('calls apiPost and emits changed on successful submit', async () => {
    vi.mocked(apiPost).mockResolvedValueOnce({ needs_restart: true })
    const wrapper = mountDialog()
    fillValidFields(wrapper)
    await refresh(wrapper)

    await getState(wrapper).submit()
    await refresh(wrapper)

    expect(apiPost).toHaveBeenCalledWith('/api/config/password', {
      current_password: 'old-password',
      new_password: 'newpass1',
    })
    expect(wrapper.emitted('changed')).toBeTruthy()
    expect(wrapper.emitted('changed')![0]).toEqual([true])
  })

  it('emits changed with false when server returns needs_restart=false', async () => {
    vi.mocked(apiPost).mockResolvedValueOnce({ needs_restart: false })
    const wrapper = mountDialog()
    fillValidFields(wrapper)
    await refresh(wrapper)

    await getState(wrapper).submit()
    await refresh(wrapper)

    expect(wrapper.emitted('changed')![0]).toEqual([false])
  })

  it('sets localError when current password is empty on submit', async () => {
    const wrapper = mountDialog()
    const s = getState(wrapper)
    s.currentPassword = ''
    s.newPassword = 'newpass1'
    s.confirmPassword = 'newpass1'
    await refresh(wrapper)

    await s.submit()
    await refresh(wrapper)

    expect(s.localError).toContain('请输入当前密码')
  })

  it('sets localError when new password matches current password', async () => {
    const wrapper = mountDialog()
    const s = getState(wrapper)
    s.currentPassword = 'samepass1'
    s.newPassword = 'samepass1'
    s.confirmPassword = 'samepass1'
    await refresh(wrapper)

    await s.submit()
    await refresh(wrapper)

    expect(s.localError).toContain('新密码不能与当前密码相同')
  })

  it('sets serverError for wrong_password', async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error('wrong_password'))
    const wrapper = mountDialog()
    fillValidFields(wrapper)
    await refresh(wrapper)

    await getState(wrapper).submit()
    await refresh(wrapper)

    expect(getState(wrapper).serverError).toContain('当前密码不正确')
  })

  it('sets serverError for TooManyLoginAttempts', async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error('TooManyLoginAttempts'))
    const wrapper = mountDialog()
    fillValidFields(wrapper)
    await refresh(wrapper)

    await getState(wrapper).submit()
    await refresh(wrapper)

    expect(getState(wrapper).serverError).toContain('尝试次数过多')
  })

  it('sets serverError for Too Many Requests in message', async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error('429 Too Many Requests'))
    const wrapper = mountDialog()
    fillValidFields(wrapper)
    await refresh(wrapper)

    await getState(wrapper).submit()
    await refresh(wrapper)

    expect(getState(wrapper).serverError).toContain('尝试次数过多')
  })

  it('sets generic serverError for unknown error', async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error('network_error'))
    const wrapper = mountDialog()
    fillValidFields(wrapper)
    await refresh(wrapper)

    await getState(wrapper).submit()
    await refresh(wrapper)

    expect(getState(wrapper).serverError).toContain('密码修改失败')
  })

  it('sets serverError for password_too_short', async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error('password_too_short'))
    const wrapper = mountDialog()
    fillValidFields(wrapper)
    await refresh(wrapper)

    await getState(wrapper).submit()
    await refresh(wrapper)

    expect(getState(wrapper).serverError).toContain('至少8个字符')
  })

  it('sets serverError for password_too_long', async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error('password_too_long'))
    const wrapper = mountDialog()
    fillValidFields(wrapper)
    await refresh(wrapper)

    await getState(wrapper).submit()
    await refresh(wrapper)

    expect(getState(wrapper).serverError).toContain('最多32个字符')
  })

  it('sets serverError for password_no_letter_digit', async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error('password_no_letter_digit'))
    const wrapper = mountDialog()
    fillValidFields(wrapper)
    await refresh(wrapper)

    await getState(wrapper).submit()
    await refresh(wrapper)

    expect(getState(wrapper).serverError).toContain('必须同时包含字母和数字')
  })

  it('sets serverError for empty_password', async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error('empty_password'))
    const wrapper = mountDialog()
    fillValidFields(wrapper)
    await refresh(wrapper)

    await getState(wrapper).submit()
    await refresh(wrapper)

    expect(getState(wrapper).serverError).toContain('请输入当前密码')
  })

  // --- Close behavior ---

  it('emits close when handleClose is called and not submitting', async () => {
    const wrapper = mountDialog()
    await getState(wrapper).handleClose()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('does not emit close when handleClose is called while submitting', async () => {
    vi.mocked(apiPost).mockImplementation(() => new Promise(() => {}))
    const wrapper = mountDialog()
    fillValidFields(wrapper)
    await refresh(wrapper)

    // Start submit (will hang)
    const _ = getState(wrapper).submit()
    await wrapper.vm.$nextTick()

    // Try to close while submitting
    await getState(wrapper).handleClose()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('close')).toBeFalsy()

    // Clean up
    getState(wrapper).submitting = false
  })

  // --- Confirm password real-time validation ---

  it('shows confirm password mismatch error in real-time', async () => {
    const wrapper = mountDialog()
    const s = getState(wrapper)
    s.newPassword = 'newpass1'
    s.confirmPassword = 'different1'
    await refresh(wrapper)

    const hint = wrapper.find('.password-dialog__hint--error')
    expect(hint.exists()).toBe(true)
    expect(hint.text()).toContain('两次输入的新密码不一致')
  })

  it('hides confirm password error when passwords match', async () => {
    const wrapper = mountDialog()
    const s = getState(wrapper)
    s.newPassword = 'newpass1'
    s.confirmPassword = 'newpass1'
    await refresh(wrapper)

    const allHints = wrapper.findAll('.password-dialog__hint--error')
    const mismatchHint = allHints.find(h => h.text().includes('两次输入的新密码不一致'))
    expect(mismatchHint).toBeUndefined()
  })

  // --- Submitting state ---

  it('resets submitting state after submit succeeds', async () => {
    vi.mocked(apiPost).mockResolvedValueOnce({ needs_restart: true })
    const wrapper = mountDialog()
    fillValidFields(wrapper)
    await refresh(wrapper)

    await getState(wrapper).submit()
    await refresh(wrapper)

    expect(getState(wrapper).submitting).toBe(false)
  })

  it('resets submitting state after submit fails', async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error('fail'))
    const wrapper = mountDialog()
    fillValidFields(wrapper)
    await refresh(wrapper)

    await getState(wrapper).submit()
    await refresh(wrapper)

    expect(getState(wrapper).submitting).toBe(false)
  })

  // --- Cancel/overlay click ---

  it('cancel button emits close', async () => {
    const wrapper = mountDialog()
    const cancelBtn = wrapper.find('.password-dialog__btn--cancel')
    await cancelBtn.trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('clicking overlay emits close', async () => {
    const wrapper = mountDialog()
    const overlay = wrapper.find('.password-dialog-overlay')
    await overlay.trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('close')).toBeTruthy()
  })
})
