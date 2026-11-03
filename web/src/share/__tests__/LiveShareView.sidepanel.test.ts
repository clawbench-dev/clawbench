import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The viewer's side panel: 聊天 + 听记 in one full-height column.
 *
 * Two things are easy to regress and neither shows up in a screenshot diff:
 *
 *   1. The panel must SQUEEZE the picture, not cover it. That is a structural
 *      promise — the panel is a flex sibling of the picture column — so it is
 *      asserted on the DOM tree and on the stylesheet, not on pixels.
 *   2. The transcript poll is scoped to the 听记 tab, not to the panel: reading
 *      the chat must not hit the transcript endpoint every 3 seconds.
 */

let lastViewer: { opts?: { callbacks?: Record<string, (v?: unknown) => void> } } | null = null

/** Captured so the test can prove the poll starts/stops with the tab. */
const poller = vi.hoisted(() => ({ start: vi.fn(), stop: vi.fn() }))

/** The one-shot "does this meeting have notes?" read, for late joiners. */
const probe = vi.hoisted(() => ({ fetch: vi.fn() }))

vi.mock('@/share/liveViewer', () => ({
  LiveViewer: class {
    opts: { callbacks?: Record<string, (v?: unknown) => void> }
    constructor(opts: { callbacks?: Record<string, (v?: unknown) => void> }) {
      this.opts = opts
      lastViewer = this as never
    }
    connect(): void {}
    close(): void {}
    startTalking(): Promise<boolean> {
      return Promise.resolve(true)
    }
    stopTalking(): Promise<void> {
      return Promise.resolve()
    }
    viewerId = 'v-test'
    sendControl(): boolean {
      return true
    }
    getStats() {
      return Promise.resolve({ rttMs: null, lossPct: null, bitrateKbps: null, jitterMs: null })
    }
  },
}))
vi.mock('@/share/shareMode', () => ({ getShareToken: () => 'tok', isShareMode: () => true }))
vi.mock('@/share/liveNotesClient', () => ({
  ShareNotesPoller: class {
    start(): void {
      poller.start()
    }
    stop(): void {
      poller.stop()
    }
  },
  fetchNotesSnapshot: () => probe.fetch(),
}))

import LiveShareView from '@/share/LiveShareView.vue'
import SplitDivider from '@/components/common/SplitDivider.vue'
import { passJoinGate } from './joinGateTestHelper'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      common: { close: '关闭' },
      share: {
        live: {
          title: '直播',
          chat: '聊天',
          chatTitle: '聊天',
          chatEmpty: '还没有消息',
          chatPlaceholder: '说点什么…',
          chatSend: '发送',
          notes: '听记',
          notesTitle: 'AI 听记',
          notesWaiting: '等待转写…',
          notesWaitingDesc: '有人说话后，这里会实时出现转写。',
          notesOffTitle: '听记已关闭',
          notesOffDesc: '主持人关闭了听记，这场会议没有留下转写。',
          notesStoppedHint: '主持人已关闭听记，以下为已记录的内容。',
          statusConnecting: '连接中…',
          joinSubtitle: '主持人邀请你加入会议。',
          joinNick: '你的称谓',
          joinNickPlaceholder: '例如：小明',
          joinNickHint: '称谓会记住。',
          joinEnter: '加入会议',
          noPictureTitle: '暂未共享画面',
          noPictureDesc: '主持人可以随时打开屏幕共享',
          statusLive: '直播中',
          fullscreen: '全屏',
          exitFullscreen: '退出全屏',
          viewerName: '观众 {id}',
          participants: '成员',
          hostRole: '主持人',
          memberYou: '你',
          memberSharing: '共享屏幕',
          memberTalking: '说话中',
          memberMuted: '已静音',
          membersEmpty: '还没有其他人的信息。',
        },
      },
    },
  },
})

beforeEach(() => {
  lastViewer = null
  poller.start.mockClear()
  poller.stop.mockClear()
  probe.fetch.mockReset()
  probe.fetch.mockResolvedValue(null)
  Object.defineProperty(HTMLMediaElement.prototype, 'play', {
    configurable: true,
    writable: true,
    value: vi.fn(async () => undefined),
  })
})

