import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent, h, nextTick, ref } from 'vue'

/**
 * Behaviour tests for the "still writing below" pill.
 *
 * The pill exists because the direction FABs are transient: during a long reply
 * a reader parked in the history had no sign that content was still arriving
 * and no persistent way back. It shares the bottom dock with the FABs, so the
 * tests that matter are (a) it appears exactly when it should, (b) the FABs give
 * up their own "back to bottom" button while it is up, and (c) the hand-off at
 * the end of a turn is a 3s farewell rather than an instant disappearance.
 *
 * jsdom has no layout, so the scroll geometry is faked explicitly — otherwise
 * every distance reads 0 and each assertion would pass no matter what the
 * component does.
 */

vi.mock('lucide-vue-next', async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>()
  const out: Record<string, unknown> = {}
  for (const key of Object.keys(actual)) {
    out[key] = defineComponent({ name: key, render: () => null })
  }
  return out
})

vi.mock('@/composables/useTabDrawer', () => ({
  useTabDrawer: () => ({
    isOpen: { value: false },
    effectiveOpen: { value: false },
    open: vi.fn(),
    close: vi.fn(),
    toggle: vi.fn(),
  }),
}))

vi.mock('../ChatMessageItem.vue', () => ({
  default: defineComponent({
    name: 'ChatMessageItem',
    props: ['msg'],
    setup(props) {
      return () => h('div', { class: 'chat-message', 'data-msg-key': props.msg?.id ? `db-${props.msg.id}` : null }, `msg-${props.msg?.id}`)
    },
  }),
}))

