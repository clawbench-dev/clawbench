import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent, h, nextTick, ref } from 'vue'

/**
 * Behaviour tests for the session-switch scroll memory (issue #576).
 *
 * jsdom has no layout: scrollHeight/clientHeight are 0 and setting scrollTop
 * does not move anything, so the geometry is faked explicitly on the scroll
 * container. That is the whole point of these tests — the decision is made from
 * real numbers, not from reading the source.
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

// A message row that keeps the real hooks the restore looks for: the class the
// selector matches and the data-msg-key the anchor comparison uses.
vi.mock('../ChatMessageItem.vue', () => ({
  default: defineComponent({
    name: 'ChatMessageItem',
    props: ['msg'],
    setup(props) {
      return () =>
        h(
          'div',
          {
            class: 'chat-message',
            'data-msg-key': props.msg?.id ? `db-${props.msg.id}` : null,
          },
          `msg-${props.msg?.id}`,
        )
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
  useCodeLinkPreview: () => ({
    enabled: { value: false },
    containerRef: null,
    close: vi.fn(),
  }),
  handleVerifiedFilePathClick: () => {},
}))
// Must be a REAL ref: the interaction composable destructures `active` and the
// template tests `!textSelecting`. A bare { value: false } object is truthy, so
// the scroll FAB would never render and the hint assertion below could not pass.
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
    userMsgIndexList: { value: [] },
    drawer: { effectiveOpen: { value: false }, isOpen: { value: false }, open: vi.fn(), close: vi.fn() },
    loadingTarget: { value: null },
    loadingIndex: { value: false },
    toggleUserMsgIndex: vi.fn(),
    closeUserMsgIndex: vi.fn(),
    jumpToUserMessage: vi.fn(),
    jumpToAdjacentMessage: vi.fn(),
    scrollToMessage: vi.fn(),
    hasBtw: { value: false },
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

// The running-session set is module-level global state; the save contract reads
// it to decide whether a session may be remembered at all (WARN-008). A REAL
// ref, not a plain object: the component reads `runningSessions.value.has(...)`,
// and a bare `{}` would throw instead of exercising the branch.
vi.mock('@/composables/useSessionIdentity', async () => {
  const { ref } = await import('vue')
  return { runningSessions: ref(new Set<string>()) }
})

import ChatMessageList from '../ChatMessageList.vue'
import en from '@/i18n/locales/en'
import {
  saveChatScrollPosition,
  hasChatScrollPosition,
  getChatScrollPosition,
  _resetChatScrollMemory,
} from '@/utils/chatScrollMemory'
import { runningSessions } from '@/composables/useSessionIdentity'
import { NEAR_BOTTOM_PX } from '@/utils/scrollState'

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

interface Geometry {
  scrollHeight: number
  clientHeight: number
  scrollTop: number
}

/**
 * Fake the scroll container's geometry. jsdom reports 0 for everything and
 * clamps nothing, so without this every distance calculation is 0 and the test
 * would pass no matter what the component does.
 */
