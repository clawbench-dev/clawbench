import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent, h } from 'vue'

// ── /btw anchor: real mount + click ──
// The source-guard tests in ChatMessageList.test.ts assert the markup exists,
// but a guard that only checks the TEXT of a call passes even when the called
// helper is undefined — which is exactly how a broken anchor shipped (the
// template called anchorKeyFor after the function was deleted, so every click
// threw and the drawer never opened). This test mounts the component and clicks
// the anchor, so an unresolvable identifier fails loudly.

vi.mock('lucide-vue-next', async (importOriginal) => {
  // Keep the real module's named exports (ESM named exports cannot be served by
  // a Proxy) and null out the render.
  const actual = await importOriginal<Record<string, unknown>>()
  const out: Record<string, unknown> = {}
  for (const key of Object.keys(actual)) {
    out[key] = defineComponent({ name: key, render: () => null })
  }
  return out
})

// The drawer composable touches tab state; stub it to a plain object so child
// components (and any that mount a drawer) can render.
vi.mock('@/composables/useTabDrawer', () => ({
  useTabDrawer: () => ({
    isOpen: { value: false },
    effectiveOpen: { value: false },
    open: vi.fn(),
    close: vi.fn(),
    toggle: vi.fn(),
  }),
}))

// Stub the heavy children; this test only cares about the anchor wiring.
vi.mock('../ChatMessageItem.vue', () => ({
  default: defineComponent({
    name: 'ChatMessageItem',
    props: ['msg'],
    setup(props) {
      return () => h('div', { class: 'cmi-stub' }, `msg-${props.msg?.id}`)
    },
  }),
}))
vi.mock('@/components/common/AgentIcon.vue', () => ({ default: { render: () => null } }))
// Renders an identifiable node: the pending anchor swaps its icon for this
// spinner, and the test asserts that swap happened.
vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: defineComponent({
    name: 'LoadingIndicator',
    props: { size: { type: String, default: 'md' } },
    // Mirror the real component's size-tier class, so a test can pin that the
    // spinner matches the icon's footprint (the real bug was a numeric `size`
    // producing a class that matched nothing and fell back to 28px).
    setup(props, { attrs }) {
      return () => h('i', { ...attrs, class: ['li-stub', `size-${props.size}`, (attrs as any).class] })
    },
  }),
}))
vi.mock('@/components/common/ProviderIcon.vue', () => ({ default: { render: () => null } }))
vi.mock('../UserMsgIndexDrawer.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/common/TableRowModal.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/file/CodeLinkPreview.vue', () => ({ default: { render: () => null } }))

vi.mock('@/composables/useCodeLinkPreview.ts', () => ({
  useCodeLinkPreview: () => ({ enabled: { value: false }, containerRef: null }),
  handleVerifiedFilePathClick: () => {},
}))
vi.mock('@/composables/useTextSelection.ts', () => ({ useTextSelectionActive: () => ({ value: false }) }))
vi.mock('@/composables/useFilePathAnnotation.ts', () => ({
  useFilePathAnnotation: () => ({ openFilePath: vi.fn(), readLineTargetFromEl: vi.fn() }),
}))
vi.mock('@/composables/useCodeBlockHeader.ts', () => ({
  handleCodeBlockClick: vi.fn(),
  handleTableBlockClick: vi.fn(),
  closeAllTableBlockMenus: vi.fn(),
}))
vi.mock('@/composables/useLocalhostAnnotation.ts', () => ({
  useLocalhostUrlClickHandler: () => ({ handleLocalhostUrlClick: vi.fn() }),
}))
vi.mock('@/composables/useDoubleClickCopy.ts', () => ({ useDoubleClickCopy: () => ({ handleDblClick: vi.fn() }) }))
vi.mock('@/composables/useDialog', () => ({ useDialog: () => ({ confirm: vi.fn() }) }))
// The returned object is read directly in ChatMessageList's render
// (`userMsgIndexDrawer.effectiveOpen.value`), so it must carry that ref.
vi.mock('@/composables/useUserMsgIndex.ts', () => ({
  useUserMsgIndex: () => ({
    drawer: { effectiveOpen: { value: false }, isOpen: { value: false }, open: vi.fn(), close: vi.fn() },
    effectiveOpen: { value: false },
    isOpen: { value: false },
    showUserMsgIndex: { value: false },
    open: vi.fn(),
    close: vi.fn(),
    indexOpen: { value: false },
    openIndex: vi.fn(),
    closeIndex: vi.fn(),
    jumpToUserMessage: vi.fn(),
    loadingTarget: { value: null },
    remainingCount: { value: 0 },
  }),
}))
vi.mock('@/composables/useTableRowExpand.ts', () => ({
  useTableRowExpand: () => ({
    tableRowModal: { value: null },
    closeTableRowModal: vi.fn(),
    tableRowPrev: vi.fn(),
    tableRowNext: vi.fn(),
    handleTableRowClick: vi.fn(),
    onTableMouseDown: vi.fn(),
    onTableTouchStart: vi.fn(),
  }),
}))
vi.mock('@/stores/app.ts', () => ({ store: { state: {} } }))
vi.mock('@/utils/appLog', () => ({ appLog: { w: vi.fn(), e: vi.fn(), i: vi.fn() } }))
vi.mock('@/utils/domFlash', () => ({ flashElement: vi.fn() }))