vi.mock('@/components/common/AvatarStack.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/common/AgentIcon.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/common/LoadingIndicator.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/common/ProviderIcon.vue', () => ({ default: { render: () => null } }))
vi.mock('../UserMsgIndexDrawer.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/common/TableRowModal.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/file/CodeLinkPreview.vue', () => ({ default: { render: () => null } }))
vi.mock('@/composables/useCodeLinkPreview.ts', () => ({
  useCodeLinkPreview: () => ({ enabled: { value: false }, containerRef: null, close: vi.fn() }),
  handleVerifiedFilePathClick: () => {},
}))
// Must be a REAL ref: the pill and the FABs are both gated on `!textSelecting`.
vi.mock('@/composables/useTextSelection.ts', () => ({
  useTextSelectionActive: () => ({ active: ref(false) }),
}))
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
vi.mock('@/composables/useDoubleClickCopy.ts', () => ({
  useDoubleClickCopy: () => ({ handleDblClick: vi.fn() }),
}))
vi.mock('@/composables/useDialog', () => ({ useDialog: () => ({ confirm: vi.fn() }) }))
vi.mock('@/composables/useUserMsgIndex.ts', () => ({
  useUserMsgIndex: () => ({
    userMsgIndexList: ref([]),
    drawer: { effectiveOpen: { value: false }, isOpen: { value: false }, open: vi.fn(), close: vi.fn() },
    loadingTarget: { value: null },
    loadingIndex: { value: false },
    toggleUserMsgIndex: vi.fn(),
    closeUserMsgIndex: vi.fn(),
    jumpToUserMessage: vi.fn(),
    jumpToAdjacentMessage: vi.fn(),
    scrollToMessage: vi.fn(),
    ensureIndexLoaded: vi.fn(),
    refreshIndexIfStale: vi.fn(),
    hasBtw: () => false,
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
vi.mock('@/utils/appLog', () => ({ appLog: { w: vi.fn(), e: vi.fn(), i: vi.fn(), d: vi.fn() } }))
vi.mock('@/utils/domFlash', () => ({ flashElement: vi.fn() }))
// Real ref: the pill reads `runningSessions.value.has(sessionId)`.
vi.mock('@/composables/useSessionIdentity', async () => {
  const { ref } = await import('vue')
  return { runningSessions: ref(new Set<string>()) }
})

import ChatMessageList from '../ChatMessageList.vue'
import en from '@/i18n/locales/en'
import { runningSessions } from '@/composables/useSessionIdentity'
import { _resetChatScrollMemory } from '@/utils/chatScrollMemory'
import { STREAM_PILL_FAREWELL_MS } from '@/utils/scrollState'

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

interface Geometry {
  scrollHeight: number
  clientHeight: number
  scrollTop: number
}

/** Fake the scroll container's geometry + the smooth-scroll call jsdom lacks. */
function fakeGeometry(el: HTMLElement, init: Geometry) {
  const state = { ...init }
  Object.defineProperty(el, 'scrollHeight', { configurable: true, get: () => state.scrollHeight })
  Object.defineProperty(el, 'clientHeight', { configurable: true, get: () => state.clientHeight })
  Object.defineProperty(el, 'scrollTop', {
    configurable: true,
    get: () => state.scrollTop,
    set: (v: number) => { state.scrollTop = v },
  })
  const scrollTo = vi.fn((opts: { top: number }) => { state.scrollTop = opts.top })
  Object.defineProperty(el, 'scrollTo', { configurable: true, value: scrollTo })
  return { state, scrollTo }
}

function mountList(props: Record<string, unknown> = {}) {
  return mount(ChatMessageList, {
    props: {
      messages: [],
      expandedTools: {},
      blockTasks: {},
      blockAskQuestions: {},
      agents: [{ id: 'a1', name: 'A' }],
      staticBlockCache: {},
      active: true,
      currentSessionId: 's1',
      ...props,
    },
    global: { plugins: [i18n] },
  })
}

const containerOf = (wrapper: ReturnType<typeof mountList>) =>
  wrapper.get('.chat-messages').element as HTMLElement

/** A user message followed by a reply that is still being written. */
function streamingMessages(text = 'hello') {
  return [
    { id: 1, role: 'user', content: 'question' },
    { id: 2, role: 'assistant', streaming: true, blocks: [{ type: 'text', text }] },
  ]
}

/** Park the viewport far from the bottom (beyond NEAR_BOTTOM_PX). */
async function scrollAway(wrapper: ReturnType<typeof mountList>) {
  const el = containerOf(wrapper)
  fakeGeometry(el, { scrollHeight: 2000, clientHeight: 500, scrollTop: 300 })
  el.dispatchEvent(new Event('scroll'))
  await nextTick()
}

beforeEach(() => {
  _resetChatScrollMemory()
  runningSessions.value = new Set()
})

afterEach(() => {
  _resetChatScrollMemory()
  runningSessions.value = new Set()
  vi.useRealTimers()
})

describe('ChatMessageList — "still writing below" pill', () => {
  it('shows while the reply is streaming and the reader is away from the bottom', async () => {
    const wrapper = mountList({ messages: streamingMessages() })
    await scrollAway(wrapper)

    const pill = wrapper.find('.stream-pill')
    expect(pill.exists()).toBe(true)
    expect(pill.text()).toContain('AI is responding')
    // The grid placement rules read `has-pill` off the ROW to decide whether
    // the FABs sit beside the pill or alone, centred. Bound on the wrong
    // element it never matched and the two stacked into two rows.
    expect(wrapper.get('.dock-row').classes()).toContain('has-pill')
    wrapper.unmount()
  })

  it('stays hidden while the reader sits at the bottom', async () => {
    // Nothing to jump to; a pill there would sit on top of the newest line.
    const wrapper = mountList({ messages: streamingMessages() })
    const el = containerOf(wrapper)
    fakeGeometry(el, { scrollHeight: 2000, clientHeight: 500, scrollTop: 1500 })
    el.dispatchEvent(new Event('scroll'))
    await nextTick()

    expect(wrapper.find('.stream-pill').exists()).toBe(false)
    expect(wrapper.get('.dock-row').classes()).not.toContain('has-pill')
    wrapper.unmount()
  })

  it('stays hidden when the reader is away but nothing is streaming', async () => {
    const wrapper = mountList({
      messages: [
        { id: 1, role: 'user', content: 'question' },
        { id: 2, role: 'assistant', blocks: [{ type: 'text', text: 'done' }] },
      ],
    })
    await scrollAway(wrapper)

    expect(wrapper.find('.stream-pill').exists()).toBe(false)
    wrapper.unmount()
  })

  it('takes over the "back to bottom" action: the FAB gives that button up', async () => {
    const wrapper = mountList({ messages: streamingMessages() })
    const el = containerOf(wrapper)
    fakeGeometry(el, { scrollHeight: 2000, clientHeight: 500, scrollTop: 1200 })
    // A genuine wheel gesture — the FABs only react to real input, not to
    // programmatic scrollTop writes.
    el.dispatchEvent(new WheelEvent('wheel', { deltaY: 120 }))
    el.dispatchEvent(new Event('scroll'))
    await nextTick()

    expect(wrapper.find('.stream-pill').exists()).toBe(true)
    // The down group shows, but only "next message" — two buttons doing the
    // same thing would make the reader choose between identical actions.
    expect(wrapper.find('.scroll-fab-group').exists()).toBe(true)
    expect(wrapper.find('.scroll-fab-dir [title="Scroll to bottom"]').exists()).toBe(false)
    expect(wrapper.find('.scroll-fab-dir [title="Next message"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('hands over to the FABs after the 3s farewell when the turn ends', async () => {
    vi.useFakeTimers()
    const wrapper = mountList({ messages: streamingMessages() })
    await scrollAway(wrapper)
    expect(wrapper.find('.stream-pill').text()).toContain('AI is responding')

    // Turn ends while the reader is still up in the history.
    await wrapper.setProps({
      messages: [
        { id: 1, role: 'user', content: 'question' },
        { id: 2, role: 'assistant', blocks: [{ type: 'text', text: 'hello world' }] },
      ],
    })
    await nextTick()

    // Not gone on the spot — that reads as "the UI broke", not "the reply ended".
    const pill = wrapper.find('.stream-pill')
    expect(pill.exists()).toBe(true)
    expect(pill.text()).toContain('Jump to latest')

    vi.advanceTimersByTime(STREAM_PILL_FAREWELL_MS)
    await nextTick()
    expect(wrapper.find('.stream-pill').exists()).toBe(false)
    wrapper.unmount()
  })

  it('clicking the pill returns to the bottom and dismisses it immediately', async () => {
    const wrapper = mountList({ messages: streamingMessages() })
    const el = containerOf(wrapper)
    const { state, scrollTo } = await (async () => {
      const geo = fakeGeometry(el, { scrollHeight: 2000, clientHeight: 500, scrollTop: 300 })
      el.dispatchEvent(new Event('scroll'))
      await nextTick()
      return geo
    })()

    await wrapper.get('.stream-pill').trigger('click')

    expect(scrollTo).toHaveBeenCalledTimes(1)
    expect(scrollTo.mock.calls[0][0]).toMatchObject({ top: 2000, behavior: 'smooth' })
    expect(state.scrollTop).toBe(2000)

    // The smooth scroll emits scroll events in a real browser; the pill hides
    // once the geometry says "at the bottom" again.
    el.dispatchEvent(new Event('scroll'))
    await nextTick()
    expect(wrapper.find('.stream-pill').exists()).toBe(false)
    wrapper.unmount()
  })
})
