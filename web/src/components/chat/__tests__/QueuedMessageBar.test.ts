import { describe, expect, it, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import QueuedMessageBar from '../QueuedMessageBar.vue'
import { queuedMessages, addQueued, removeQueued, setActiveQueueSession, resetQueuesForTest } from '@/composables/useMessageQueue'
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
    // Two entries, because a single collapsed row deliberately hides the count
    // (it would repeat the preview). See the count-visibility block below.
    addQueued('s1', { queueId: 'q1', text: 'one' })
    addQueued('s1', { queueId: 'q2', text: 'two' })

    const wrapper = mount(Host, { global: { plugins: [i18n] } })
    await nextTick()
    expect(wrapper.find('.queued-bar-count').text(), 'two queued').toBe('2')

    // A third message must update the COLLAPSED header without expanding.
    addQueued('s1', { queueId: 'q3', text: 'three' })
    await nextTick()
    expect(wrapper.find('.queued-bar-count').text(), 'the collapsed count must show three').toBe('3')
    expect(wrapper.find('.queued-bar-list').exists(), 'the list stays collapsed').toBe(false)
  })

  it('renders the count as a badge, separate from the title and preview', async () => {
    // The count must not be glued to the preview text ("排队中 2 看一下…" reads
    // as one sentence). It is its own pill, between the title and the preview.
    setActiveQueueSession('s1')
    addQueued('s1', { queueId: 'q1', text: '看一下这个文件' })
    addQueued('s1', { queueId: 'q2', text: '再看一下这个' })

    const wrapper = mount(Host, { global: { plugins: [i18n] } })
    await nextTick()
    const badge = wrapper.find('.queued-bar-count')
    expect(badge.exists(), 'the count must be its own element').toBe(true)
    expect(badge.classes(), 'shape comes from the shared pill class').toContain('count-badge')
    // Title and preview are separate elements, so the badge sits between them.
    const status = wrapper.find('.queued-bar-status')
    expect(status.exists()).toBe(true)
    expect(status.find('.queued-bar-title').text()).toBe('Queued')
    expect(status.find('.queued-bar-count').exists()).toBe(true)
    expect(wrapper.find('.queued-bar-preview').text()).toBe('看一下这个文件')
  })
})

/**
 * Count visibility in the COLLAPSED banner. With a single queued message the
 * number carries no information the preview does not already show
 * ("排队中 1 看一下这个文件"), so it is hidden; from two entries up it tells the
 * user how much is waiting. Expanded always shows it, because it then labels
 * the list below rather than the header row.
 */
