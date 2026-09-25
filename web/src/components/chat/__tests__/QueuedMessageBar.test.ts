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

/**
 * Type scale contract. The queue card is a CONTENT card, so its primary text
 * must use the same scale as the execution-plan chip (.plan-chip__text,
 * --font-size-sm). It previously used --font-size-2xs (10px) for everything —
 * the BADGE size — which made the card text too small to read comfortably.
 *
 * Source contract because jsdom has no CSS engine.
 */
describe('QueuedMessageBar type scale (source contract)', () => {
  const src = readFileSync(resolve(__dirname, '../QueuedMessageBar.vue'), 'utf8')
  const planSrc = readFileSync(resolve(__dirname, '../PlanPanel.vue'), 'utf8')

  function rule(selector: string): string {
    const m = src.match(new RegExp(selector.replace(/[.]/g, '\\.') + '\\s*\\{([^}]*)\\}'))
    expect(m, `${selector} rule must exist`).not.toBeNull()
    return m![1]
  }

  it('uses the plan panel primary-text size for the card text and header', () => {
    // Assert the plan panel actually uses sm, so this test fails loudly if the
    // reference panel is ever restyled — rather than silently pinning a stale
    // expectation.
    expect(planSrc, 'reference: plan chip text is sm').toMatch(
      /\.plan-chip__text\s*\{[^}]*font-size:\s*var\(--font-size-sm\)/,
    )
    expect(rule('.queued-bar-text'), 'card text must match the plan panel').toMatch(/font-size:\s*var\(--font-size-sm\)/)
    expect(rule('.queued-bar-header'), 'header must match the plan panel').toMatch(/font-size:\s*var\(--font-size-sm\)/)
  })

  it('no longer uses the badge size (2xs) for any card text', () => {
    // 2xs is reserved for badges/corner marks. Any text a user must READ
    // (message body, header title, file chips, action label) must be larger.
    for (const sel of ['.queued-bar-text', '.queued-bar-header', '.queued-bar-file', '.queued-bar-action']) {
      expect(rule(sel), `${sel} must not use the 10px badge size`).not.toMatch(/font-size:\s*var\(--font-size-2xs\)/)
    }
  })

  it('keeps meta (file chips, action label) one step below primary text', () => {
    expect(rule('.queued-bar-file')).toMatch(/font-size:\s*var\(--font-size-xs\)/)
    expect(rule('.queued-bar-action')).toMatch(/font-size:\s*var\(--font-size-xs\)/)
  })
})

/**
 * Density + coexistence contract with the execution-plan card.
 *
 * Two cards stack directly above the input (PlanPanel, then QueuedMessageBar).
 * When both are visible they must read as ONE column, so the queue card adopts
 * the plan card's geometry: the same horizontal inset and bottom rhythm as
 * .plan-panel, and the same radius as its collapsed chip (.plan-chip). It also
 * must not grow without bound, or expanding both would squeeze the message area.
 *
 * Source contract because jsdom has no CSS engine.
 */
describe('QueuedMessageBar density + coexistence (source contract)', () => {
  const src = readFileSync(resolve(__dirname, '../QueuedMessageBar.vue'), 'utf8')
  const planSrc = readFileSync(resolve(__dirname, '../PlanPanel.vue'), 'utf8')

  function rule(selector: string): string {
    const m = src.match(new RegExp(selector.replace(/[.]/g, '\\.') + '\\s*\\{([^}]*)\\}'))
    expect(m, `${selector} rule must exist`).not.toBeNull()
    return m![1]
  }

  it('uses the same horizontal inset as the plan card', () => {
    expect(planSrc, 'reference: plan panel inset').toMatch(/\.plan-panel\s*\{[^}]*margin:\s*0\s+var\(--space-5\)/)
    expect(rule('.queued-bar'), 'queue card must share the plan card column').toMatch(
      /margin:\s*0\s+var\(--space-5\)\s+var\(--space-4\)/,
    )
  })

  it('matches the plan chip corner radius', () => {
    expect(planSrc, 'reference: plan chip radius').toMatch(/\.plan-chip\s*\{[^}]*border-radius:\s*var\(--radius-lg\)/)
    expect(rule('.queued-bar')).toMatch(/border-radius:\s*var\(--radius-lg\)/)
  })

  it('is no longer cramped: header uses the plan chip box, rows breathe', () => {
    // .plan-chip is 4px/10px with a 6px gap — the reference rhythm.
    expect(rule('.queued-bar-header')).toMatch(/padding:\s*var\(--space-2\)\s+var\(--space-5\)/)
    expect(rule('.queued-bar-header')).toMatch(/gap:\s*var\(--space-3\)/)
    expect(rule('.queued-bar-item'), 'rows must have room to breathe').toMatch(
      /padding:\s*var\(--space-2\)\s+var\(--space-3\)/,
    )
    // Row gap one step up from the old space-1 (2px).
    expect(rule('.queued-bar-list')).toMatch(/gap:\s*var\(--space-2\)/)
  })

  it('is height-bounded so it cannot crowd out the message area', () => {
    // The plan timeline is capped at 240px; the queue list must be bounded too,
    // otherwise expanding both would leave the conversation almost no room.
    expect(planSrc, 'reference: plan timeline cap').toMatch(/\.plan-expanded__timeline\s*\{[^}]*max-height:\s*240px/)
    expect(rule('.queued-bar-list'), 'the queue list must be capped').toMatch(/max-height:\s*min\(40vh,\s*240px\)/)
  })
})