afterEach(() => {
  // @ts-expect-error -- jsdom has no real implementation to restore to.
  delete HTMLMediaElement.prototype.play
})

function mountView() {
  const w = mount(LiveShareView, {
    global: {
      plugins: [i18n],
      // The avatar pulls a DiceBear chunk; the icon is decoration here and has
      // its own tests.
      stubs: { SpeakerAvatar: { props: ['name'], template: '<span class="sa" :data-name="name" />' } },
    },
  })
  passJoinGate(w)
  return w
}

function display(el: { element: Element }): string {
  return (el.element as HTMLElement).style.display
}

describe('LiveShareView side panel', () => {
  it('starts closed and opens on the chat tab from the dock', async () => {
    const w = mountView()
    await flushPromises()

    expect(display(w.find('.live-side'))).toBe('none')

    await w.find('[data-action="chat"]').trigger('click')
    await flushPromises()

    expect(display(w.find('.live-side'))).not.toBe('none')
    expect(w.find('[data-tab="chat"]').classes()).toContain('live-side-tab--on')
    expect(display(w.find('[data-panel="chat"]'))).not.toBe('none')

    w.unmount()
  })

  it('closes again when the same dock button is pressed', async () => {
    const w = mountView()
    await flushPromises()

    await w.find('[data-action="chat"]').trigger('click')
    await flushPromises()
    await w.find('[data-action="chat"]').trigger('click')
    await flushPromises()

    expect(display(w.find('.live-side'))).toBe('none')

    w.unmount()
  })

  it('sits beside the picture instead of on top of it', async () => {
    const w = mountView()
    await flushPromises()

    // The squeeze is structural: the panel and the picture column are siblings
    // inside the row shell, so no absolute positioning is involved. The column
    // wrapper is the shared layout's (LiveStageLayout), which the host's
    // member-screen view renders too.
    expect(w.find('.live-shell > .live-stage-col > .live-share-view > .live-stage').exists()).toBe(true)
    expect(w.find('.live-shell > .live-side').exists()).toBe(true)
    expect(w.find('.live-share-view .live-side').exists()).toBe(false)

    w.unmount()
  })

  it('offers the 听记 tab only when the publisher enabled it, and switches panes', async () => {
    const w = mountView()
    await flushPromises()

    expect(w.find('[data-tab="notes"]').exists()).toBe(false)

    lastViewer?.opts?.callbacks?.onNotesEnabled?.(true)
    await flushPromises()

    await w.find('[data-action="notes"]').trigger('click')
    await flushPromises()

    expect(w.find('[data-tab="notes"]').classes()).toContain('live-side-tab--on')
    expect(display(w.find('[data-panel="notes"]'))).not.toBe('none')
    // Only one pane is ever visible: the chat must not stay behind the notes.
    expect(display(w.find('[data-panel="chat"]'))).toBe('none')

    await w.find('[data-tab="chat"]').trigger('click')
    await flushPromises()

    expect(display(w.find('[data-panel="chat"]'))).not.toBe('none')
    expect(display(w.find('[data-panel="notes"]'))).toBe('none')

    w.unmount()
  })

  it('keeps the 听记 tab and its pane after the publisher turns notes off', async () => {
    const w = mountView()
    await flushPromises()

    lastViewer?.opts?.callbacks?.onNotesEnabled?.(true)
    await flushPromises()
    await w.find('[data-action="notes"]').trigger('click')
    await flushPromises()

    // 听记 off mid-meeting. The pane must NOT be yanked back to the chat: the
    // lines already transcribed are the whole reason the viewer opened it.
    lastViewer?.opts?.callbacks?.onNotesEnabled?.(false)
    await flushPromises()

    expect(w.find('[data-action="notes"]').exists()).toBe(true)
    expect(w.find('[data-tab="notes"]').classes()).toContain('live-side-tab--on')
    expect(display(w.find('[data-panel="notes"]'))).not.toBe('none')
    expect(display(w.find('[data-panel="chat"]'))).toBe('none')
    // The transcript cannot grow any more, so the poll stops even though the
    // pane — and everything already in it — stays.
    expect(poller.stop).toHaveBeenCalled()

    // ...and it stays reachable after switching to the chat and back.
    await w.find('[data-tab="chat"]').trigger('click')
    await flushPromises()
    await w.find('[data-tab="notes"]').trigger('click')
    await flushPromises()
    expect(display(w.find('[data-panel="notes"]'))).not.toBe('none')

    w.unmount()
  })

  it('resumes polling when 听记 is switched back on while the pane is open', async () => {
    const w = mountView()
    await flushPromises()

    lastViewer?.opts?.callbacks?.onNotesEnabled?.(true)
    await flushPromises()
    await w.find('[data-action="notes"]').trigger('click')
    await flushPromises()
    expect(poller.start).toHaveBeenCalledTimes(1)

    lastViewer?.opts?.callbacks?.onNotesEnabled?.(false)
    await flushPromises()
    expect(poller.stop).toHaveBeenCalledTimes(1)

    // Back on: the reader does not have to leave the tab and return to see new
    // lines, so the poll restarts right here.
    lastViewer?.opts?.callbacks?.onNotesEnabled?.(true)
    await flushPromises()
    expect(poller.start).toHaveBeenCalledTimes(2)

    w.unmount()
  })

  it('explains that notes are off instead of pretending to wait for a transcript', async () => {    const w = mountView()
    await flushPromises()

    lastViewer?.opts?.callbacks?.onNotesEnabled?.(true)
    await flushPromises()
    lastViewer?.opts?.callbacks?.onNotesEnabled?.(false)
    await flushPromises()
    await w.find('[data-action="notes"]').trigger('click')
    await flushPromises()

    const empty = w.find('[data-panel="notes"] [data-role="notes-empty"]')
    expect(empty.exists()).toBe(true)
    expect(empty.text()).toContain('听记已关闭')
    expect(empty.text()).not.toContain('等待转写')

    w.unmount()
  })

  it('offers 听记 to a late joiner when the meeting already recorded something', async () => {
    // Joined after the publisher switched 听记 off: the publisher's state says
    // nothing about what is already on the server, so one probe decides.
    probe.fetch.mockResolvedValue({
      segments: [
        { id: 1, channel: 'mic', speaker: '主持人', startedAt: 1000, durationMs: 2000, text: '开场' },
      ],
      files: [],
      minutes: { status: 'idle' },
    })

    const w = mountView()
    await flushPromises()
    lastViewer?.opts?.callbacks?.onNotesEnabled?.(false)
    await flushPromises()

    expect(probe.fetch).toHaveBeenCalledTimes(1)
    expect(w.find('[data-action="notes"]').exists()).toBe(true)
    expect(w.find('[data-tab="notes"]').exists()).toBe(true)

    w.unmount()
  })

  it('does not invent a 听记 tab for a meeting that recorded nothing', async () => {
    probe.fetch.mockResolvedValue({ segments: [], files: [], minutes: { status: 'idle' } })

    const w = mountView()
    await flushPromises()
    lastViewer?.opts?.callbacks?.onNotesEnabled?.(false)
    await flushPromises()

    expect(probe.fetch).toHaveBeenCalledTimes(1)
    expect(w.find('[data-action="notes"]').exists()).toBe(false)
    expect(w.find('[data-tab="notes"]').exists()).toBe(false)

    w.unmount()
  })

  it('shows the shared empty state in both panes', async () => {    const w = mountView()
    await flushPromises()
    await w.find('[data-action="chat"]').trigger('click')
    await flushPromises()

    const chatEmpty = w.find('[data-panel="chat"] [data-role="chat-empty"]')
    expect(chatEmpty.exists()).toBe(true)
    // `MeetingEmpty` — the same block the record's tabs draw.
    expect(chatEmpty.classes()).toContain('meeting-empty')
    expect(chatEmpty.text()).toContain('还没有消息')

    lastViewer?.opts?.callbacks?.onNotesEnabled?.(true)
    await flushPromises()
    await w.find('[data-tab="notes"]').trigger('click')
    await flushPromises()

    const notesEmpty = w.find('[data-panel="notes"] [data-role="notes-empty"]')
    expect(notesEmpty.exists()).toBe(true)
    expect(notesEmpty.classes()).toContain('meeting-empty')
    // The copy explains the LIVE situation ("waiting"), not the record's
    // "notes were off / nothing to transcribe": the tab only exists when the
    // publisher enabled 听记.
    expect(notesEmpty.text()).toContain('等待转写')

    w.unmount()
  })

  it('polls the transcript only while the 听记 tab is showing', async () => {
    const w = mountView()
    await flushPromises()
    lastViewer?.opts?.callbacks?.onNotesEnabled?.(true)
    await flushPromises()

    // Opening the panel on the chat tab costs nothing: the viewer is not
    // reading the transcript, so the endpoint must not be hit.
    await w.find('[data-action="chat"]').trigger('click')
    await flushPromises()
    expect(poller.start).not.toHaveBeenCalled()

    await w.find('[data-tab="notes"]').trigger('click')
    await flushPromises()
    expect(poller.start).toHaveBeenCalledTimes(1)

    // Back to chat: polling stops rather than running behind a hidden pane.
    await w.find('[data-tab="chat"]').trigger('click')
    await flushPromises()
    expect(poller.stop).toHaveBeenCalledTimes(1)

    // Closing the panel also stops it.
    await w.find('[data-tab="notes"]').trigger('click')
    await flushPromises()
    await w.find('[data-action="side-close"]').trigger('click')
    await flushPromises()
    expect(poller.stop).toHaveBeenCalledTimes(2)

    w.unmount()
  })

  it('counts unread only while the chat pane is not the one showing', async () => {
    const w = mountView()
    await flushPromises()
    const onControl = lastViewer?.opts?.callbacks?.onControl

    onControl?.({ t: 'chat', id: 'm1', from: 'host', name: '主持人', text: '大家好', at: 1000 })
    await flushPromises()
    expect(w.find('.live-dock-badge').text()).toBe('1')

    // Opening the panel on the chat clears it.
    await w.find('[data-action="chat"]').trigger('click')
    await flushPromises()
    expect(w.find('.live-dock-badge').exists()).toBe(false)

    // A message arriving while the panel shows 听记 is still unread.
    lastViewer?.opts?.callbacks?.onNotesEnabled?.(true)
    await flushPromises()
    await w.find('[data-tab="notes"]').trigger('click')
    await flushPromises()
    onControl?.({ t: 'chat', id: 'm2', from: 'host', name: '主持人', text: '再说一句', at: 2000 })
    await flushPromises()
    expect(w.find('.live-dock-badge').text()).toBe('1')

    await w.find('[data-tab="chat"]').trigger('click')
    await flushPromises()
    expect(w.find('.live-dock-badge').exists()).toBe(false)

    w.unmount()
  })

  it('keeps the panel mounted while closed, so an unsent draft survives', async () => {
    const w = mountView()
    await flushPromises()

    await w.find('[data-action="chat"]').trigger('click')
    await flushPromises()
    const input = w.find('[data-role="chat-input"]')
    ;(input.element as HTMLInputElement).value = '还没发出去'
    await input.trigger('input')

    await w.find('[data-action="chat"]').trigger('click')
    await flushPromises()
    await w.find('[data-action="chat"]').trigger('click')
    await flushPromises()

    expect((w.find('[data-role="chat-input"]').element as HTMLInputElement).value).toBe('还没发出去')

    w.unmount()
  })
})