describe('QueuedMessageBar (collapsed count visibility)', () => {
  beforeEach(() => resetQueuesForTest())

  it('hides the count when collapsed with a single message', async () => {
    setActiveQueueSession('s1')
    addQueued('s1', { queueId: 'q1', text: 'only one' })

    const wrapper = mount(Host, { global: { plugins: [i18n] } })
    await nextTick()
    expect(wrapper.find('.queued-bar-list').exists(), 'collapsed').toBe(false)
    expect(wrapper.find('.queued-bar-count').exists(), 'one message: the count is redundant').toBe(false)
    // The preview still identifies the message.
    expect(wrapper.find('.queued-bar-preview').text()).toBe('only one')
  })

  it('shows the count when collapsed with more than one message', async () => {
    setActiveQueueSession('s1')
    addQueued('s1', { queueId: 'q1', text: 'one' })
    addQueued('s1', { queueId: 'q2', text: 'two' })

    const wrapper = mount(Host, { global: { plugins: [i18n] } })
    await nextTick()
    expect(wrapper.find('.queued-bar-count').exists()).toBe(true)
    expect(wrapper.find('.queued-bar-count').text()).toBe('2')
  })

  it('reveals the count on expand even for a single message', async () => {
    // Expanded, the badge labels the list below, so it is informative again.
    setActiveQueueSession('s1')
    addQueued('s1', { queueId: 'q1', text: 'only one' })

    const wrapper = mount(Host, { global: { plugins: [i18n] } })
    await nextTick()
    expect(wrapper.find('.queued-bar-count').exists()).toBe(false)

    await wrapper.find('.queued-bar-header').trigger('click')
    expect(wrapper.find('.queued-bar-count').exists(), 'expanded shows the count').toBe(true)
    expect(wrapper.find('.queued-bar-count').text()).toBe('1')
  })

  it('starts collapsed again after the queue empties and refills', async () => {
    // The root v-if hides the card but does NOT unmount the component, so
    // `expanded` used to survive the empty gap: expand once, and every later
    // batch of queued messages appeared already expanded — the panel no longer
    // defaulted to collapsed. It must reset on the empty transition only, so an
    // in-progress queue keeps the user's choice.
    setActiveQueueSession('s1')
    addQueued('s1', { queueId: 'q1', text: 'one' })
    addQueued('s1', { queueId: 'q2', text: 'two' })

    const wrapper = mount(Host, { global: { plugins: [i18n] } })
    await nextTick()
    await wrapper.find('.queued-bar-header').trigger('click')
    expect(wrapper.find('.queued-bar-list').exists(), 'expanded by the click').toBe(true)

    // The queue drains completely: the card is hidden.
    removeQueued('s1', 'q1')
    removeQueued('s1', 'q2')
    await nextTick()
    expect(wrapper.find('.queued-bar').exists(), 'no messages, no card').toBe(false)

    // A new batch arrives and must show the COLLAPSED banner.
    addQueued('s1', { queueId: 'q3', text: 'three' })
    addQueued('s1', { queueId: 'q4', text: 'four' })
    await nextTick()
    expect(wrapper.find('.queued-bar-list').exists(), 'refilled panel must default to collapsed').toBe(false)
    expect(wrapper.find('.queued-bar-preview').text(), 'collapsed shows the next message').toBe('three')
  })

  it('keeps the user expansion while the queue is in progress', async () => {
    // The reset must fire on the EMPTY transition only — adding more messages
    // to a queue the user expanded must not snap it shut.
    setActiveQueueSession('s1')
    addQueued('s1', { queueId: 'q1', text: 'one' })
    addQueued('s1', { queueId: 'q2', text: 'two' })

    const wrapper = mount(Host, { global: { plugins: [i18n] } })
    await nextTick()
    await wrapper.find('.queued-bar-header').trigger('click')
    expect(wrapper.find('.queued-bar-list').exists()).toBe(true)

    addQueued('s1', { queueId: 'q3', text: 'three' })
    await nextTick()
    expect(wrapper.find('.queued-bar-list').exists(), 'a third message must not collapse it').toBe(true)
  })
})

/**
 * Collapsed-banner action button.
 *
 * The user asked for the insert/interrupt control to sit on the RIGHT of the
 * collapsed banner, so the queue can be acted on without expanding it first.
 * It acts on the HEAD entry (the next one out) — the same entry the preview
 * shows — and disappears when expanded, where every row carries its own button.
 *
 * Source contract for the geometry (jsdom has no CSS engine); behaviour is
 * asserted through a real mount.
 */
