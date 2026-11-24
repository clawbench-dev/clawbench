import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import MeetingJoin from '../MeetingJoin.vue'
import { MEETING_NICK_KEY } from '@/share/meetingNickname'

/**
 * The publisher-side join page. 参会称呼 (meeting.attendee_name) is read from
 * /api/config and written back through the settings API. It is NOT the account
 * nickname agents use in a chat (user.nickname): the two are independent, so a
 * nickname configured for chat must not pre-fill this field. The value is
 * mirrored into the share page's localStorage key so the guest gate comes up
 * pre-filled on this device.
 */

const mocks = vi.hoisted(() => ({
  // A plain { value } object is enough: the component only reads it, and tests
  // set it before mounting (no post-mount reactivity is exercised).
  serverConfig: { value: {} as Record<string, unknown> },
  setServerValue: vi.fn(),
}))

vi.mock('lucide-vue-next', () => ({
  ArrowLeft: { render: () => null },
  MonitorUp: { render: () => null },
}))
vi.mock('@/utils/clawbenchNative', () => ({ getNative: () => null, isDesktopApp: () => false }))
vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: vi.fn() }) }))
vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    serverConfig: mocks.serverConfig,
    setServerValue: mocks.setServerValue,
  }),
}))

function mountJoin() {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(MeetingJoin, { global: { plugins: [i18n] } })
}

const openMock = vi.fn()

beforeEach(() => {
  localStorage.clear()
  openMock.mockReset()
  mocks.serverConfig.value = {}
  mocks.setServerValue.mockReset()
  mocks.setServerValue.mockResolvedValue({ needsRestart: false, changedColdFields: [], warnings: [] })
  vi.stubGlobal('open', openMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('MeetingJoin', () => {
  it('cannot join without a nickname and a link', async () => {
    const w = mountJoin()
    const submit = w.find('[data-action="meeting-join-submit"]')

    expect(submit.attributes('disabled')).toBeDefined()

    await w.find('[data-role="meeting-link"]').setValue('http://example.test/share/abc')
    // Link alone is not enough: the nickname is required.
    expect(submit.attributes('disabled')).toBeDefined()

    await w.find('[data-role="meeting-nick"]').setValue('小明')
    expect(submit.attributes('disabled')).toBeUndefined()
  })

  it('refuses a link that is not http(s)', async () => {
    const w = mountJoin()
    await w.find('[data-role="meeting-nick"]').setValue('小明')
    await w.find('[data-role="meeting-link"]').setValue('example.test/share/abc')
    await w.find('[data-action="meeting-join-submit"]').trigger('click')

    expect(openMock).not.toHaveBeenCalled()
    expect(w.find('.meeting-error').exists()).toBe(true)
  })

  it('pre-fills from 参会称呼', () => {
    mocks.serverConfig.value = { meeting: { attendee_name: '老板' } }
    const w = mountJoin()
    expect((w.find('[data-role="meeting-nick"]').element as HTMLInputElement).value).toBe('老板')
  })

  it('ignores the chat nickname: the two names are independent', () => {
    // The regression this exists for: the field used to read user.nickname, so
    // editing it silently renamed the user inside every agent conversation.
    mocks.serverConfig.value = { user: { nickname: '老板' }, meeting: { attendee_name: '' } }
    const w = mountJoin()
    expect((w.find('[data-role="meeting-nick"]').element as HTMLInputElement).value).toBe('')
    expect(w.find('[data-action="meeting-join-submit"]').attributes('disabled')).toBeDefined()
  })

  it('does not pre-fill when 参会称呼 is unset', () => {
    mocks.serverConfig.value = { meeting: { attendee_name: '' } }
    const w = mountJoin()
    expect((w.find('[data-role="meeting-nick"]').element as HTMLInputElement).value).toBe('')
    expect(w.find('[data-action="meeting-join-submit"]').attributes('disabled')).toBeDefined()
  })

  it('does not pre-fill the fallback literal', () => {
    // "User" is the fallback applied when the account never configured a name.
    // Pre-filling it would make every unconfigured viewer join as "User" and
    // become indistinguishable in the publisher's audience list.
    mocks.serverConfig.value = { meeting: { attendee_name: 'User' } }
    const w = mountJoin()
    expect((w.find('[data-role="meeting-nick"]').element as HTMLInputElement).value).toBe('')
    expect(w.find('[data-action="meeting-join-submit"]').attributes('disabled')).toBeDefined()
  })

  it('falls back to the remembered guest name when 参会称呼 is unset', () => {
    localStorage.setItem(MEETING_NICK_KEY, 'Alex')
    const w = mountJoin()
    expect((w.find('[data-role="meeting-nick"]').element as HTMLInputElement).value).toBe('Alex')
  })

  it('keeps an unchanged name out of the settings API and opens the link', async () => {
    mocks.serverConfig.value = { meeting: { attendee_name: '老板' } }
    const w = mountJoin()
    await w.find('[data-role="meeting-link"]').setValue('http://example.test/share/abc')
    await w.find('[data-action="meeting-join-submit"]').trigger('click')
    await flushPromises()

    expect(mocks.setServerValue).not.toHaveBeenCalled()
    expect(localStorage.getItem(MEETING_NICK_KEY)).toBe('老板')
    expect(openMock).toHaveBeenCalledWith('http://example.test/share/abc', '_blank', 'noopener')
  })

  it('writes an edited name back to meeting.attendee_name, not user.nickname', async () => {
    mocks.serverConfig.value = { user: { nickname: '老板' }, meeting: { attendee_name: '老板' } }
    const w = mountJoin()
    await w.find('[data-role="meeting-nick"]').setValue('小明')
    await w.find('[data-role="meeting-link"]').setValue('http://example.test/share/abc')
    await w.find('[data-action="meeting-join-submit"]').trigger('click')
    await flushPromises()

    expect(mocks.setServerValue).toHaveBeenCalledWith('meeting.attendee_name', '小明')
    expect(mocks.setServerValue).not.toHaveBeenCalledWith('user.nickname', expect.anything())
    expect(localStorage.getItem(MEETING_NICK_KEY)).toBe('小明')
    expect(openMock).toHaveBeenCalled()
  })

  it('does not open the link when the settings save fails', async () => {
    mocks.serverConfig.value = { meeting: { attendee_name: '老板' } }
    mocks.setServerValue.mockRejectedValue(new Error('invalid value'))

    const w = mountJoin()
    await w.find('[data-role="meeting-nick"]').setValue('Claude')
    await w.find('[data-role="meeting-link"]').setValue('http://example.test/share/abc')
    await w.find('[data-action="meeting-join-submit"]').trigger('click')
    await flushPromises()

    expect(openMock).not.toHaveBeenCalled()
    expect(w.find('.meeting-error').text()).toContain('invalid value')
  })
})
