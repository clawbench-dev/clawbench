import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ChatMessageRail from '../ChatMessageRail.vue'

/**
 * The rail is the conversation index painted as a numbered timeline. Its whole
 * point is that it reuses MessageIndexRow, so most of these assertions are
 * about which VARIANT is rendered where — if the node or the preview ever
 * stopped coming from that component, the rail could silently drift from the
 * index drawer it mirrors.
 */

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      chat: {
        btw: { anchorTitle: 'side question' },
        messageList: {
          conversationIndexTitle: 'Conversation index',
          conversationIndexRoleAssistant: 'assistant',
          conversationIndexRoleUser: 'user',
          conversationIndexRoleSystem: 'system',
          conversationIndexNoText: '(no text)',
          userMsgIndexAttachment: 'attachment',
          messageRailLabel: 'Message navigation',
          messageRailGenerating: 'Replying',
        },
      },
    },
  },
})

function rows(n: number) {
  return Array.from({ length: n }, (_, i) => ({
    id: i + 1,
    role: i % 2 === 0 ? 'user' : 'assistant',
    content: `message ${i + 1}`,
    summary: i % 2 === 0 ? undefined : `summary ${i + 1}`,
  }))
}

function mountRail(props: Record<string, unknown> = {}) {
  return mount(ChatMessageRail, {
    props: { messages: rows(5), ...props },
    global: { plugins: [i18n] },
    attachTo: document.body,
  })
}

/**
 * Stub the scroller the rail measures its gutter against. The rail lives in the
 * START gutter (left in LTR), so that is the padding the gate reads.
 */
function scrollerWithPadding(px: number) {
  const el = document.createElement('div')
  el.style.paddingInlineStart = `${px}px`
  document.body.appendChild(el)
  return el
}

/**
 * jsdom reports every rect as 0, so the rail has to be given a real track
 * height for the overflow maths to mean anything. `measure()` reads the
 * PARENT's box, so the component is mounted into a host whose rect is stubbed
 * — mounting straight onto <body> would leave the parent reporting 0.
 */
function mountWithTrack(count: number, props: Record<string, unknown> = {}, height = 300) {
  const w = mountRail({ messages: rows(count), ...props })
  // The component renders two roots (rail + card), so VTU wraps them; the
  // rail's ACTUAL parent is what `measure()` reads, and that wrapper is not
  // the mount target. Stub it where it lives.
  const host = w.find('.msg-rail').element.parentElement as HTMLElement
  Object.defineProperty(host, 'getBoundingClientRect', {
    configurable: true,
    value: () => ({ top: 0, bottom: height, left: 0, right: 100, width: 100, height, x: 0, y: 0, toJSON: () => ({}) }),
  })
  return w
}

beforeEach(() => {
  document.body.innerHTML = ''
})

describe('ChatMessageRail — renders the index as a numbered timeline', () => {
  it('paints one node per index row, numbered from 1', () => {
    const w = mountRail()
    const nodes = w.findAll('.msg-rail-node')
    expect(nodes).toHaveLength(5)
    expect(nodes.map(n => n.find('.msg-index').text())).toEqual(['1', '2', '3', '4', '5'])
  })

  it('renders nodes through MessageIndexRow (node variant) rather than a copy', () => {
    const w = mountRail()
    const nodes = w.findAll('.msg-rail-node')
    // `msg-item--node` is set by MessageIndexRow's `variant="node"`; a local
    // reimplementation would not carry it.
    expect(nodes[0].classes()).toContain('msg-item--node')
    // The node variant must NOT render the preview body — that is the card's job.
    expect(nodes[0].find('.msg-body').exists()).toBe(false)
  })

  it('paints the hover card rows through MessageIndexRow (body variant)', () => {
    const w = mountRail()
    const cardRows = w.findAll('.msg-rail-row')
    expect(cardRows).toHaveLength(5)
    expect(cardRows[0].classes()).toContain('msg-item--body')
    // …and the body variant must not render a second node: the node column is
    // painted on top of the card, and two would double every number.
    expect(cardRows[0].find('.msg-node').exists()).toBe(false)
  })

  it('marks exactly the active message on both layers', () => {
    const w = mountRail({ activeId: 3 })
    expect(w.findAll('.msg-rail-node')[2].classes()).toContain('active')
    expect(w.findAll('.msg-rail-node')[1].classes()).not.toContain('active')
    expect(w.findAll('.msg-rail-row')[2].classes()).toContain('active')
  })

  it('renders nothing at all for an empty index', () => {
    const w = mountRail({ messages: [] })
    expect(w.find('.msg-rail').exists()).toBe(false)
    expect(w.find('.msg-rail-card').exists()).toBe(false)
  })
})

