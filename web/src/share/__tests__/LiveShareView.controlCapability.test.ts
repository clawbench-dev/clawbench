import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

/**
 * The "request control" button follows the PUBLISHER's capability, not this
 * page's platform.
 *
 * Injection happens on the host machine, so only the host knows whether it can
 * take the request (it needs the input injector AND a whole-screen picture).
 * The server carries that as `control-enabled`, and the viewer greys the button
 * on it. The defect this pins: the button was always clickable, so a viewer
 * pressed it and the host could only answer "this client cannot inject input".
 */

type Cbs = Record<string, (v?: unknown) => void>
let lastViewer: { opts?: { callbacks?: Cbs } } | null = null
/** Every frame the component pushed onto the control channel. */
let sent: Array<Record<string, unknown>> = []

vi.mock('@/utils/clawbenchNative', () => ({ getNative: () => undefined }))
vi.mock('@/share/shareMode', () => ({ getShareToken: () => 'tok', isShareMode: () => true }))

vi.mock('@/share/liveViewer', () => ({
  LiveViewer: class {
    opts: { callbacks?: Cbs }
    viewerId = 'v-test'
    constructor(opts: { callbacks?: Cbs }) {
      this.opts = opts
      lastViewer = this as never
    }
    connect(): void {}
    close(): void {}
    sendControl(msg: Record<string, unknown>): boolean {
      sent.push(msg)
      return true
    }
    /** The page calls this after sending the request; without it the click
     *  handler throws and the test only passes by accident. */
    requestControl(): void {}
    getStats() {
      return Promise.resolve({ rttMs: null, lossPct: null, bitrateKbps: null, jitterMs: null })
    }
  },
}))

import LiveShareView from '@/share/LiveShareView.vue'
import { passJoinGate } from './joinGateTestHelper'

const UNAVAILABLE = '主持人当前无法接受远程控制'
const HINT = '请求操作主持人的屏幕'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      share: {
        live: {
          title: '直播',
          chat: '聊天',
          chatTitle: '聊天',
          requestControl: '请求控制',
          requestControlHint: HINT,
          controlUnavailable: UNAVAILABLE,
          controlPending: '等待主持人批准…',
          controlCancel: '取消请求',
          controlling: '你正在控制对方的屏幕',
          controlRelease: '结束控制',
          statusConnecting: '连接中…',
          statusLive: '直播中',
          joinSubtitle: '主持人邀请你加入会议。',
          joinNick: '你的称谓',
          joinNickPlaceholder: '例如：小明',
          joinNickHint: '称谓会记住。',
          joinEnter: '加入会议',
          noPictureTitle: '暂未共享画面',
          noPictureDesc: '主持人可以随时打开屏幕共享',
        },
      },
    },
  },
})

let wrapper: ReturnType<typeof mount> | null = null

beforeEach(() => {
  lastViewer = null
  sent = []
  Object.defineProperty(HTMLMediaElement.prototype, 'play', {
    configurable: true,
    writable: true,
    value: vi.fn(async () => undefined),
  })
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  // @ts-expect-error -- jsdom has no real implementation to restore to.
  delete HTMLMediaElement.prototype.play
  document.body.innerHTML = ''
})

async function mountViewer() {
  const w = mount(LiveShareView, { global: { plugins: [i18n] } })
  passJoinGate(w)
  wrapper = w
  await flushPromises()
  return w
}

describe('request-control button follows the publisher capability', () => {
  it('is greyed until the publisher advertises the capability', async () => {
    const w = await mountViewer()
    const btn = w.find('[data-action="request-control"]')

    // The host shares a screen, so the button is there at all.
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeDefined()
    // A greyed button with no explanation is its own bug: the reason has to be
    // reachable from the control itself.
    expect(btn.attributes('title')).toBe(UNAVAILABLE)
  })

  it('turns usable when the publisher advertises it, and back off when it stops', async () => {
    const w = await mountViewer()
    lastViewer?.opts?.callbacks?.onControlEnabled?.(true)
    await flushPromises()

    const btn = w.find('[data-action="request-control"]')
    expect(btn.attributes('disabled')).toBeUndefined()
    expect(btn.attributes('title')).toBe(HINT)

    // The host swapped to a window capture (or stopped sharing): the pointer
    // could no longer be mapped onto a desktop.
    lastViewer?.opts?.callbacks?.onControlEnabled?.(false)
    await flushPromises()
    expect(w.find('[data-action="request-control"]').attributes('disabled')).toBeDefined()

    // And back on: the button must still work, not just look enabled. (Checked
    // last because pressing it replaces it with the pending pill.)
    lastViewer?.opts?.callbacks?.onControlEnabled?.(true)
    await flushPromises()
    await w.find('[data-action="request-control"]').trigger('click')
    expect(sent).toContainEqual({ t: 'ctrl-req' })
  })
})