import ChatMessageList from '../ChatMessageList.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      chat: {
        btw: { anchorLabel: 'Asked by the way', anchorTitle: 'View the side question', answering: 'Answering…' },
        messageList: { loadingMore: 'Loading…', moreOlderMessages: 'More ({count})', allMessagesLoaded: 'All loaded' },
      },
    },
  },
})

function mountList(props = {}) {
  return mount(ChatMessageList, {
    props: {
      messages: [
        { id: 10, role: 'user', content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
        { id: 11, role: 'assistant', content: 'hello', blocks: [{ type: 'text', text: 'hello' }] },
      ],
      expandedTools: {},
      blockTasks: {},
      blockAskQuestions: {},
      agents: [],
      staticBlockCache: {},
      active: true,
      ...props,
    },
    global: { plugins: [i18n] },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('ChatMessageList — /btw anchor (mounted)', () => {
  it('renders no anchor when there are no records', () => {
    const wrapper = mountList({ btwAnchors: {} })
    expect(wrapper.findAll('.btw-anchor')).toHaveLength(0)
  })

  it('renders an anchor after the message it is anchored to', () => {
    const wrapper = mountList({ btwAnchors: { 10: [{ id: 1, anchorMessageId: 10 }] } })
    const anchors = wrapper.findAll('.btw-anchor')
    expect(anchors).toHaveLength(1)
    // It must follow the message row, not precede it.
    const html = wrapper.find('.chat-messages-list').html()
    expect(html.indexOf('msg-10')).toBeLessThan(html.indexOf('btw-anchor'))
  })

  it('clicking the anchor emits open-btw with the anchor key', async () => {
    const wrapper = mountList({ btwAnchors: { 10: [{ id: 1, anchorMessageId: 10 }] } })
    await wrapper.find('.btw-anchor').trigger('click')
    // This is the regression: with anchorKeyFor undefined the click threw and
    // nothing was emitted, so the drawer never opened.
    expect(wrapper.emitted('open-btw')).toBeTruthy()
    expect(wrapper.emitted('open-btw')![0]).toEqual(['10'])
  })

  it('shows the count badge when several questions share a position', () => {
    const wrapper = mountList({
      btwAnchors: { 10: [{ id: 1, anchorMessageId: 10 }, { id: 2, anchorMessageId: 10 }] },
    })
    expect(wrapper.find('.btw-anchor-count').text()).toBe('2')
  })

  it('omits the badge for a single question', () => {
    const wrapper = mountList({ btwAnchors: { 10: [{ id: 1, anchorMessageId: 10 }] } })
    expect(wrapper.find('.btw-anchor-count').exists()).toBe(false)
  })

  it('renders the pre-message anchor (0) at the top and clicks it', async () => {
    const wrapper = mountList({ btwAnchors: { 0: [{ id: 9, anchorMessageId: 0 }] } })
    const top = wrapper.find('.btw-anchor-top')
    expect(top.exists()).toBe(true)
    await top.trigger('click')
    expect(wrapper.emitted('open-btw')![0]).toEqual(['0'])
  })

  // ── Pending state ──
  // A question asked just now is inserted optimistically, so its anchor must
  // show an in-progress state immediately rather than waiting for the server.
  it('shows a spinner in place of the icon while a question is pending', () => {
    const wrapper = mountList({ btwAnchors: { 10: [{ id: 'btw-pending-1', anchorMessageId: 10, pending: true }] } })
    const anchor = wrapper.find('.btw-anchor')
    expect(anchor.exists()).toBe(true)
    expect(anchor.classes()).toContain('btw-anchor--pending')
    // The icon is REPLACED by the spinner, not shown alongside it.
    expect(anchor.find('.btw-anchor-spinner').exists()).toBe(true)
    expect(anchor.find('svg').exists()).toBe(false)
  })

  it('keeps the label and footprint identical while pending', () => {
    // The pending state must not resize or reword the pill: it only swaps the
    // 14px icon for a 14px spinner and solidifies the border. A different label
    // or a mis-sized spinner would make the anchor jump when a question starts.
    const resting = mountList({ btwAnchors: { 10: [{ id: 1, anchorMessageId: 10, answer: 'a' }] } })
    const pending = mountList({ btwAnchors: { 10: [{ id: 'btw-pending-1', anchorMessageId: 10, pending: true }] } })
    expect(pending.find('.btw-anchor-label').text())
      .toBe(resting.find('.btw-anchor-label').text())
    // The spinner must use the same size tier as the icon it replaces.
    const spinner = pending.find('.btw-anchor-spinner')
    expect(spinner.classes()).toContain('size-sm')
    expect(spinner.classes()).not.toContain('size-14')
  })

  it('marks the pending anchor busy and describes the wait out-of-band', () => {
    // The wait must be conveyed without changing the visible text, so it lives
    // in title/aria-label (which do not affect layout) plus aria-busy.
    const pending = mountList({ btwAnchors: { 10: [{ id: 'btw-pending-1', anchorMessageId: 10, pending: true }] } })
    const resting = mountList({ btwAnchors: { 10: [{ id: 1, anchorMessageId: 10, answer: 'a' }] } })
    const p = pending.find('.btw-anchor')
    const r = resting.find('.btw-anchor')
    expect(p.attributes('aria-busy')).toBe('true')
    expect(r.attributes('aria-busy')).toBe('false')
    expect(p.attributes('title')).toBe('Answering…')
    expect(p.attributes('aria-label')).toBe('Answering…')
    expect(r.attributes('title')).toBe('View the side question')
    // The visible label is NOT the pending text.
    expect(p.find('.btw-anchor-label').text()).not.toContain('Answering')
  })

  it('returns to the resting state once the answer is stored', () => {
    const wrapper = mountList({ btwAnchors: { 10: [{ id: 5, anchorMessageId: 10, question: 'q', answer: 'a' }] } })
    const anchor = wrapper.find('.btw-anchor')
    expect(anchor.classes()).not.toContain('btw-anchor--pending')
    expect(anchor.text()).toContain('Asked by the way')
    expect(anchor.find('.btw-anchor-spinner').exists()).toBe(false)
  })

  it('shows the pending state on the pre-message anchor too', () => {
    const wrapper = mountList({ btwAnchors: { 0: [{ id: 'btw-pending-2', anchorMessageId: 0, pending: true }] } })
    const anchor = wrapper.find('.btw-anchor-top')
    expect(anchor.exists()).toBe(true)
    expect(anchor.classes()).toContain('btw-anchor--pending')
  })

  it('stays pending while a settled question shares the anchor', () => {
    // Two questions at one position, the newer one still answering.
    const wrapper = mountList({
      btwAnchors: { 10: [
        { id: 1, anchorMessageId: 10, answer: 'a' },
        { id: 'btw-pending-3', anchorMessageId: 10, pending: true },
      ] },
    })
    const anchor = wrapper.find('.btw-anchor')
    expect(anchor.classes()).toContain('btw-anchor--pending')
    expect(anchor.find('.btw-anchor-count').text()).toBe('2')
  })

  it('does not anchor a message without a settled numeric id', () => {
    // A placeholder id cannot match a persisted row, so no anchor may render
    // even if the map happens to carry that key.
    const wrapper = mountList({
      messages: [{ id: 'pending-123', role: 'user', content: 'x', blocks: [] }],
      btwAnchors: { 'pending-123': [{ id: 1 }] },
    })
    expect(wrapper.findAll('.btw-anchor')).toHaveLength(0)
  })
})