describe('ChatMessageRail — gutter gate', () => {
  it('stays hidden when the pane has no room beside the reading measure', async () => {
    const scroller = scrollerWithPadding(0)
    const w = mountRail({ getScroller: () => scroller })
    await w.vm.refresh()
    expect(w.find('.msg-rail').classes()).not.toContain('msg-rail--visible')
  })

  it('appears once the gutter is wide enough', async () => {
    // 100 CSS px of right padding is comfortably past the 60 design px floor at
    // the default scale (--ui-scale is unset in jsdom → designToCss is identity).
    const scroller = scrollerWithPadding(100)
    const w = mountRail({ getScroller: () => scroller })
    await w.vm.refresh()
    expect(w.find('.msg-rail').classes()).toContain('msg-rail--visible')
  })
})

describe('ChatMessageRail — interaction', () => {
  it('opens the card on hover and lights the row nearest the pointer', async () => {
    const w = mountRail()
    const rail = w.find('.msg-rail')
    expect(w.find('.msg-rail-card').classes()).not.toContain('msg-rail-card--show')

    // jsdom reports every rect as 0, so the first node's centre is the origin;
    // a pointer at y=0 resolves to slot 0.
    await rail.trigger('mousemove', { clientY: 0 })
    expect(w.find('.msg-rail-card').classes()).toContain('msg-rail-card--show')
    expect(w.findAll('.msg-rail-node')[0].classes()).toContain('msg-item--lit')
    expect(w.findAll('.msg-rail-row')[0].classes()).toContain('msg-item--lit')
  })

  it('emits select with the row payload when a node is clicked', async () => {
    const w = mountRail()
    await w.findAll('.msg-rail-node')[2].trigger('click')
    const emitted = w.emitted('select')
    expect(emitted).toHaveLength(1)
    expect((emitted![0][0] as { id: number }).id).toBe(3)
  })

  it('closes the card after a selection', async () => {
    const w = mountRail()
    await w.find('.msg-rail').trigger('mousemove', { clientY: 0 })
    await w.findAll('.msg-rail-node')[1].trigger('click')
    expect(w.find('.msg-rail-card').classes()).not.toContain('msg-rail-card--show')
  })
})

describe('ChatMessageRail — connector', () => {
  it('draws one line layer, not one per node', () => {
    const w = mountRail()
    // A per-node connector would be a child of each node, and a node carries a
    // transform (its own stacking context), so the line would paint OVER the
    // node's background instead of behind it.
    expect(w.findAll('.msg-rail-line')).toHaveLength(1)
    const nodes = w.findAll('.msg-rail-node')
    for (const n of nodes) expect(n.find('.msg-rail-line').exists()).toBe(false)
  })
})

