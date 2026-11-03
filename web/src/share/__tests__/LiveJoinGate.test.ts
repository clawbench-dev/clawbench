import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import LiveJoinGate from '../LiveJoinGate.vue'
import { MEETING_NICK_KEY } from '../meetingNickname'

/**
 * The viewer's nickname gate. Its contract: no name, no connection — the gate
 * is what stops an anonymous viewer from taking an audience slot.
 */

function mountGate() {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(LiveJoinGate, { props: { title: 'Release review' }, global: { plugins: [i18n] } })
}

beforeEach(() => {
  localStorage.clear()
})

describe('LiveJoinGate', () => {
  it('cannot be passed without a nickname', async () => {
    const w = mountGate()
    expect(w.find('[data-action="join-meeting"]').attributes('disabled')).toBeDefined()

    await w.find('[data-action="join-meeting"]').trigger('click')
    expect(w.emitted('join')).toBeUndefined()
  })

  it('rejects a whitespace-only nickname', async () => {
    const w = mountGate()
    await w.find('[data-role="join-nick"]').setValue('   ')
    expect(w.find('[data-action="join-meeting"]').attributes('disabled')).toBeDefined()
  })

  it('emits the trimmed name and remembers it', async () => {
    const w = mountGate()
    await w.find('[data-role="join-nick"]').setValue('  Alex  ')
    await w.find('[data-action="join-meeting"]').trigger('click')

    expect(w.emitted('join')?.[0]).toEqual(['Alex'])
    expect(localStorage.getItem(MEETING_NICK_KEY)).toBe('Alex')
  })

  it('pre-fills the remembered name', () => {
    localStorage.setItem(MEETING_NICK_KEY, '小明')
    const w = mountGate()

    expect((w.find('[data-role="join-nick"]').element as HTMLInputElement).value).toBe('小明')
    expect(w.find('[data-action="join-meeting"]').attributes('disabled')).toBeUndefined()
  })

  it('does not pre-fill the fallback literal', () => {
    // "User" is a fallback applied when nobody configured a name, not an
    // identity. Pre-filling it would make every such viewer indistinguishable
    // in the publisher's audience list.
    localStorage.setItem(MEETING_NICK_KEY, 'User')
    const w = mountGate()

    expect((w.find('[data-role="join-nick"]').element as HTMLInputElement).value).toBe('')
    expect(w.find('[data-action="join-meeting"]').attributes('disabled')).toBeDefined()
  })

  it('shows the meeting title the share page resolved', () => {
    const w = mountGate()
    expect(w.text()).toContain('Release review')
  })
})
