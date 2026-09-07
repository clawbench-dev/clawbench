import { describe, expect, it, vi, afterEach, beforeEach } from 'vitest'
import { mount, VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'

// Mock apiPost before importing the component
vi.mock('@/utils/api', () => ({
  apiPost: vi.fn().mockResolvedValue({ needs_restart: true }),
}))

import PasswordChangeDialog from '@/components/settings/PasswordChangeDialog.vue'
import { apiPost } from '@/utils/api'

// PasswordChangeDialog renders inside ModalDialog which Teleports its overlay
// to <body>, so element lookups must go through document.body — the dialog
// content is not part of the component's own wrapper tree.
function $(selector: string): HTMLElement | null {
  return document.body.querySelector(selector)
}

function $$(selector: string): HTMLElement[] {
  return Array.from(document.body.querySelectorAll(selector))
}

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

let wrapper: VueWrapper | null = null
let host: HTMLDivElement | null = null

beforeEach(() => {
  vi.useRealTimers()
})

afterEach(() => {
  vi.useRealTimers()
  if (wrapper) {
    wrapper.unmount()
    wrapper = null
  }
  if (host?.parentNode) host.parentNode.removeChild(host)
  host = null
  document.body.querySelectorAll('.modal-overlay').forEach((el) => el.remove())
})

function mountDialog() {
  host = document.createElement('div')
  document.body.appendChild(host)
  wrapper = mount(PasswordChangeDialog, {
    props: { open: true },
    attachTo: host,
    global: { stubs: globalStubs, plugins: [i18n] },
  })
  return wrapper
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
  it('renders inside the public ModalDialog (teleported to body)', async () => {
    mountDialog()
    await nextTick()
    expect($('.modal-overlay')).toBeTruthy()
    expect($('.password-dialog__input')).toBeTruthy()
  })

  it('submit button is disabled initially', () => {
    mountDialog()
    const submitBtn = $('.fbtn-primary')!
    expect(submitBtn.hasAttribute('disabled')).toBe(true)
  })

  it('submit button is enabled when all fields are valid', async () => {
    const w = mountDialog()!
    fillValidFields(w)
    await refresh(w)

    const submitBtn = $('.fbtn-primary')!
    expect(submitBtn.hasAttribute('disabled')).toBe(false)
  })

  it('submit button is disabled when passwords do not match', async () => {
    const w = mountDialog()!
    const s = getState(w)
    s.currentPassword = 'old-password'
    s.newPassword = 'newpass1'
    s.confirmPassword = 'different1'
    await refresh(w)

    const submitBtn = $('.fbtn-primary')!
    expect(submitBtn.hasAttribute('disabled')).toBe(true)
  })

  it('submit button is disabled when new password is too short (<8)', async () => {
    const w = mountDialog()!
    const s = getState(w)
    s.currentPassword = 'old-password'
    s.newPassword = 'abc12'
    s.confirmPassword = 'abc12'
    await refresh(w)

    const submitBtn = $('.fbtn-primary')!
    expect(submitBtn.hasAttribute('disabled')).toBe(true)
  })

  it('submit button is disabled when new password has no letter', async () => {
    const w = mountDialog()!
    const s = getState(w)
    s.currentPassword = 'old-password'
    s.newPassword = '12345678'
    s.confirmPassword = '12345678'
    await refresh(w)

    const submitBtn = $('.fbtn-primary')!
    expect(submitBtn.hasAttribute('disabled')).toBe(true)
  })

  it('submit button is disabled when new password has no digit', async () => {
    const w = mountDialog()!
    const s = getState(w)
    s.currentPassword = 'old-password'
    s.newPassword = 'abcdefgh'
    s.confirmPassword = 'abcdefgh'
    await refresh(w)

    const submitBtn = $('.fbtn-primary')!
    expect(submitBtn.hasAttribute('disabled')).toBe(true)
  })

  it('submit button is disabled when new password exceeds 32 chars', async () => {
    const w = mountDialog()!
    const s = getState(w)
    s.currentPassword = 'old-password'
    s.newPassword = 'a1'.repeat(17)
    s.confirmPassword = 'a1'.repeat(17)
    await refresh(w)

    const submitBtn = $('.fbtn-primary')!
    expect(submitBtn.hasAttribute('disabled')).toBe(true)
  })

  it('shows real-time validation hints for new password when non-empty', async () => {
    const w = mountDialog()!
    const s = getState(w)

    await w.vm.$nextTick()
    expect($('.password-dialog__hints')).toBeFalsy()

    s.newPassword = 'abcdef'
    await refresh(w)

    const hints = $$('.password-dialog__hint--error')
    expect(hints.length).toBeGreaterThanOrEqual(1)
  })

  it('hides validation hints when new password is valid', async () => {
    const w = mountDialog()!
    const s = getState(w)
    s.newPassword = 'validpass1'
    await refresh(w)

    expect($('.password-dialog__hints')).toBeFalsy()
  })

  it('shows strength indicator when new password is valid', async () => {
    const w = mountDialog()!
    const s = getState(w)

    s.newPassword = 'weakpass1'
    await refresh(w)
    expect($('.password-dialog__strength')).toBeTruthy()
    expect($('.password-dialog__strength-fill--weak')).toBeTruthy()

    s.newPassword = 'mediumpass1234'
    await refresh(w)
    expect($('.password-dialog__strength-fill--medium')).toBeTruthy()

    s.newPassword = 'strongpass1234567890'
    await refresh(w)
    expect($('.password-dialog__strength-fill--strong')).toBeTruthy()
  })

  it('hides strength indicator when new password has validation errors', async () => {
    const w = mountDialog()!
    const s = getState(w)
    s.newPassword = 'short1'
    await refresh(w)

    expect($('.password-dialog__strength')).toBeFalsy()
  })

  it('has visibility toggle buttons for all three fields', () => {
    mountDialog()
    const eyeButtons = $$('.password-dialog__eye')
    expect(eyeButtons.length).toBe(3)
  })

  // --- Submit flow tests ---

  it('calls apiPost and emits changed on successful submit', async () => {
    vi.mocked(apiPost).mockResolvedValueOnce({ needs_restart: true })
    const w = mountDialog()!
    fillValidFields(w)
    await refresh(w)

    await getState(w).submit()
    await refresh(w)

    expect(apiPost).toHaveBeenCalledWith('/api/config/password', {
      current_password: 'old-password',
      new_password: 'newpass1',
    })
    expect(w.emitted('changed')).toBeTruthy()
    expect(w.emitted('changed')![0]).toEqual([true])
  })

  it('emits changed with false when server returns needs_restart=false', async () => {
    vi.mocked(apiPost).mockResolvedValueOnce({ needs_restart: false })
    const w = mountDialog()!
    fillValidFields(w)
    await refresh(w)

    await getState(w).submit()
    await refresh(w)

    expect(w.emitted('changed')![0]).toEqual([false])
  })

  it('sets localError when current password is empty on submit', async () => {
    const w = mountDialog()!
    const s = getState(w)
    s.currentPassword = ''
    s.newPassword = 'newpass1'
    s.confirmPassword = 'newpass1'
    await refresh(w)

    await s.submit()
    await refresh(w)

    expect(s.localError).toContain('请输入当前密码')
  })

  it('sets localError when new password matches current password', async () => {
    const w = mountDialog()!
    const s = getState(w)
    s.currentPassword = 'samepass1'
    s.newPassword = 'samepass1'
    s.confirmPassword = 'samepass1'
    await refresh(w)

    await s.submit()
    await refresh(w)

    expect(s.localError).toContain('新密码不能与当前密码相同')
  })

  it('sets serverError for wrong_password', async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error('wrong_password'))
    const w = mountDialog()!
    fillValidFields(w)
    await refresh(w)

    await getState(w).submit()
    await refresh(w)

    expect(getState(w).serverError).toContain('当前密码不正确')
  })

  it('sets serverError for TooManyLoginAttempts', async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error('TooManyLoginAttempts'))
    const w = mountDialog()!
    fillValidFields(w)
    await refresh(w)

    await getState(w).submit()
    await refresh(w)

    expect(getState(w).serverError).toContain('尝试次数过多')
  })

  it('sets serverError for Too Many Requests in message', async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error('429 Too Many Requests'))
    const w = mountDialog()!
    fillValidFields(w)
    await refresh(w)

    await getState(w).submit()
    await refresh(w)

    expect(getState(w).serverError).toContain('尝试次数过多')
  })

  it('sets generic serverError for unknown error', async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error('network_error'))
    const w = mountDialog()!
    fillValidFields(w)
    await refresh(w)

    await getState(w).submit()
    await refresh(w)

    expect(getState(w).serverError).toContain('密码修改失败')
  })

  it('sets serverError for password_too_short', async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error('password_too_short'))
    const w = mountDialog()!
    fillValidFields(w)
    await refresh(w)

    await getState(w).submit()
    await refresh(w)

    expect(getState(w).serverError).toContain('至少8个字符')
  })

  it('sets serverError for password_too_long', async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error('password_too_long'))
    const w = mountDialog()!
    fillValidFields(w)
    await refresh(w)

    await getState(w).submit()
    await refresh(w)

    expect(getState(w).serverError).toContain('最多32个字符')
  })

  it('sets serverError for password_no_letter_digit', async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error('password_no_letter_digit'))
    const w = mountDialog()!
    fillValidFields(w)
    await refresh(w)

    await getState(w).submit()
    await refresh(w)

    expect(getState(w).serverError).toContain('必须同时包含字母和数字')
  })

  it('sets serverError for empty_password', async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error('empty_password'))
    const w = mountDialog()!
    fillValidFields(w)
    await refresh(w)

    await getState(w).submit()
    await refresh(w)

    expect(getState(w).serverError).toContain('请输入当前密码')
  })

  // --- Close behavior ---

  it('emits close when handleClose is called and not submitting', async () => {
    const w = mountDialog()!
    await getState(w).handleClose()
    await w.vm.$nextTick()

    expect(w.emitted('close')).toBeTruthy()
  })

  it('does not emit close when handleClose is called while submitting', async () => {
    vi.mocked(apiPost).mockImplementation(() => new Promise(() => {}))
    const w = mountDialog()!
    fillValidFields(w)
    await refresh(w)

    // Start submit (will hang)
    const _ = getState(w).submit()
    await w.vm.$nextTick()

    // Try to close while submitting
    await getState(w).handleClose()
    await w.vm.$nextTick()

    expect(w.emitted('close')).toBeFalsy()

    // Clean up
    getState(w).submitting = false
  })

  // --- Confirm password real-time validation ---

  it('shows confirm password mismatch error in real-time', async () => {
    const w = mountDialog()!
    const s = getState(w)
    s.newPassword = 'newpass1'
    s.confirmPassword = 'different1'
    await refresh(w)

    const hint = $('.password-dialog__hint--error')
    expect(hint).toBeTruthy()
    expect(hint!.textContent).toContain('两次输入的新密码不一致')
  })

  it('hides confirm password error when passwords match', async () => {
    const w = mountDialog()!
    const s = getState(w)
    s.newPassword = 'newpass1'
    s.confirmPassword = 'newpass1'
    await refresh(w)

    const allHints = $$('.password-dialog__hint--error')
    const mismatchHint = allHints.find(h => h.textContent?.includes('两次输入的新密码不一致'))
    expect(mismatchHint).toBeUndefined()
  })

  // --- Submitting state ---

  it('resets submitting state after submit succeeds', async () => {
    vi.mocked(apiPost).mockResolvedValueOnce({ needs_restart: true })
    const w = mountDialog()!
    fillValidFields(w)
    await refresh(w)

    await getState(w).submit()
    await refresh(w)

    expect(getState(w).submitting).toBe(false)
  })

  it('resets submitting state after submit fails', async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error('fail'))
    const w = mountDialog()!
    fillValidFields(w)
    await refresh(w)

    await getState(w).submit()
    await refresh(w)

    expect(getState(w).submitting).toBe(false)
  })

  // --- Cancel/overlay click ---

  it('cancel button emits close', async () => {
    vi.useFakeTimers()
    const w = mountDialog()!
    await nextTick()

    const cancelBtn = $('.fbtn')!
    cancelBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()

    expect(w.emitted('close')).toBeTruthy()
  })

  it('clicking overlay emits close after leave animation', async () => {
    vi.useFakeTimers()
    const w = mountDialog()!
    await nextTick()

    const overlay = $('.modal-overlay')!
    overlay.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()

    // ModalDialog runs a 250ms leave animation before emitting close
    expect(w.emitted('close')).toBeFalsy()
    vi.advanceTimersByTime(250)
    await nextTick()

    expect(w.emitted('close')).toBeTruthy()
  })

  it('does not emit close when overlay clicked while submitting', async () => {
    vi.mocked(apiPost).mockImplementation(() => new Promise(() => {}))
    const w = mountDialog()!
    fillValidFields(w)
    await refresh(w)

    // Start submit (will hang)
    const _ = getState(w).submit()
    await w.vm.$nextTick()

    const overlay = $('.modal-overlay')!
    overlay.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()

    // Leave animation starts (visible close attempt), but onModalClose sees
    // submitting=true and instantly re-opens instead of emitting close.
    expect(w.emitted('close')).toBeFalsy()
    expect(getState(w).submitting).toBe(true)

    // Clean up
    getState(w).submitting = false
  })
})