describe('ChatMessageRail — inset from the pane', () => {
  /** Inline px value for a declaration, e.g. `top: 12.8px`. */
  function px(style: string, key: string) {
    const m = style.match(new RegExp(`${key}:\\s*(-?[\\d.]+)px`))
    expect(m, `${key} should be written inline (${style})`).not.toBeNull()
    return parseFloat(m![1])
  }

  it('insets the axis from the left and centres it vertically in the band', async () => {
    // Both a short list (card shorter than the track) and one that overflows.
    // The short one is the case that must CENTRE: four numbers hanging off the
    // top of a tall pane leave the rest of the axis empty, which reads as a
    // list that failed to load. Equal free space above and below is the whole
    // assertion — a flush-top implementation gives 0 above and half a pane below.
    const track = 300
    for (const count of [3, 40]) {
      const w = mountWithTrack(count, {}, track)
      await w.vm.refresh()
      const card = w.find('.msg-rail-card').attributes('style') || ''
      const top = px(card, 'top')
      const height = px(card, 'height')
      const inset = px(card, 'left')
      expect(top, `count=${count}`).toBeGreaterThanOrEqual(inset - 0.001)
      expect(top - inset, `count=${count}`).toBeCloseTo(track - (top + height), 5)

      // The axis is the card's box exactly: same top, same left, same height.
      const rail = w.find('.msg-rail').attributes('style') || ''
      expect(px(rail, 'top'), `count=${count}`).toBeCloseTo(top, 5)
      expect(px(rail, 'left'), `count=${count}`).toBeCloseTo(inset, 5)
      expect(px(rail, 'height'), `count=${count}`).toBeCloseTo(height, 5)
    }
  })

  it('centres a short index instead of pinning it to the top edge', async () => {
    // Two rows in a 300px track: the card is far shorter than the band, so the
    // centring term must actually move it — `>= inset` alone would pass for a
    // flush-top card.
    const w = mountWithTrack(2)
    await w.vm.refresh()
    const card = w.find('.msg-rail-card').attributes('style') || ''
    expect(px(card, 'top')).toBeGreaterThan(px(card, 'left') + 20)
  })

  it('leaves room for that inset at the foot of the card, not just at its top', async () => {
    // Reserving the inset only when POSITIONING would let the card's bottom edge
    // run past the band by exactly that amount and collide with the composer.
    // 290 is chosen so the two readings disagree: a band of 290 fits 7 rows
    // (card 288 + 12 inset = 300 > 290), a band of 278 fits 6 (250 + 12 = 262).
    // At 300 both come out at exactly 300 and the assertion cannot tell them
    // apart — a mutation survived that boundary.
    const track = 290
    const w = mountWithTrack(40, {}, track)
    await w.vm.refresh()
    const card = w.find('.msg-rail-card').attributes('style') || ''
    expect(px(card, 'top') + px(card, 'height')).toBeLessThanOrEqual(track)
  })
})

describe('ChatMessageRail — edge fades follow the actual overflow', () => {
  it('fades the bottom only while rows are still hidden below', async () => {
    const w = mountWithTrack(40)
    await w.vm.refresh()
    const rail = w.find('.msg-rail')
    // At the top: everything hidden is below, nothing above.
    expect(rail.classes()).toContain('msg-rail--more-below')
    expect(rail.classes()).not.toContain('msg-rail--more-above')
  })

  it('drops the bottom fade once scrolled to the end', async () => {
    const w = mountWithTrack(40)
    await w.vm.refresh()
    const railEl = w.find('.msg-rail')
    // Scroll far past the end: `setOffset` clamps to maxOffset, which is the
    // exact "the last node is now fully visible" position.
    railEl.element.dispatchEvent(new WheelEvent('wheel', { deltaY: 100000, bubbles: true, cancelable: true }))
    await w.vm.$nextTick()

    // The regression this guards: the fade used to be tied to `--scrollable`,
    // so the last node stayed dimmed even with nothing below it.
    expect(railEl.classes()).not.toContain('msg-rail--more-below')
    expect(railEl.classes()).toContain('msg-rail--more-above')
  })

  it('never fades an edge with no hidden rows', async () => {
    // Three rows fit comfortably in the 300px track → nothing hidden anywhere.
    const w = mountWithTrack(3)
    await w.vm.refresh()
    const rail = w.find('.msg-rail')
    expect(rail.classes()).not.toContain('msg-rail--more-above')
    expect(rail.classes()).not.toContain('msg-rail--more-below')
    // …and it is not scrollable at all, so no grab cursor either.
    expect(rail.classes()).not.toContain('msg-rail--scrollable')
  })
})

