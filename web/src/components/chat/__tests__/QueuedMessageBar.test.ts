import { describe, expect, it, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import QueuedMessageBar from '../QueuedMessageBar.vue'
import { queuedMessages, addQueued, setActiveQueueSession, resetQueuesForTest } from '@/composables/useMessageQueue'
import enLocale from '@/i18n/locales/en'

// The panel reads the real store, so the store's reactivity contract is what is
// under test: the COLLAPSED header renders `messages.length`, and a frozen count
// was the reported bug ("add a second queued message, the collapsed panel still
// says one"). Only the queue composable is exercised — no backend.
vi.mock('@/utils/chatStreamUtils', async (io) => {
  const a: any = await io()
  return { ...a, isInFlightSend: () => false, untrackInFlightSend: () => {} }
})

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enLocale } })

// Bind `queuedMessages` the way ChatPanelContent does (`:messages="queuedMessages"`)
// — a LIVE binding, not a snapshot prop. Using a real binding is what makes this
// test able to catch the in-place-mutation bug: a frozen store reference then
// leaves the header at its old count.
const Host = defineComponent({
  render() {
    return h(QueuedMessageBar, { messages: queuedMessages.value, midTurnSupported: true, busy: '' })
  },
})

describe('QueuedMessageBar (collapsed count)', () => {
  beforeEach(() => resetQueuesForTest())

  it('the collapsed header count grows with each queued message', async () => {
    setActiveQueueSession('s1')
    addQueued('s1', { queueId: 'q1', text: 'one' })

    const wrapper = mount(Host, { global: { plugins: [i18n] } })
    await nextTick()
    expect(wrapper.text(), 'one queued').toContain('Queued · 1')

    // A second message must update the COLLAPSED header without expanding.
    addQueued('s1', { queueId: 'q2', text: 'two' })
    await nextTick()
    expect(wrapper.text(), 'the collapsed count must show two').toContain('Queued · 2')
    expect(wrapper.find('.queued-bar-list').exists(), 'the list stays collapsed').toBe(false)

    // And a third.
    addQueued('s1', { queueId: 'q3', text: 'three' })
    await nextTick()
    expect(wrapper.text(), 'and three').toContain('Queued · 3')
  })
})