describe('QueuedMessageBar (collapsed banner action button)', () => {
  const src = readFileSync(resolve(__dirname, '../QueuedMessageBar.vue'), 'utf8')

  beforeEach(() => resetQueuesForTest())

  it('is a sibling of the toggle, not nested inside it', () => {
    // A <button> inside a <button> is invalid HTML, so the toggle must be
    // wrapped and the action placed alongside it.
    expect(src, 'the banner is a layout row').toMatch(/class="queued-bar-banner"/)
    const headerIdx = src.indexOf('class="queued-bar-header"')
    const actionIdx = src.indexOf('queued-bar-header-action')
    expect(headerIdx).toBeGreaterThan(-1)
    expect(actionIdx, 'the action must come after the toggle').toBeGreaterThan(headerIdx)
    // And it must be outside the toggle element: the toggle's closing tag is
    // the first `</button>` after the header class.
    const toggleClose = src.indexOf('</button>', headerIdx)
    expect(actionIdx, 'the action must not be nested in the toggle button').toBeGreaterThan(toggleClose)
  })

  it('is pinned to the right and cannot be squeezed by a long preview', () => {
    const m = src.match(/\.queued-bar-header-action\s*\{([^}]*)\}/)
    expect(m, '.queued-bar-header-action rule must exist').not.toBeNull()
    expect(m![1], 'must not shrink when the preview is long').toMatch(/flex-shrink:\s*0/)
    // The toggle takes the remaining space, so the action lands at the right.
    const h = src.match(/\.queued-bar-header\s*\{([^}]*)\}/)
    expect(h, '.queued-bar-header rule must exist').not.toBeNull()
    expect(h![1], 'the toggle must absorb the free space').toMatch(/flex:\s*1/)
  })

  it('is a standalone HALF-CAPSULE capping the card right endpoint', () => {
    // Shape is asymmetric on purpose: the LEFT edge is a straight vertical cut
    // (0), so the control reads as a SEGMENT attached to the banner rather than
    // a free-floating pill; the RIGHT end follows the card's INNER corner so
    // the outer edge closes the card cleanly.
    const m = src.match(/\.queued-bar-header-action\s*\{([^}]*)\}/)
    expect(m, '.queued-bar-header-action rule must exist').not.toBeNull()
    const rule = m![1]
    // Four-value radius: TL TR BR BL — left pair square, right pair card-inner.
    const radius = rule.match(/border-radius:\s*([^;]+);/)
    expect(radius, 'must declare an explicit border-radius').not.toBeNull()
    // Split on top-level whitespace only: a value like
    // `calc(var(--radius-lg) - 1px)` contains spaces of its own.
    const parts: string[] = []
    let depth = 0
    let cur = ''
    for (const ch of radius![1].trim()) {
      if (ch === '(') depth++
      if (ch === ')') depth--
      if (/\s/.test(ch) && depth === 0) {
        if (cur) { parts.push(cur); cur = '' }
        continue
      }
      cur += ch
    }
    if (cur) parts.push(cur)
    expect(parts.length, 'must be a 4-corner radius').toBe(4)
    expect(parts[0], 'the LEFT edge must be a straight cut (flat)').toBe('0')
    expect(parts[3], 'the LEFT edge must be a straight cut (flat)').toBe('0')
    expect(parts[1], 'the RIGHT end must match the card inner corner').toMatch(
      /calc\(var\(--radius-lg\)\s*-\s*1px\)/,
    )
    expect(parts[2], 'the RIGHT end must match the card inner corner').toMatch(
      /calc\(var\(--radius-lg\)\s*-\s*1px\)/,
    )
    // It must OVERRIDE the shared .queued-bar-action radius, which is declared
    // earlier in the file — a later rule of equal specificity wins.
    const baseIdx = src.indexOf('.queued-bar-action {')
    const capIdx = src.indexOf('.queued-bar-header-action')
    expect(baseIdx).toBeGreaterThan(-1)
    expect(capIdx, 'the override must come after the shared base rule').toBeGreaterThan(baseIdx)
  })

  it('occupies the endpoint: flush right and full banner height', () => {
    const m = src.match(/\.queued-bar-header-action\s*\{([^}]*)\}/)
    expect(m, '.queued-bar-header-action rule must exist').not.toBeNull()
    const rule = m![1]
    // No right margin — the cap IS the right edge, not an inset chip.
    expect(rule, 'must sit flush against the card edge').not.toMatch(/margin-right/)
    // Stretches to the banner height: it is an endpoint, not a centred chip.
    expect(rule, 'must span the banner height').toMatch(/align-self:\s*stretch/)
    // And it needs its own surface to read as a distinct control.
    expect(rule, 'must have its own background surface').toMatch(/background:\s*var\(--bg-tertiary\)/)
  })

  it('pins the collapsed preview text to the LEFT', () => {
    // The header is a <button>, which centres its text by default — that is
    // what made the summary look centred. The preview needs an explicit pin.
    const m = src.match(/\.queued-bar-preview\s*\{([^}]*)\}/)
    expect(m, '.queued-bar-preview rule must exist').not.toBeNull()
    expect(m![1], 'the preview must be left-aligned, not centred').toMatch(/text-align:\s*left/)
  })

  it('acts on the head entry and hides once expanded', async () => {
    // Mount the component directly: `Host` swallows the child's emits (it
    // renders the child without an onAction handler), so an emit assertion has
    // to observe the component itself.
    const messages = [
      { queueId: 'q1', text: 'first', files: [], createdAt: '' },
      { queueId: 'q2', text: 'second', files: [], createdAt: '' },
    ]
    const wrapper = mount(QueuedMessageBar, {
      props: { messages, midTurnSupported: true, busy: '' },
      global: { plugins: [i18n] },
    })

    const btn = wrapper.find('.queued-bar-header-action')
    expect(btn.exists(), 'the collapsed banner carries an action button').toBe(true)
    // Same label as the row button, on the head entry.
    expect(btn.text()).toContain('Insert')

    await btn.trigger('click')
    const emitted = wrapper.emitted('action')
    expect(emitted, 'clicking must emit the action').toBeTruthy()
    expect(emitted![0]).toEqual(['q1', 'insert'])

    // Expanding removes it (each row has its own button).
    await wrapper.find('.queued-bar-header').trigger('click')
    expect(wrapper.find('.queued-bar-header-action').exists(), 'no duplicate once expanded').toBe(false)
  })

  it('emits interrupt when the backend cannot join the running turn', async () => {
    // The LABEL stays constant, but the behaviour must still follow capability.
    const messages = [{ queueId: 'q1', text: 'first', files: [], createdAt: '' }]
    const wrapper = mount(QueuedMessageBar, {
      props: { messages, midTurnSupported: false, busy: '' },
      global: { plugins: [i18n] },
    })

    const btn = wrapper.find('.queued-bar-header-action')
    expect(btn.exists()).toBe(true)
    expect(btn.text(), 'the label does not change with capability').toContain('Insert')
    expect(btn.classes(), 'the interrupt styling is applied').toContain('queued-bar-action-interrupt')

    await btn.trigger('click')
    expect(wrapper.emitted('action')![0]).toEqual(['q1', 'interrupt'])
  })

  it('disables the collapsed action while that entry is busy', async () => {
    const messages = [{ queueId: 'q1', text: 'first', files: [], createdAt: '' }]
    const wrapper = mount(QueuedMessageBar, {
      props: { messages, midTurnSupported: true, busy: 'q1' },
      global: { plugins: [i18n] },
    })
    expect(wrapper.find('.queued-bar-header-action').attributes('disabled')).toBeDefined()
  })
})