describe('ChatMessageRail — pending (in-flight reply) node', () => {
  it('adds no pending slot when nothing is being written', () => {
    const w = mountRail()
    expect(w.findAll('.msg-rail-node--pending')).toHaveLength(0)
    expect(w.findAll('.msg-rail-node')).toHaveLength(5)
  })

  it('appends one pending node while a reply is streaming', () => {
    const w = mountRail({ generating: true })
    // The index only lists finalized messages, so this slot is the ONLY thing
    // that tells the reader a turn is still running.
    expect(w.findAll('.msg-rail-node--pending')).toHaveLength(1)
    // …and it is an ordinary MessageIndexRow node, not a bespoke marker.
    expect(w.findAll('.msg-rail-node')).toHaveLength(6)
  })

  it('shows a pending row in the card too, so the axis stays row-aligned', () => {
    const w = mountRail({ generating: true })
    // A node with no row would be an orphan number; the pending slot must appear
    // on BOTH layers, exactly like a numbered one.
    expect(w.findAll('.msg-rail-row--pending')).toHaveLength(1)
    expect(w.findAll('.msg-rail-row')).toHaveLength(6)
  })

  it('shows the ordinal the reply will get, through the shared node', () => {
    const w = mountRail({ generating: true })
    const pending = w.find('.msg-rail-node--pending')
    expect(pending.find('.msg-index').text()).toBe('6')
    expect(pending.classes()).toContain('msg-item--node')
  })

  it('is clickable and asks the host to reveal the in-flight reply', async () => {
    const w = mountRail({ generating: true })
    // It has no index row, so it cannot go through `select` — a dedicated event
    // lets the host answer with "jump to the streaming bubble".
    await w.find('.msg-rail-node--pending').trigger('click')
    expect(w.emitted('select')).toBeUndefined()
    expect(w.emitted('select-pending')).toHaveLength(1)
  })

  it('lets the card row trigger the same jump', async () => {
    const w = mountRail({ generating: true })
    await w.find('.msg-rail-row--pending').trigger('click')
    expect(w.emitted('select-pending')).toHaveLength(1)
  })

  it('keeps the pending slot inside the axis while a reply streams', async () => {
    // The regression: while generating, the axis tracked the ACTIVE message —
    // the last FINALIZED one, whose row then sits on the bottom edge, putting
    // the pending slot exactly one row lower: outside the clip box, so it was
    // invisible AND unclickable (`elementFromPoint` at its centre hit the panel
    // behind it). With the reader on the tail, the slot must stay on screen.
    const w = mountWithTrack(40, { activeId: 39, generating: true })
    await w.vm.refresh()
    // 40 is the last message, i.e. the row right before the pending slot.
    await w.setProps({ activeId: 40 })
    await w.vm.$nextTick()

    const railHeight = parseFloat(
      (w.find('.msg-rail').attributes('style') || '').match(/height:\s*(-?[\d.]+)px/)![1],
    )
    const nodes = w.findAll('.msg-rail-node')
    const pending = nodes[nodes.length - 1]
    expect(pending.classes()).toContain('msg-rail-node--pending')
    const centre = parseFloat(
      (pending.attributes('style') || '').match(/top:\s*(-?[\d.]+)px/)![1],
    )
    // The node is centred on its slot, so half of it must still fit too.
    expect(centre + 11).toBeLessThanOrEqual(railHeight)
    expect(centre - 11).toBeGreaterThanOrEqual(0)
  })

  it('marks the pending slot as the active node while a reply streams', () => {
    // While streaming, the tail IS the position the axis is showing, so the mark
    // belongs on the slot. Leaving it unmarked made it the only node without the
    // highlight — it read as "not a real message" instead of "being written".
    const w = mountRail({ generating: true, activeId: 5 })
    expect(w.find('.msg-rail-node--pending').classes()).toContain('active')
    // …and the finished row it replaced gives the mark up: exactly one node ever
    // carries it, or the cue stops meaning anything.
    const marked = w.findAll('.msg-rail-node').filter(n => n.classes().includes('active'))
    expect(marked).toHaveLength(1)
    // Both layers agree, or the number is lit while its row is not.
    expect(w.find('.msg-rail-row--pending').classes()).toContain('active')
  })

  it('hands the mark back to the message once the turn ends', () => {
    const w = mountRail({ activeId: 5 })
    expect(w.findAll('.msg-rail-node')[4].classes()).toContain('active')
    expect(w.findAll('.msg-rail-node').filter(n => n.classes().includes('active'))).toHaveLength(1)
  })
})
