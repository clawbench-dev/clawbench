import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

/**
 * Desktop-client mode of the share page.
 *
 * The same page runs in a browser and in the Electron shell. In the shell the
 * capture APIs exist even over plain HTTP (the app promotes its server origin),
 * and Electron refuses an undeclared source — so the page must run a picker
 * before asking for the stream.
 */

let lastViewer: { opts?: { callbacks?: Record<string, (v?: unknown) => void> } } | null = null
const startScreenShare = vi.fn(async () => true)

vi.mock('@/share/liveViewer', () => ({
  LiveViewer: class {
    opts: { callbacks?: Record<string, (v?: unknown) => void> }
    constructor(opts: { callbacks?: Record<string, (v?: unknown) => void> }) {
      this.opts = opts
      lastViewer = this as never
    }
    connect(): void {}
    close(): void {}
    viewerId = 'v1'
    startScreenShare = startScreenShare
    stopScreenShare = vi.fn(async () => undefined)
    sendControl = vi.fn(() => true)
    getStats = vi.fn(async () => ({ rttMs: null, lossPct: null, bitrateKbps: null, jitterMs: null }))
  },
}))

const nativeBridge = {
  isNativeApp: () => true,
  listCaptureSources: vi.fn(async () => JSON.stringify([{ id: 'screen:0:0', name: '屏幕 1', kind: 'screen' }])),
  setPendingCaptureSource: vi.fn(async () => undefined),
}
vi.mock('@/utils/clawbenchNative', () => ({ getNative: () => nativeBridge }))
vi.mock('@/share/shareMode', () => ({ getShareToken: () => 'tok' }))

import LiveShareView from '@/share/LiveShareView.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      common: { close: '关闭', cancel: '取消' },
      share: {
        live: {
          title: '直播',
          notes: '听记',
          notesTitle: 'AI 听记',
          notesWaiting: '等待转写…',
          chat: '聊天',
          chatTitle: '聊天',
          chatEmpty: '还没有消息',
          chatPlaceholder: '说点什么…',
          chatSend: '发送',
          viewerName: '观众 {id}',
          qualityGood: '流畅',
          qualityFair: '一般',
          qualityPoor: '较差',
          shareScreen: '共享屏幕',
          stopSharing: '停止共享',
          shareScreenHint: '共享屏幕',
          stopSharingHint: '停止共享',
          shareFailed: '失败',
          screenBusy: '已有成员在共享',
          screenDisabled: '未开启',
          screenNeedsHttps: '需要 HTTPS',
          pickSource: '选择要共享的内容',
          clientHint: '需要客户端',
          youAreSharing: '你在共享',
          viewingSharer: '成员 {id} 在共享',
          backToHost: '看主持人',
          viewShared: '看共享',
          requestControl: '请求控制',
          requestControlHint: '请求控制',
          controlPending: '等待批准',
          controlling: '控制中',
          controlRelease: '结束控制',
          controlCancel: '取消',
          statusLive: '直播中',
          statusConnecting: '连接中',
          statusNegotiating: '协商中',
          statusEnded: '已结束',
          statusFull: '已满',
          statusFailed: '失败',
          endedTitle: '已结束',
          endedDesc: '结束',
          fullTitle: '已满',
          fullDesc: '满',
          failedTitle: '失败',
          failedDesc: '失败',
          failedIceDesc: '失败',
          retry: '重试',
          enableSound: '开启声音',
          micToggle: '麦克风',
          micOn: '开',
          micOff: '关',
          micOnHint: '开',
          micOffHint: '关',
          fullscreen: '全屏',
          exitFullscreen: '退出',
        },
      },
    },
  },
})

beforeEach(() => {
  lastViewer = null
  startScreenShare.mockClear()
  nativeBridge.listCaptureSources.mockClear()
  nativeBridge.setPendingCaptureSource.mockClear()
  // jsdom has no capture API; the client shell does, and that is what makes the
  // button usable over plain HTTP.
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: { getDisplayMedia: vi.fn() },
  })
})

describe('share page in the desktop client', () => {
  it('offers screen sharing and picks a source before capturing', async () => {
    const w = mount(LiveShareView, { global: { plugins: [i18n] } })
    await flushPromises()

    lastViewer?.opts?.callbacks?.onScreenEnabled?.(true)
    await flushPromises()

    const btn = w.find('[data-action="screen-share"]')
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeUndefined()

    await btn.trigger('click')
    await flushPromises()

    // Electron refuses an undeclared source, so the picker must come first and
    // getDisplayMedia must not have been called yet.
    expect(w.find('[data-role="source-picker"]').exists()).toBe(true)
    expect(startScreenShare).not.toHaveBeenCalled()

    const item = w.findAll('.live-picker__item')[0]
    await item.trigger('click')
    await flushPromises()

    expect(nativeBridge.setPendingCaptureSource).toHaveBeenCalledWith('screen:0:0', false)
    expect(startScreenShare).toHaveBeenCalled()
    w.unmount()
  })

  it('does not nag a client user with the browser hint', async () => {
    const w = mount(LiveShareView, { global: { plugins: [i18n] } })
    await flushPromises()
    lastViewer?.opts?.callbacks?.onScreenEnabled?.(true)
    await flushPromises()

    expect(w.find('[data-role="client-hint"]').exists()).toBe(false)
    w.unmount()
  })
})