/**
 * The divider itself is `components/common/SplitDivider.vue` — the same one the
 * app's split panes use, so its pointer handling is covered there. What is ours
 * is the arithmetic: the panel width comes from the pointer's distance to the
 * shell's right edge, and it must stay inside bounds that keep both panes usable.
 */
describe('LiveShareView side panel resize', () => {
  /** jsdom has no layout: give the shell a real box so the clamp has an extent. */
  function stubShell(w: ReturnType<typeof mountView>, width = 1280): void {
    const el = w.find('.live-shell').element as HTMLElement
    el.getBoundingClientRect = () =>
      ({ left: 0, top: 0, right: width, bottom: 800, width, height: 800, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect
  }

  async function drag(w: ReturnType<typeof mountView>, clientX: number): Promise<string> {
    w.findComponent(SplitDivider).vm.$emit('dragmove', clientX)
    await flushPromises()
    return (w.find('.live-side').element as HTMLElement).style.getPropertyValue('--live-side-w')
  }

  it('follows the pointer while the divider is dragged', async () => {
    const w = mountView()
    await flushPromises()
    await w.find('[data-action="chat"]').trigger('click')
    await flushPromises()
    stubShell(w)

    // 1280 - 900 = 380px wide.
    expect(await drag(w, 900)).toBe('380px')

    w.unmount()
  })

  it('stops at the bounds instead of collapsing a pane', async () => {
    const w = mountView()
    await flushPromises()
    await w.find('[data-action="chat"]').trigger('click')
    await flushPromises()
    stubShell(w)

    // Dragged to the far right: the chat keeps its minimum.
    expect(parseFloat(await drag(w, 1279))).toBeGreaterThanOrEqual(260)
    // Dragged to the far left: the picture keeps its minimum, and a very wide
    // window does not turn the chat into half the screen.
    expect(parseFloat(await drag(w, 0))).toBeLessThanOrEqual(560)

    w.unmount()
  })

  it('only offers the divider while the panel is open', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.find('.live-side-divider').exists()).toBe(false)

    await w.find('[data-action="chat"]').trigger('click')
    await flushPromises()
    expect(w.find('.live-side-divider').exists()).toBe(true)

    await w.find('[data-action="side-close"]').trigger('click')
    await flushPromises()
    expect(w.find('.live-side-divider').exists()).toBe(false)

    w.unmount()
  })
})

