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
/** Every frame the component pushed onto the control channel. */
let sent: Array<Record<string, unknown>> = []
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
    sendControl = vi.fn((msg: Record<string, unknown>) => {
      sent.push(msg)
      return true
    })
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
// Avatars are decoration here; the real generator pulls a DiceBear chunk.
vi.mock('@/utils/lazyAvatar', () => ({
  generateAvatarSvgWithStyle: async (seed: string) => `<svg data-seed="${seed}"/>`,
}))

import LiveShareView from '@/share/LiveShareView.vue'
import { passJoinGate } from './joinGateTestHelper'

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
          notesWaitingDesc: '有人说话后，这里会实时出现转写。',
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
          joinSubtitle: '主持人邀请你加入会议。填一个称谓，发布端与其他观众会看到它。',
          joinNick: '你的称谓',
          joinNickPlaceholder: '例如：小明',
          joinNickHint: '称谓会记住，下次加入不用再填。',
          joinEnter: '加入会议',
          noPictureTitle: '暂未共享画面',
          noPictureDesc: '主持人可以随时打开屏幕共享或摄像头',
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
  sent = []
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
    passJoinGate(w)
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
    passJoinGate(w)
    await flushPromises()
    lastViewer?.opts?.callbacks?.onScreenEnabled?.(true)
    await flushPromises()

    expect(w.find('[data-role="client-hint"]').exists()).toBe(false)
    w.unmount()
  })

  it('sends and displays the joined nickname, not the viewer slot id', async () => {
    const w = mount(LiveShareView, { global: { plugins: [i18n] } })
    passJoinGate(w, '小明')
    await flushPromises()

    await w.find('[data-action="chat"]').trigger('click')
    await flushPromises()

    const input = w.find('[data-role="chat-input"]')
    ;(input.element as HTMLInputElement).value = '大家好'
    await input.trigger('input')
    await w.find('.live-chat__compose').trigger('submit')
    await flushPromises()

    // The frame carries the nickname so the publisher (which resolves names
    // from the room roster) and the local echo agree.
    expect(sent).toHaveLength(1)
    expect(sent[0]).toMatchObject({ t: 'chat', name: '小明', text: '大家好' })
    expect(w.findAll('.chat-row__name').map((n) => n.text())).toEqual(['小明'])
    w.unmount()
  })

  it('shows the transcript it missed when it (re)joins', async () => {
    const w = mount(LiveShareView, { global: { plugins: [i18n] } })
    passJoinGate(w, '小明')
    await flushPromises()
    const onControl = lastViewer?.opts?.callbacks?.onControl

    // One line arrived live while the replay was still in flight; the replay
    // repeats it, and a duplicate here is exactly what a returning viewer must
    // not see.
    onControl?.({ t: 'chat', id: 'live', from: 'host', name: '主持人', text: '欢迎', at: 2000 })
    onControl?.({
      t: 'chat-history',
      messages: [
        { id: 'old1', from: 'v2', name: '小红', text: '早', at: 1000 },
        { id: 'live', from: 'host', name: '主持人', text: '欢迎', at: 2000 },
      ],
    })
    await flushPromises()

    await w.find('[data-action="chat"]').trigger('click')
    await flushPromises()

    expect(w.findAll('.chat-row__text').map((n) => n.text())).toEqual(['早', '欢迎'])
    expect(w.findAll('.chat-row__name').map((n) => n.text())).toEqual(['小红', '主持人'])
    w.unmount()
  })
})