function fakeGeometry(el: HTMLElement, init: Geometry) {
  const state = { ...init }
  Object.defineProperty(el, 'scrollHeight', {
    configurable: true,
    get: () => state.scrollHeight,
  })
  Object.defineProperty(el, 'clientHeight', {
    configurable: true,
    get: () => state.clientHeight,
  })
  Object.defineProperty(el, 'scrollTop', {
    configurable: true,
    get: () => state.scrollTop,
    set: (v: number) => { state.scrollTop = v },
  })
  Object.defineProperty(el, 'getBoundingClientRect', {
    configurable: true,
    value: () => ({
      top: 0, left: 0, right: 0, bottom: state.clientHeight,
      width: 0, height: state.clientHeight, x: 0, y: 0, toJSON: () => ({}),
    }),
  })
  return state
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

const msgs = (...ids: number[]) => ids.map((id) => ({ id, role: id % 2 ? 'user' : 'assistant', content: `m${id}` }))

/** Wait for the deferred restore (nextTick → rAF, possibly a retry chain). */
async function flushRestore(frames = 6) {
  for (let i = 0; i < frames; i++) {
    await new Promise((r) => requestAnimationFrame(() => r(null)))
    await nextTick()
  }
}

beforeEach(() => {
  _resetChatScrollMemory()
  runningSessions.value = new Set()
})

afterEach(() => {
  _resetChatScrollMemory()
  runningSessions.value = new Set()
})

describe('ChatMessageList — saving the position of the session being left', () => {
  it('saves the old session when it is away from the bottom', async () => {
    const wrapper = mountList({ messages: msgs(1) })
    fakeGeometry(containerOf(wrapper), { scrollHeight: 2000, clientHeight: 500, scrollTop: 300 })

    await wrapper.setProps({ currentSessionId: 's2' })

    const entry = getChatScrollPosition('s1')
    expect(entry).toBeDefined()
    expect(entry?.scrollTop).toBe(300)
    // max = 1500 → ratio describes the same place if the content reloads taller
    expect(entry?.ratio).toBeCloseTo(300 / 1500, 5)
    wrapper.unmount()
  })

  it('forgets the session when it is left at the bottom', async () => {
    // Pre-seed a stale memory: being at the bottom on the way out must CLEAR it,
    // otherwise returning would restore an old position instead of the newest
    // content the user was actually looking at.
    saveChatScrollPosition('s1', { scrollTop: 100, anchorKey: '', anchorOffset: 0, ratio: 0.1 })
    const wrapper = mountList({ messages: msgs(1) })
    fakeGeometry(containerOf(wrapper), {
      scrollHeight: 2000,
      clientHeight: 500,
      scrollTop: 2000 - 500 - (NEAR_BOTTOM_PX - 1),
    })

    await wrapper.setProps({ currentSessionId: 's2' })

    expect(hasChatScrollPosition('s1')).toBe(false)
    wrapper.unmount()
  })

  it('savePositionNow applies the same contract for an external teardown', async () => {
    const wrapper = mountList({ messages: msgs(1) })
    fakeGeometry(containerOf(wrapper), { scrollHeight: 2000, clientHeight: 500, scrollTop: 700 })

    ;(wrapper.vm as unknown as { savePositionNow: () => void }).savePositionNow()

    expect(getChatScrollPosition('s1')?.scrollTop).toBe(700)
    wrapper.unmount()
  })

  it('never remembers a running session (its output is the point)', async () => {
    // shouldRestoreChatScroll refuses to restore into a running session, so an
    // entry written for one could only ever be ignored — or worse, restored
    // after the turn finished, hiding the reply that just landed. The save side
    // must therefore apply the same rule instead of storing a dead position.
    runningSessions.value = new Set(['s1'])
    const wrapper = mountList({ messages: msgs(1) })
    fakeGeometry(containerOf(wrapper), { scrollHeight: 2000, clientHeight: 500, scrollTop: 300 })

    await wrapper.setProps({ currentSessionId: 's2' })

    expect(hasChatScrollPosition('s1')).toBe(false)
    wrapper.unmount()
  })

  it('drops a stale memory for a session that started running', async () => {
    // A position saved while the session was still can go stale if the session
    // is then run and the user leaves mid-turn: the reader is no longer where
    // they were, so the old entry must not survive.
    saveChatScrollPosition('s1', { scrollTop: 100, anchorKey: '', anchorOffset: 0, ratio: 0.1 })
    runningSessions.value = new Set(['s1'])
    const wrapper = mountList({ messages: msgs(1) })
    fakeGeometry(containerOf(wrapper), { scrollHeight: 2000, clientHeight: 500, scrollTop: 300 })

    await wrapper.setProps({ currentSessionId: 's2' })

    expect(hasChatScrollPosition('s1')).toBe(false)
    wrapper.unmount()
  })
})

describe('ChatMessageList — restoring a still session position', () => {
  it('restores the saved offset and keeps the follow latch (no yank to bottom)', async () => {
    saveChatScrollPosition('s2', { scrollTop: 300, anchorKey: '', anchorOffset: 0, ratio: 0.2 })
    const wrapper = mountList({ messages: msgs(1) })
    const el = containerOf(wrapper)
    const geo = fakeGeometry(el, { scrollHeight: 2000, clientHeight: 500, scrollTop: 2000 - 500 })

    await wrapper.setProps({ currentSessionId: 's2' })
    await wrapper.setProps({ messages: msgs(1, 2) })
    await flushRestore()

    expect(geo.scrollTop).toBe(300)
    // The latch must be SET after restoring into the middle: otherwise the next
    // streamed token / render flush pins the reader back to the bottom, which is
    // the exact bug this feature fixes.
    const vm = wrapper.vm as unknown as { shouldStayPinned: () => boolean }
    expect(vm.shouldStayPinned()).toBe(false)
    wrapper.unmount()
  })

  it('does not clamp when the restored history is still shorter than it was', async () => {
    // The user left at 1200 in a 2000px document; on return only part of the
    // history is loaded (800px tall), then the rest arrives. Clamping at the
    // first attempt would strand the reader at the top.
    saveChatScrollPosition('s2', { scrollTop: 1200, anchorKey: '', anchorOffset: 0, ratio: 0.6 })
    const wrapper = mountList({ messages: msgs(1) })
    const el = containerOf(wrapper)
    const geo = fakeGeometry(el, { scrollHeight: 800, clientHeight: 500, scrollTop: 0 })

    await wrapper.setProps({ currentSessionId: 's2' })
    await wrapper.setProps({ messages: msgs(1, 2) })
    // Let the first attempts run while the content is still short.
    await flushRestore(3)
    expect(geo.scrollTop).toBe(0)

    // Older history / images finish loading.
    geo.scrollHeight = 2000
    await flushRestore()

    expect(geo.scrollTop).toBe(1200)
    wrapper.unmount()
  })

  it('lands the anchor message back at its saved viewport offset', async () => {
    // anchorOffset is the anchor's distance from the container top when the
    // user left. Pixels alone would drift once any block above it re-renders.
    saveChatScrollPosition('s2', { scrollTop: 300, anchorKey: 'db-2', anchorOffset: 40, ratio: 0.2 })
    const wrapper = mountList({ messages: msgs(1) })
    const el = containerOf(wrapper)
    fakeGeometry(el, { scrollHeight: 2000, clientHeight: 500, scrollTop: 0 })

    await wrapper.setProps({ currentSessionId: 's2' })
    await wrapper.setProps({ messages: msgs(1, 2) })
    // The anchor row sits 100px below the container top in this layout.
    const anchorRow = wrapper.findAll('.chat-message')[1].element as HTMLElement
    Object.defineProperty(anchorRow, 'getBoundingClientRect', {
      configurable: true,
      value: () => ({ top: 100, left: 0, right: 0, bottom: 140, width: 0, height: 40, x: 0, y: 100, toJSON: () => ({}) }),
    })
    await flushRestore()

    // 300 (saved pixel) + (100 − (0 + 40)) = 360
    expect(el.scrollTop).toBe(360)
    wrapper.unmount()
  })

  it('wins over the load-time bottom pin (syncSessionState calls onScrollBottom(false))', async () => {
    // The real switch sequence is: db_load lands → messages watcher arms the
    // restore → syncSessionState calls onScrollBottom(forceScrollBottom=false),
    // which pins to the bottom synchronously in its nextTick. The restore must
    // still be the last write, or every session would land at the bottom with
    // the memory silently discarded.
    saveChatScrollPosition('s2', { scrollTop: 300, anchorKey: '', anchorOffset: 0, ratio: 0.2 })
    const wrapper = mountList({ messages: msgs(1) })
    const el = containerOf(wrapper)
    const geo = fakeGeometry(el, { scrollHeight: 2000, clientHeight: 500, scrollTop: 0 })

    await wrapper.setProps({ currentSessionId: 's2' })
    await wrapper.setProps({ messages: msgs(1, 2) })
    ;(wrapper.vm as unknown as { scrollToBottom: (f?: boolean) => void }).scrollToBottom(false)
    await flushRestore()

    expect(geo.scrollTop).toBe(300)
    wrapper.unmount()
  })

  it('re-shows the jump-to-bottom FAB when new messages arrive at a restored position', async () => {
    // The "new messages arrived while you were reading" hint is the existing
    // down FAB group — no separate banner. It must appear again when the list
    // grows under a restored (mid-history) viewport.
    saveChatScrollPosition('s2', { scrollTop: 300, anchorKey: '', anchorOffset: 0, ratio: 0.2 })
    const wrapper = mountList({ messages: msgs(1) })
    fakeGeometry(containerOf(wrapper), { scrollHeight: 2000, clientHeight: 500, scrollTop: 0 })

    await wrapper.setProps({ currentSessionId: 's2' })
    await wrapper.setProps({ messages: msgs(1, 2) })
    await flushRestore()
    expect(wrapper.find('.scroll-fab-bottom').exists()).toBe(true)

    // Dismiss it through the real path (click outside the FAB group), so the
    // assertion below cannot pass just because the restore left it showing.
    document.body.click()
    await nextTick()
    expect(wrapper.find('.scroll-fab-bottom').exists()).toBe(false)

    await wrapper.setProps({ messages: msgs(1, 2, 3) })

    expect(wrapper.find('.scroll-fab-bottom').exists()).toBe(true)
    expect(wrapper.find('.scroll-fab-dir [title="Scroll to bottom"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('does not restore into a session the user already left again', async () => {
    saveChatScrollPosition('s2', { scrollTop: 300, anchorKey: '', anchorOffset: 0, ratio: 0.2 })
    const wrapper = mountList({ messages: msgs(1) })
    const el = containerOf(wrapper)
    const geo = fakeGeometry(el, { scrollHeight: 2000, clientHeight: 500, scrollTop: 1500 })

    await wrapper.setProps({ currentSessionId: 's2' })
    await wrapper.setProps({ messages: msgs(1, 2) })
    // Switch away again before the deferred restore runs.
    await wrapper.setProps({ currentSessionId: 's3' })
    await flushRestore()

    // The restore must have been abandoned — the viewport still belongs to s3.
    expect(geo.scrollTop).toBe(1500)
    wrapper.unmount()
  })

  it('abandons the restore once the user takes the viewport back', async () => {
    // The retry chain runs for up to MAX_RESTORE_TRIES frames (~1s of layout
    // settling). If the user grabs the scrollbar during that window, the final
    // write must not drag them back to the saved position — their gesture is
    // the newer intent.
    saveChatScrollPosition('s2', { scrollTop: 1200, anchorKey: '', anchorOffset: 0, ratio: 0.6 })
    const wrapper = mountList({ messages: msgs(1) })
    const el = containerOf(wrapper)
    // maxScroll = 200, so the saved 1200 cannot land yet and the chain retries.
    const geo = fakeGeometry(el, { scrollHeight: 700, clientHeight: 500, scrollTop: 0 })

    await wrapper.setProps({ currentSessionId: 's2' })
    await wrapper.setProps({ messages: msgs(1, 2) })
    // Let the retry chain start while the content is still too short.
    await flushRestore(2)
    expect(geo.scrollTop).toBe(0)

    // The user starts dragging: mousedown on the list sets the user-input flag
    // (the same one shouldPin consults).
    await wrapper.get('.chat-messages').trigger('mousedown')
    // The content finishes loading and the user has moved the viewport.
    geo.scrollHeight = 2000
    geo.scrollTop = 900
    await flushRestore()

    expect(geo.scrollTop).toBe(900, 'the restore must not yank the user back')
    wrapper.unmount()
  })
})

describe('ChatMessageList — "new messages" hint is not fooled by prepends', () => {
  it('does not re-show the jump-to-bottom FAB when older history is prepended', async () => {
    // load-more prepends older messages to the HEAD, so the array length grows
    // with no new message. A length-based check fired the hint on every
    // load-more while the reader sat at a restored position.
    saveChatScrollPosition('s2', { scrollTop: 300, anchorKey: '', anchorOffset: 0, ratio: 0.2 })
    const wrapper = mountList({ messages: msgs(1) })
    fakeGeometry(containerOf(wrapper), { scrollHeight: 2000, clientHeight: 500, scrollTop: 0 })

    await wrapper.setProps({ currentSessionId: 's2' })
    await wrapper.setProps({ messages: msgs(1, 2) })
    await flushRestore()
    expect(wrapper.find('.scroll-fab-bottom').exists()).toBe(true)

    // Dismiss the hint through the real path so the assertion below cannot pass
    // just because the restore left it showing.
    document.body.click()
    await nextTick()
    expect(wrapper.find('.scroll-fab-bottom').exists()).toBe(false)

    // Older history arrives at the head: 3 messages instead of 2, same tail.
    await wrapper.setProps({ messages: msgs(0, 1, 2) })

    expect(wrapper.find('.scroll-fab-bottom').exists()).toBe(false)
    wrapper.unmount()
  })

  it('still re-shows the FAB when a message is genuinely appended', async () => {
    // The control case for the test above: the hint must survive for real new
    // messages, otherwise the fix would just disable the feature.
    saveChatScrollPosition('s2', { scrollTop: 300, anchorKey: '', anchorOffset: 0, ratio: 0.2 })
    const wrapper = mountList({ messages: msgs(1) })
    fakeGeometry(containerOf(wrapper), { scrollHeight: 2000, clientHeight: 500, scrollTop: 0 })

    await wrapper.setProps({ currentSessionId: 's2' })
    await wrapper.setProps({ messages: msgs(1, 2) })
    await flushRestore()
    document.body.click()
    await nextTick()
    expect(wrapper.find('.scroll-fab-bottom').exists()).toBe(false)

    await wrapper.setProps({ messages: msgs(1, 2, 3) })

    expect(wrapper.find('.scroll-fab-bottom').exists()).toBe(true)
    wrapper.unmount()
  })

  it('treats a numeric and a string id for the same message as the same tail', async () => {
    // Transient bubbles carry string ids and DB rows carry numbers. Without a
    // normalised comparison the id type flip alone would look like a new
    // message and pop the hint for nothing.
    saveChatScrollPosition('s2', { scrollTop: 300, anchorKey: '', anchorOffset: 0, ratio: 0.2 })
    const wrapper = mountList({ messages: msgs(1) })
    fakeGeometry(containerOf(wrapper), { scrollHeight: 2000, clientHeight: 500, scrollTop: 0 })

    await wrapper.setProps({ currentSessionId: 's2' })
    await wrapper.setProps({ messages: msgs(1, 2) })
    await flushRestore()
    document.body.click()
    await nextTick()
    expect(wrapper.find('.scroll-fab-bottom').exists()).toBe(false)

    await wrapper.setProps({
      messages: [
        { id: 1, role: 'user', content: 'm1' },
        { id: '2', role: 'assistant', content: 'm2' },
      ],
    })

    expect(wrapper.find('.scroll-fab-bottom').exists()).toBe(false)
    wrapper.unmount()
  })
})

describe('ChatMessageList — older history prefetches before the top edge', () => {
  /** Scroll to an offset and let the component observe it. */
  async function scrollTo(wrapper: ReturnType<typeof mountList>, geo: Geometry, top: number) {
    geo.scrollTop = top
    containerOf(wrapper).dispatchEvent(new Event('scroll'))
    await nextTick()
  }

  it('fires load-more while the top edge is still about a viewport away', async () => {
    const wrapper = mountList({ messages: msgs(1, 2, 3), hasMore: true, loadingMore: false })
    // maxScroll = 2500, so the reader sits mid-history, not at the bottom.
    const geo = fakeGeometry(containerOf(wrapper), {
      scrollHeight: 3000,
      clientHeight: 500,
      scrollTop: 2500,
    })
    // Band = max(400, clientHeight=500) = 500px.
    await scrollTo(wrapper, geo, 1200)
    expect(wrapper.emitted('load-more')).toBeUndefined()

    // Still 500px from the top — the reader has not reached the edge, and this
    // is exactly where the request must already be in flight.
    await scrollTo(wrapper, geo, 500)
    expect(wrapper.emitted('load-more')).toHaveLength(1)
    wrapper.unmount()
  })

  it('does not fire at all when there is no older history', async () => {
    const wrapper = mountList({ messages: msgs(1, 2, 3), hasMore: false })
    const geo = fakeGeometry(containerOf(wrapper), {
      scrollHeight: 3000,
      clientHeight: 500,
      scrollTop: 2500,
    })
    await scrollTo(wrapper, geo, 0)
    expect(wrapper.emitted('load-more')).toBeUndefined()
    wrapper.unmount()
  })

  it('stays armed until the load completes (no overlapping requests)', async () => {
    const wrapper = mountList({ messages: msgs(1, 2, 3), hasMore: true, loadingMore: false })
    const geo = fakeGeometry(containerOf(wrapper), {
      scrollHeight: 3000,
      clientHeight: 500,
      scrollTop: 2500,
    })
    await scrollTo(wrapper, geo, 400)
    await scrollTo(wrapper, geo, 200)
    await scrollTo(wrapper, geo, 0)
    expect(wrapper.emitted('load-more')).toHaveLength(1)

    // The page arrives: loadingMore true → false re-arms the guard.
    await wrapper.setProps({ loadingMore: true })
    await wrapper.setProps({ loadingMore: false })
    await scrollTo(wrapper, geo, 100)
    expect(wrapper.emitted('load-more')).toHaveLength(2)
    wrapper.unmount()
  })
})