/**
 * jsdom has no layout engine, so the "squeeze, never cover" promise has to be
 * read off the stylesheet. An `absolute` side panel would pass every DOM
 * assertion above while covering the picture.
 */
describe('LiveShareView side panel layout', () => {
  // The shell, the panel and the divider live in the SHARED layout — the host's
  // view of a member's screen renders the same file, so the promise has to be
  // pinned there or the two ends can drift apart again.
  const FILE = 'src/share/LiveStageLayout.vue'

  function styleBlock(): string {
    const src = readWebFile(FILE)
    const m = src.match(/<style scoped>([\s\S]*)<\/style>/)
    expect(m).not.toBeNull()
    return (m as RegExpMatchArray)[1].replace(/\/\*[\s\S]*?\*\//g, '')
  }

  function rule(css: string, selector: string): string {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const m = css.match(new RegExp(`\\n${escaped}\\s*\\{([^}]*)\\}`))
    expect(m, `rule ${selector} not found`).not.toBeNull()
    return (m as RegExpMatchArray)[1]
  }

  it('lays the panel out as a flex sibling of the picture', () => {
    const css = styleBlock()
    expect(rule(css, '.live-shell')).toMatch(/display:\s*flex/)
    const side = rule(css, '.live-side')
    // The whole point: no absolute positioning, so the stage keeps its own box.
    expect(side).not.toMatch(/position:\s*absolute/)
    expect(side).not.toMatch(/(^|;)\s*inset:/)
    expect(side).toMatch(/(^|;)\s*flex:/)
    expect(side).toMatch(/(^|;)\s*width:/)
  })

  it('lets the picture column actually shrink', () => {
    // `min-width: auto` (the flex default) refuses to go below the content and
    // would push the panel off screen instead of shrinking the video.
    expect(rule(styleBlock(), '.live-stage-col')).toMatch(/(^|;)\s*min-width:\s*0/)
  })

  it('takes the dragged width through a variable, so the narrow layout still wins', () => {
    const side = rule(styleBlock(), '.live-side')
    // The drag writes `--live-side-w` inline. Setting `width` inline instead
    // would beat the narrow-screen `width: 100%` rule and leave the stacked
    // panel a fixed column on a phone.
    expect(side).toMatch(/(^|;)\s*width:\s*var\(--live-side-w/)
    // ...and a drag can never starve the picture.
    expect(side).toMatch(/(^|;)\s*max-width:/)
  })

  it('stacks on narrow screens instead of squeezing the picture to a sliver', () => {
    const css = styleBlock()
    const media = css.match(/@media \(max-width: 720px\)\s*\{([\s\S]*)\}\s*$/)
    expect(media).not.toBeNull()
    const block = (media as RegExpMatchArray)[1]
    expect(block).toMatch(/\.live-shell\s*\{[^}]*flex-direction:\s*column/)
    expect(block).toMatch(/\.live-side\s*\{[^}]*width:\s*100%/)
    // A horizontal divider has nothing to separate once the panes are stacked.
    expect(block).toMatch(/\.live-side-divider\s*\{[^}]*display:\s*none/)
  })
})

/**
 * The unread count used to be red. Red means "error / destructive" everywhere
 * else in this UI, so an ordinary chat line read as a warning — on the app's
 * meeting panel and on the viewer's share page alike. Both follow the theme.
 */
describe('meeting unread badge colour', () => {
  it.each([
    { file: 'src/share/LiveDockButton.vue', selector: '.live-dock-badge' },
    { file: 'src/components/live/MeetingStage.vue', selector: '.live-count' },
  ])('paints $selector with the accent in $file', ({ file, selector }) => {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const m = readWebFile(file).match(new RegExp(`\\n${escaped}\\s*\\{([^}]*)\\}`))
    expect(m, `rule ${selector} not found in ${file}`).not.toBeNull()
    const body = (m as RegExpMatchArray)[1]
    expect(body).toMatch(/background:\s*var\(--accent-color/)
    expect(body).not.toMatch(/--color-red/)
  })
})

/**
 * 成员 is the audience's view of the room: it is the one tab that answers "who
 * else is here", which the chat only answers indirectly (and only for people
 * who have spoken).
 */
describe('LiveShareView 成员 tab', () => {
  it('is offered even when the meeting has no 听记', async () => {
    const w = mountView()
    await flushPromises()

    await w.find('[data-action="chat"]').trigger('click')
    await flushPromises()

    expect(w.find('[data-tab="participants"]').exists()).toBe(true)
    expect(w.find('[data-tab="notes"]').exists()).toBe(false)

    w.unmount()
  })

  it('lists the host first, then the audience, with me marked', async () => {
    const w = mountView()
    await flushPromises()
    await w.find('[data-action="chat"]').trigger('click')
    await flushPromises()

    lastViewer?.opts?.callbacks?.onJoined?.('v2')
    lastViewer?.opts?.callbacks?.onRoster?.([
      { id: 'host', name: '泷泽', host: true },
      { id: 'v1', name: '小明' },
      { id: 'v2', name: '小红' },
    ])
    await flushPromises()

    await w.find('[data-tab="participants"]').trigger('click')
    await flushPromises()

    const rows = w.findAll('.live-member')
    expect(rows).toHaveLength(3)
    // The same avatar mapping the transcript and the chat use.
    expect(rows.map((r) => r.find('.sa').attributes('data-name'))).toEqual(['泷泽', '小明', '小红'])
    expect(rows[0].text()).toContain('主持人')
    // "You" is a position, not a name: duplicates make the name ambiguous.
    expect(rows[2].find('[data-role="member-you"]').exists()).toBe(true)
    expect(rows[1].find('[data-role="member-you"]').exists()).toBe(false)

    w.unmount()
  })

  it('shows who is speaking, who is muted and who is sharing', async () => {
    const w = mountView()
    await flushPromises()
    await w.find('[data-action="chat"]').trigger('click')
    await flushPromises()

    lastViewer?.opts?.callbacks?.onJoined?.('v2')
    lastViewer?.opts?.callbacks?.onScreenSharer?.('v1')
    lastViewer?.opts?.callbacks?.onRoster?.([
      { id: 'host', name: '泷泽', host: true },
      { id: 'v1', name: '小明', talking: true },
      { id: 'v2', name: '小红', muted: true },
    ])
    await flushPromises()
    await w.find('[data-tab="participants"]').trigger('click')
    await flushPromises()

    const rows = w.findAll('.live-member')
    expect(rows[1].find('[data-role="member-talking"]').exists()).toBe(true)
    expect(rows[1].find('[data-role="member-sharing"]').exists()).toBe(true)
    expect(rows[2].find('[data-role="member-muted"]').exists()).toBe(true)

    w.unmount()
  })

  it('says the list is empty instead of rendering nothing', async () => {
    const w = mountView()
    await flushPromises()
    await w.find('[data-action="chat"]').trigger('click')
    await flushPromises()
    await w.find('[data-tab="participants"]').trigger('click')
    await flushPromises()

    expect(w.find('[data-role="members-empty"]').exists()).toBe(true)

    w.unmount()
  })
})