/**
 * Collapsed-header preview: the next message to be sent is shown next to the
 * count, so the queue is readable without expanding. It must track the HEAD of
 * the queue (the next one out), disappear once expanded (the list already shows
 * every row), and degrade to the attachment label for an attachment-only entry.
 */
describe('QueuedMessageBar (collapsed next-message preview)', () => {
  beforeEach(() => resetQueuesForTest())

  it('shows the head of the queue while collapsed', async () => {
    setActiveQueueSession('s1')
    addQueued('s1', { queueId: 'q1', text: 'first message' })
    addQueued('s1', { queueId: 'q2', text: 'second message' })

    const wrapper = mount(Host, { global: { plugins: [i18n] } })
    await nextTick()
    const preview = wrapper.find('.queued-bar-preview')
    expect(preview.exists(), 'collapsed header must show a preview').toBe(true)
    expect(preview.text(), 'the NEXT message to be sent, not the last queued').toBe('first message')
  })

  it('advances to the new head when the first entry leaves the queue', async () => {
    setActiveQueueSession('s1')
    addQueued('s1', { queueId: 'q1', text: 'first message' })
    addQueued('s1', { queueId: 'q2', text: 'second message' })

    const wrapper = mount(Host, { global: { plugins: [i18n] } })
    await nextTick()
    expect(wrapper.find('.queued-bar-preview').text()).toBe('first message')

    removeQueued('s1', 'q1')
    await nextTick()
    expect(wrapper.find('.queued-bar-preview').text(), 'preview must follow the head').toBe('second message')
  })

  it('falls back to the attachment label when the next entry has no text', async () => {
    setActiveQueueSession('s1')
    addQueued('s1', { queueId: 'q1', text: '', files: [{ path: 'a.png', isDir: false }] })

    const wrapper = mount(Host, { global: { plugins: [i18n] } })
    await nextTick()
    expect(wrapper.find('.queued-bar-preview').text()).toBe('Attachment')
  })

  it('hides the preview when expanded (the list already shows every row)', async () => {
    setActiveQueueSession('s1')
    addQueued('s1', { queueId: 'q1', text: 'first message' })

    const wrapper = mount(Host, { global: { plugins: [i18n] } })
    await nextTick()
    expect(wrapper.find('.queued-bar-preview').exists()).toBe(true)

    await wrapper.find('.queued-bar-header').trigger('click')
    expect(wrapper.find('.queued-bar-list').exists(), 'expanded').toBe(true)
    expect(wrapper.find('.queued-bar-preview').exists(), 'no duplicate preview once expanded').toBe(false)
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

  it('shows the SAME label whether or not mid-turn insert is supported', () => {
    // The label is deliberately constant: the button always reads "插话",
    // independent of the backend's mid-turn capability. Only the behaviour
    // (insert vs interrupt) and its icon/tooltip stay capability-dependent.
    // Pinned at the source because jsdom cannot mount the real i18n store here.
    expect(src, 'the label must not branch on midTurnSupported').toMatch(
      /\{\{\s*t\('chat\.pending\.insert'\)\s*\}\}/,
    )
    expect(src, 'the label must not fall back to the interrupt text').not.toMatch(
      /midTurnSupported\s*\?\s*t\('chat\.pending\.insert'\)\s*:\s*t\('chat\.pending\.interrupt'\)/,
    )
  })

  it('still branches the action and tooltip on midTurnSupported', () => {
    // The label change must NOT flatten the actual behaviour: the emitted mode
    // and the hover hint remain capability-dependent.
    expect(src, 'the emitted action still branches').toMatch(
      /@click="\$emit\('action', msg\.queueId, midTurnSupported \? 'insert' : 'interrupt'\)"/,
    )
    expect(src, 'the tooltip still branches').toMatch(
      /:title="midTurnSupported \? t\('chat\.pending\.insertHint'\) : t\('chat\.pending\.interruptHint'\)"/,
    )
  })

  it('clips the collapsed preview to one line with an ellipsis, in a faint colour', () => {
    // jsdom has no CSS engine, so the preview's two defining properties are
    // pinned at the source: it must never wrap (a long message cannot grow the
    // collapsed header) and must be dimmer than the title.
    const m = src.match(/\.queued-bar-preview\s*\{([^}]*)\}/)
    expect(m, '.queued-bar-preview rule must exist').not.toBeNull()
    const rule = m![1]
    expect(rule, 'one line only').toMatch(/white-space:\s*nowrap/)
    expect(rule, 'overflowing text must be clipped').toMatch(/overflow:\s*hidden/)
    expect(rule, 'clipping must show an ellipsis').toMatch(/text-overflow:\s*ellipsis/)
    expect(rule, 'must be fainter than the title').toMatch(/color:\s*var\(--text-muted\)/)
  })

  it('the title no longer carries the count placeholder (the badge owns it)', () => {
    // The count moved out of the label and into a pill. Leaving `{count}` in the
    // locale would render it LITERALLY ("排队中 · {count}") because the template
    // now calls t() with no params — and the literalKeys guard only checks that a
    // key EXISTS, not that its placeholders are supplied.
    for (const file of ['zh', 'en']) {
      const locale = readFileSync(resolve(__dirname, `../../../i18n/locales/${file}.ts`), 'utf8')
      const m = locale.match(/^\s*barTitle:\s*'([^']*)',/m)
      expect(m, `${file}: chat.pending.barTitle must exist`).not.toBeNull()
      expect(m![1], `${file}: the count is rendered by the badge, not the label`).not.toContain('{count}')
    }
  })

  it('does not re-declare the badge geometry in the scoped rule', () => {
    // .count-badge owns shape; a scoped border-radius would outrank it and
    // square the pill off (see countBadge.css.test.ts).
    const m = src.match(/\.queued-bar-count\s*\{([^}]*)\}/)
    expect(m, '.queued-bar-count rule must exist').not.toBeNull()
    expect(m![1]).not.toMatch(/border-radius:/)
  })

  it('pins the chevron to the right edge in both collapsed and expanded states', () => {
    // The collapsed preview fills the row with flex:1, but it is absent once
    // expanded — so the chevron needs its own `margin-left: auto` or it drifts
    // left and the header stops matching the plan chip.
    const m = src.match(/\.queued-bar-chevron\s*\{([^}]*)\}/)
    expect(m, '.queued-bar-chevron rule must exist').not.toBeNull()
    expect(m![1]).toMatch(/margin-left:\s*auto/)
  })
})

