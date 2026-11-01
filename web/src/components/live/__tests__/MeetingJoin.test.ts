import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import MeetingJoin from '../MeetingJoin.vue'
import { MEETING_NICK_KEY } from '@/share/meetingNickname'

/**
 * The publisher-side join page. It writes the SAME nickname key the share
 * page's gate reads, so opening the link does not ask for the name twice.
 */

vi.mock('lucide-vue-next', () => ({ ArrowLeft: { render: () => null } }))
vi.mock('@/utils/clawbenchNative', () => ({ getNative: () => null }))
vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: vi.fn() }) }))

function mountJoin() {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(MeetingJoin, { global: { plugins: [i18n] } })
}

const openMock = vi.fn()

beforeEach(() => {
  localStorage.clear()
  openMock.mockReset()
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

  it('remembers the nickname and opens the link', async () => {
    const w = mountJoin()
    await w.find('[data-role="meeting-nick"]').setValue('小明')
    await w.find('[data-role="meeting-link"]').setValue('http://example.test/share/abc')
    await w.find('[data-action="meeting-join-submit"]').trigger('click')

    expect(localStorage.getItem(MEETING_NICK_KEY)).toBe('小明')
    expect(openMock).toHaveBeenCalledWith('http://example.test/share/abc', '_blank', 'noopener')
  })

  it('pre-fills the nickname remembered by the share page', () => {
    localStorage.setItem(MEETING_NICK_KEY, 'Alex')
    const w = mountJoin()
    expect((w.find('[data-role="meeting-nick"]').element as HTMLInputElement).value).toBe('Alex')
  })
})
