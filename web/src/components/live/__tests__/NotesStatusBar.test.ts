import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import NotesStatusBar from '../NotesStatusBar.vue'

/**
 * The transcript status bar is the only place the panel says WHY nothing new is
 * appearing. Before it existed, "nobody has spoken yet" and "the microphone
 * died" rendered as the same empty card — and only one of them needs the user
 * to do something.
 */

function mountBar(props: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(NotesStatusBar, {
    props: {
      health: 'ok',
      silentForMs: 0,
      pending: 0,
      lost: 0,
      retrying: false,
      segments: 0,
      ...props,
    },
    global: { plugins: [i18n] },
  })
}

const textOf = (w: ReturnType<typeof mountBar>) => w.find('[data-role="notes-status-text"]').text()

describe('NotesStatusBar', () => {
  it('says it is listening and how much has been transcribed', () => {
    const bar = mountBar({ segments: 24 })

    expect(textOf(bar)).toContain('24')
    expect(bar.attributes('data-health')).toBe('ok')
  })

  it('names a dead microphone and offers the reconnect action', async () => {
    const bar = mountBar({ health: 'micDead' })

    expect(textOf(bar)).toMatch(/microphone/i)
    const button = bar.find('[data-role="notes-retry-mic"]')
    expect(button.exists()).toBe(true)

    await button.trigger('click')
    expect(bar.emitted('retry')).toHaveLength(1)
  })

  it('says it is reconnecting instead of offering a button it is already using', () => {
    const bar = mountBar({ health: 'micDead', retrying: true })

    expect(textOf(bar)).toMatch(/reconnecting/i)
    // A button here would start a second attempt on top of the running one.
    expect(bar.find('[data-role="notes-retry-mic"]').exists()).toBe(false)
  })

  it('reports a backlog with the count', () => {
    const bar = mountBar({ health: 'queued', pending: 18 })

    expect(textOf(bar)).toContain('18')
    expect(bar.attributes('data-health')).toBe('queued')
  })

  it('states the silent period in whole minutes, never zero', () => {
    // The state only exists once the threshold has passed, so the count can
    // never read "0 minutes".
    expect(textOf(mountBar({ health: 'silent', silentForMs: 61_000 }))).toContain('for 1 minute')
    expect(textOf(mountBar({ health: 'silent', silentForMs: 120_000 }))).toContain('for 2 minute')
  })

  it('keeps the lost-segment count separate from the live state', () => {
    // A permanent hole is not a current condition: it must stay visible while
    // the meeting is otherwise fine.
    const bar = mountBar({ health: 'ok', segments: 10, lost: 3 })

    expect(textOf(bar)).toContain('10')
    expect(bar.find('[data-role="notes-status-lost"]').text()).toContain('3')
  })

  it('says nothing about losses when there are none', () => {
    expect(mountBar({ segments: 2 }).find('[data-role="notes-status-lost"]').exists()).toBe(false)
  })

  it('reports a lost server connection as its own state', () => {
    expect(textOf(mountBar({ health: 'reconnecting' }))).toMatch(/reconnecting/i)
    expect(textOf(mountBar({ health: 'offline' }))).toMatch(/meeting ended/i)
  })
})
