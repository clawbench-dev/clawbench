import { describe, expect, it, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
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

/**
 * Layout + label contract, checked against the SOURCE because jsdom has no CSS
 * engine and no layout. Both are deliberate and easy to undo by accident:
 *
 *   - The delete control is pinned to the RIGHT edge. The actions row is a flex
 *     row whose first child is the action button; without `margin-left: auto`
 *     the × sits immediately after it, so the row reads as one cluster instead
 *     of "action left, destructive control right".
 *   - The insert action is a SHORT two-character label ("插话"). The old
 *     "插入当前回复" was a sentence crammed into a pill button.
 */
describe('QueuedMessageBar layout + labels (source contract)', () => {
  const src = readFileSync(resolve(__dirname, '../QueuedMessageBar.vue'), 'utf8')

  it('pins the remove (×) button to the right edge', () => {
    const m = src.match(/\.queued-bar-remove\s*\{([^}]*)\}/)
    expect(m, '.queued-bar-remove rule must exist').not.toBeNull()
    expect(m![1], 'the × must be pushed right, away from the action button').toMatch(/margin-left:\s*auto/)
  })

  it('renders the remove button AFTER the action button in the flex row', () => {
    const actionIdx = src.indexOf('class="queued-bar-action"')
    const removeIdx = src.indexOf('class="queued-bar-remove"')
    expect(actionIdx).toBeGreaterThan(-1)
    expect(removeIdx).toBeGreaterThan(actionIdx)
  })

  it('uses a two-character insert label', () => {
    const zh = readFileSync(resolve(__dirname, '../../../i18n/locales/zh.ts'), 'utf8')
    const m = zh.match(/^\s*insert:\s*'([^']*)',/m)
    expect(m, 'chat.pending.insert must exist').not.toBeNull()
    expect(m![1], 'the insert label must stay short').toBe('插话')
    expect([...m![1]]).toHaveLength(2)
  })
})
