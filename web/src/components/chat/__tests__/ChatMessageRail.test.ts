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

describe('ChatMessageRail — edge fades follow the actual overflow', () => {
  /**
   * jsdom reports every rect as 0, so the rail has to be given a real track
   * height for the overflow maths to mean anything. `measure()` reads the
   * PARENT's box, so the component is mounted into a host whose rect is stubbed
   * — mounting straight onto <body> would leave the parent reporting 0.
   */
  function mountWithTrack(count: number) {
    const w = mountRail({ messages: rows(count) })
    // The component renders two roots (rail + card), so VTU wraps them; the
    // rail's ACTUAL parent is what `measure()` reads, and that wrapper is not
    // the mount target. Stub it where it lives.
    const host = w.find('.msg-rail').element.parentElement as HTMLElement
    Object.defineProperty(host, 'getBoundingClientRect', {
      configurable: true,
      value: () => ({ top: 0, bottom: 300, left: 0, right: 100, width: 100, height: 300, x: 0, y: 0, toJSON: () => ({}) }),
    })
    return w
  }

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
    expect(w.findAll('.msg-rail-pending')).toHaveLength(0)
    expect(w.findAll('.msg-rail-node')).toHaveLength(5)
  })

  it('appends one pending node while a reply is streaming', () => {
    const w = mountRail({ generating: true })
    // The index only lists finalized messages, so this slot is the ONLY thing
    // that tells the reader a turn is still running.
    expect(w.findAll('.msg-rail-pending')).toHaveLength(1)
    expect(w.findAll('.msg-rail-node')).toHaveLength(5)
  })

  it('shows a pending row in the card too, so the axis stays row-aligned', () => {
    const w = mountRail({ generating: true })
    // A node with no row would be an orphan number; the pending slot must appear
    // on BOTH layers, exactly like a numbered one.
    expect(w.findAll('.msg-rail-row--pending')).toHaveLength(1)
    expect(w.findAll('.msg-rail-row:not(.msg-rail-row--pending)')).toHaveLength(5)
  })

  it('carries no ordinal — the pending reply has no index row yet', () => {
    const w = mountRail({ generating: true })
    const pending = w.find('.msg-rail-pending')
    expect(pending.text()).toBe('')
    expect(pending.attributes('title')).toBe('Replying')
  })

  it('is clickable and asks the host to reveal the in-flight reply', async () => {
    const w = mountRail({ generating: true })
    // It has no index row, so it cannot go through `select` — a dedicated event
    // lets the host answer with "jump to the streaming bubble".
    await w.find('.msg-rail-pending').trigger('click')
    expect(w.emitted('select')).toBeUndefined()
    expect(w.emitted('select-pending')).toHaveLength(1)
  })

  it('lets the card row trigger the same jump', async () => {
    const w = mountRail({ generating: true })
    await w.find('.msg-rail-row--pending').trigger('click')
    expect(w.emitted('select-pending')).toHaveLength(1)
  })

  it('never marks the pending slot as the active message', () => {
    // activeId 5 is the last real row; the pending slot must not inherit it even
    // though it is the last item in the list.
    const w = mountRail({ generating: true, activeId: 5 })
    const nodes = w.findAll('.msg-rail-node')
    expect(nodes[4].classes()).toContain('active')
    expect(w.find('.msg-rail-pending').classes()).not.toContain('active')
  })
})
