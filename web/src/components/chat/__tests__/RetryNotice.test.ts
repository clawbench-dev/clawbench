import { describe, expect, it, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import RetryNotice from '../RetryNotice.vue'
import { showRetryNotice, clearRetryNotice, useRetryNotice } from '@/composables/useRetryNotice'
import enLocale from '@/i18n/locales/en'

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enLocale } })

function mountNotice(sessionId = 's1', active = true) {
  return mount(RetryNotice, { props: { sessionId, active }, global: { plugins: [i18n] } })
}

describe('RetryNotice', () => {
  beforeEach(() => clearRetryNotice())

  it('shows the attempt number while the agent retries', async () => {
    const w = mountNotice()
    expect(w.find('.retry-notice').exists()).toBe(false)

    showRetryNotice('s1', { attempt: 3, message: 'Rate limit exceeded', httpStatus: 429 })
    await nextTick()

    expect(w.find('.retry-notice').exists()).toBe(true)
    expect(w.text()).toContain('attempt 3')
    // The provider's own wording goes in the tooltip, not the localized line.
    expect(w.find('.retry-notice').attributes('title')).toContain('429: Rate limit exceeded')
  })

  it('falls back to the attempt-less wording when the agent reports none', async () => {
    const w = mountNotice()
    showRetryNotice('s1', { message: 'ECONNREFUSED' })
    await nextTick()
    expect(w.text()).toContain('retrying automatically')
    expect(w.text()).not.toContain('attempt')
  })

  // A background session's retry must not intrude on the conversation the user
  // is reading.
  it('ignores a notice belonging to another session', async () => {
    const w = mountNotice('s1')
    showRetryNotice('s2', { attempt: 2 })
    await nextTick()
    expect(w.find('.retry-notice').exists()).toBe(false)
  })

  // A stale notice must not survive a turn that ended without its terminal event
  // reaching this client (WS blip, session switch).
  it('is hidden while no turn is in flight', async () => {
    const w = mountNotice('s1', false)
    showRetryNotice('s1', { attempt: 2 })
    await nextTick()
    expect(w.find('.retry-notice').exists()).toBe(false)
  })

  // The notice is transient by contract: the next content event clears it, so a
  // retry that recovered leaves nothing behind.
  it('disappears when the turn resumes', async () => {
    const w = mountNotice()
    showRetryNotice('s1', { attempt: 2 })
    await nextTick()
    expect(w.find('.retry-notice').exists()).toBe(true)

    clearRetryNotice('s1')
    await nextTick()
    expect(w.find('.retry-notice').exists()).toBe(false)
  })
})

describe('useRetryNotice store', () => {
  beforeEach(() => clearRetryNotice())

  it('a delayed clear from another session cannot wipe the current notice', () => {
    showRetryNotice('s1', { attempt: 1 })
    clearRetryNotice('s2')
    expect(useRetryNotice().notice.value?.sessionId).toBe('s1')
  })

  it('a newer notice replaces the previous one', () => {
    showRetryNotice('s1', { attempt: 1 })
    showRetryNotice('s1', { attempt: 2, message: 'still limited' })
    expect(useRetryNotice().notice.value).toMatchObject({ sessionId: 's1', attempt: 2, message: 'still limited' })
  })
})