/**
 * Merge footer. The action is queue-WIDE, so it lives in a footer row of the
 * expanded panel rather than on each row (which would read as per-row). It is
 * offered only when there are 2+ entries — merging one message into itself is
 * meaningless, so it is absent, not merely disabled.
 */
describe('QueuedMessageBar (merge footer)', () => {
  beforeEach(() => resetQueuesForTest())

  function mountBar(props: Record<string, unknown>) {
    return mount(QueuedMessageBar, {
      props: { messages: [], midTurnSupported: true, busy: '', ...props },
      global: { plugins: [i18n] },
    })
  }

  const two = [
    { queueId: 'q1', text: 'first', files: [], createdAt: '' },
    { queueId: 'q2', text: 'second', files: [], createdAt: '' },
  ]

  it('is absent while collapsed, even with multiple messages', () => {
    const wrapper = mountBar({ messages: two })
    expect(wrapper.find('.queued-bar-footer').exists()).toBe(false)
  })

  it('appears once expanded with more than one message', async () => {
    const wrapper = mountBar({ messages: two })
    await wrapper.find('.queued-bar-header').trigger('click')
    const footer = wrapper.find('.queued-bar-footer')
    expect(footer.exists()).toBe(true)
    expect(footer.find('button').text()).toContain('Merge')
  })

  it('is absent when expanded with only one message', async () => {
    // Nothing to merge into: absent rather than a permanently disabled control.
    const wrapper = mountBar({ messages: [two[0]] })
    await wrapper.find('.queued-bar-header').trigger('click')
    expect(wrapper.find('.queued-bar-list').exists(), 'expanded').toBe(true)
    expect(wrapper.find('.queued-bar-footer').exists()).toBe(false)
  })

  it('emits merge when clicked', async () => {
    const wrapper = mountBar({ messages: two })
    await wrapper.find('.queued-bar-header').trigger('click')
    await wrapper.find('.queued-bar-footer button').trigger('click')
    expect(wrapper.emitted('merge')).toHaveLength(1)
  })

  it('is disabled while the merge request is in flight', async () => {
    const wrapper = mountBar({ messages: two, mergeBusy: true })
    await wrapper.find('.queued-bar-header').trigger('click')
    expect(wrapper.find('.queued-bar-footer button').attributes('disabled')).toBeDefined()
  })

  it('keeps the merge control OUT of the per-row action cluster', async () => {
    // A per-row merge button would read as "merge this one row". The footer must
    // be a sibling of the list, not nested inside a row.
    const src = readFileSync(resolve(__dirname, '../QueuedMessageBar.vue'), 'utf8')
    const listClose = src.indexOf('</ul>')
    const footerIdx = src.indexOf('class="queued-bar-footer"')
    expect(listClose).toBeGreaterThan(-1)
    expect(footerIdx, 'the merge footer must come after the row list').toBeGreaterThan(listClose)
    // And it must not live inside the per-row actions cluster.
    const actionsIdx = src.indexOf('class="queued-bar-actions"')
    const actionsClose = src.indexOf('</div>', actionsIdx)
    expect(footerIdx).toBeGreaterThan(actionsClose)
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
