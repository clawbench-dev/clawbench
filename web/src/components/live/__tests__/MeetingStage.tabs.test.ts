import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick, ref, shallowRef } from 'vue'
import enMessages from '@/i18n/locales/en'
import MeetingStage from '../MeetingStage.vue'

/**
 * While a share runs the panel is three surfaces — setup, transcript, chat —
 * and exactly one is on screen. Stacked in a single scroll the transcript sat
 * below a wall of controls and the chat opened as a box wedged into the middle
 * of the form, which is what these tabs replace.
 *
 * The assertions are about WHICH surface is mounted, not about styling: a
 * `v-show` regression that keeps all three panes in the DOM would still pass a
 * class check but would leave a transcript rendering while the user reads the
 * audience list, so the panes are found by their `data-role` hooks.
 */

vi.mock('lucide-vue-next', () => ({
  MonitorUp: { name: 'MonitorUpIcon', render: () => null },
  Mic: { name: 'MicIcon', render: () => null },
  NotebookPen: { name: 'NotebookPenIcon', render: () => null },
  Volume2: { name: 'Volume2Icon', render: () => null },
  X: { name: 'XIcon', render: () => null },
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

vi.mock('@/composables/useMarkdownRenderer', () => ({
  renderMarkdownHtml: (text: string) => `<p>${text}</p>`,
}))

vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => null,
}))

vi.mock('@/utils/remoteControlSupport', () => ({
  resolveRemoteControlSupport: async () => false,
}))

/** `onControl` hands the panel its relay callback; the tests feed it messages. */
const onControlMock = vi.hoisted(() => vi.fn(() => () => undefined))

const shareApi = vi.hoisted(() => ({} as Record<string, unknown>))
vi.mock('@/composables/useLiveShare', async () => {
  const { ref: r, shallowRef: sr } = await import('vue')
  Object.assign(shareApi, {
    state: r('publishing'),
    token: r('tok-1'),
    shareUrl: r('https://h/share/tok-1'),
    shareLinks: r([{ label: 'LAN', url: 'https://h/share/tok-1' }]),
    title: r('demo'),
    viewers: r([{ id: 'v1', name: '', talk: 'muted' }]),
    talkEnabled: r(false),
    screenEnabled: r(true),
    screenSharer: r(''),
    remoteScreenStream: sr(null),
    error: r(''),
    levels: r({}),
    previewStream: sr(null),
    captureKind: r('screen'),
    videoEnabled: r(true),
    micEnabled: r(true),
    systemAudioEnabled: r(false),
    setMicEnabled: vi.fn(),
    setSystemAudioEnabled: vi.fn(),
    startNotes: vi.fn(() => true),
    stopNotes: vi.fn(async () => undefined),
    start: vi.fn(async () => true),
    stop: vi.fn(async () => undefined),
    setTalkEnabled: vi.fn(),
    revokeTalk: vi.fn(),
    muteAll: vi.fn(),
    setScreenEnabled: vi.fn(),
    revokeScreen: vi.fn(),
    sendControl: vi.fn(),
    sendControlTo: vi.fn(),
    onControl: onControlMock,
    switchSource: vi.fn(async () => true),
  })
  return {
    useLiveShare: () => shareApi,
    listCaptureSources: vi.fn(async () => []),
    getAudioCaptureSupport: vi.fn(async () => false),
  }
})

const notesApi = vi.hoisted(() => ({} as Record<string, unknown>))
vi.mock('@/composables/useLiveNotes', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useLiveNotes')>()
  const { ref: r } = await import('vue')
  Object.assign(notesApi, {
    phase: r('idle'),
    segments: r([]),
    minutes: r(''),
    minutesPhase: r('idle'),
    revisePhase: r('idle'),
    reviseError: r(''),
    artifacts: r({}),
    error: r(''),
    pending: r(0),
    lost: r(0),
    truncated: r(false),
    start: vi.fn(),
    addViewer: vi.fn(),
    removeViewer: vi.fn(),
    stopCapture: vi.fn(async () => undefined),
    finishUploads: vi.fn(async () => undefined),
    generateMinutes: vi.fn(async () => undefined),
    refresh: vi.fn(async () => undefined),
    reset: vi.fn(),
  })
  return { ...actual, useLiveNotes: () => notesApi }
})

function mountPanel() {
  // A fresh instance per mount: installing the same one twice trips
  // vue-i18n's "already set up" guard.
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(MeetingStage, {
    global: {
      plugins: [i18n],
      stubs: {
        ShareAddressList: true,
        LiveChatPanel: true,
        LoadingIndicator: true,
        LiveAnnotateOverlay: { template: '<div data-role="annotate-overlay" />' },
      },
    },
  })
}

/** The relay callback the panel registered on mount. */
function controlRelay(): (msg: Record<string, unknown>) => void {
  expect(onControlMock).toHaveBeenCalled()
  return onControlMock.mock.calls[0][0] as (msg: Record<string, unknown>) => void
}

function tab(wrapper: ReturnType<typeof mountPanel>, key: string) {
  return wrapper.find(`[data-live-tab="${key}"]`)
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(shareApi.state as { value: string }).value = 'publishing'
  ;(shareApi.screenSharer as { value: string }).value = ''
  ;(notesApi.phase as { value: string }).value = 'idle'
  ;(notesApi.segments as { value: unknown[] }).value = []
})

describe('MeetingStage tabs', () => {
  // A plain `<template>` in a Vue template is a NATIVE inert template element,
  // not a fragment: everything inside it lands in `content` and is never laid
  // out. Wrapping the tab panes in one left the whole stage body blank while
  // every DOM query still found the nodes — jsdom has no layout, so only an
  // assertion about the DOM tree catches it.
  it('renders the tab panes into the layout, not inside an inert template', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await tab(wrapper, 'chat').trigger('click')

    const pane = wrapper.find('[data-role="chat-pane"]')
    expect(pane.exists()).toBe(true)
    expect(pane.element.closest('template')).toBeNull()
  })

  it('opens on the setup tab and shows neither the transcript nor the chat', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(tab(wrapper, 'basic').classes()).toContain('live-tab--active')
    expect(wrapper.find('share-address-list-stub').exists()).toBe(true)
    expect(wrapper.find('[data-role="notes-pane"]').exists()).toBe(false)
    expect(wrapper.find('[data-role="chat-pane"]').exists()).toBe(false)

    // The labels are rendered from a key table, so the repo-wide
    // "no literal key is missing" scan cannot see them. A missing key renders
    // as the raw key, which is what this pins.
    expect(tab(wrapper, 'basic').text()).toBe('Setup')
    expect(tab(wrapper, 'notes').text()).toBe('Transcript')
    expect(tab(wrapper, 'chat').text()).toBe('Chat')
  })

  it('swaps one surface for another instead of stacking them', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await tab(wrapper, 'chat').trigger('click')
    expect(wrapper.find('[data-role="chat-pane"]').exists()).toBe(true)
    expect(wrapper.find('share-address-list-stub').exists()).toBe(false)
    expect(wrapper.find('[data-role="notes-pane"]').exists()).toBe(false)

    await tab(wrapper, 'notes').trigger('click')
    expect(wrapper.find('[data-role="notes-pane"]').exists()).toBe(true)
    expect(wrapper.find('[data-role="chat-pane"]').exists()).toBe(false)
  })

  it('counts a chat message as unread while another tab is open, and clears it on the chat tab', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    controlRelay()({ t: 'chat', id: 'm1', from: 'v1', text: 'hello', at: Date.now() })
    await nextTick()

    expect(tab(wrapper, 'chat').find('.live-count').text()).toBe('1')

    await tab(wrapper, 'chat').trigger('click')
    expect(wrapper.find('.live-count').exists()).toBe(false)
  })

  it('does not mark a message unread while the chat tab is already open', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await tab(wrapper, 'chat').trigger('click')

    controlRelay()({ t: 'chat', id: 'm2', from: 'v1', text: 'again', at: Date.now() })
    await nextTick()

    expect(wrapper.find('.live-count').exists()).toBe(false)
  })

  it('replays the transcript to a viewer that asks for it, and only to that viewer', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    const relay = controlRelay()

    relay({ t: 'chat', id: 'm1', from: 'v1', name: '小明', text: '早', at: 1000 })
    // The host's own lines must be in the replay too — they never pass through
    // the inbound handler.
    await tab(wrapper, 'chat').trigger('click')
    const chatPanel = wrapper.findComponent({ name: 'LiveChatPanel' })
    chatPanel.vm.$emit('send', '开会了')
    await nextTick()

    relay({ t: 'chat-history-req', from: 'v2' })

    expect(shareApi.sendControlTo).toHaveBeenCalledTimes(1)
    const [target, frame] = (shareApi.sendControlTo as ReturnType<typeof vi.fn>).mock.calls[0]
    expect(target).toBe('v2')
    expect(frame.t).toBe('chat-history')
    expect(frame.messages.map((m: { text: string }) => m.text)).toEqual(['早', '开会了'])
    // The sender is carried along so the receiver can tell its own lines apart.
    expect(frame.messages[0]).toMatchObject({ id: 'm1', from: 'v1' })
  })

  it('does not replay to a viewer whose request carries no id', async () => {
    mountPanel()
    await flushPromises()
    controlRelay()({ t: 'chat-history-req' })

    expect(shareApi.sendControlTo).not.toHaveBeenCalled()
  })

  it('says the transcript was off rather than showing an empty pane', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await tab(wrapper, 'notes').trigger('click')

    expect(wrapper.find('[data-role="notes-pane"]').text()).toContain('AI notes were off')
  })

  it('renders the live transcript in its own tab while recording', async () => {
    ;(notesApi.phase as { value: string }).value = 'recording'
    ;(notesApi.segments as { value: unknown[] }).value = [
      { id: 1, channel: 'mic', speaker: 'Me', startedAt: 1000, durationMs: 900, text: 'hello there' },
    ]

    const wrapper = mountPanel()
    await flushPromises()
    await tab(wrapper, 'notes').trigger('click')

    const pane = wrapper.find('[data-role="notes-pane"]')
    expect(pane.text()).toContain('hello there')
    expect(pane.text()).not.toContain('AI notes were off')
    // The tab bar carries the "still recording" dot while another tab is open.
    expect(tab(wrapper, 'notes').find('.live-tab__rec').exists()).toBe(true)
  })

  it('returns to the setup tab when a share ends and a new one starts', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await tab(wrapper, 'chat').trigger('click')
    expect(tab(wrapper, 'chat').classes()).toContain('live-tab--active')

    ;(shareApi.state as { value: string }).value = 'idle'
    await nextTick()
    ;(shareApi.state as { value: string }).value = 'publishing'
    await nextTick()

    expect(tab(wrapper, 'basic').classes()).toContain('live-tab--active')
  })

  it('drops the footer while a share is running: 结束共享 lives in the header', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    // Positive control: the header stop button is the one that stays.
    expect(wrapper.find('[data-action="panel-stop"]').exists()).toBe(true)
    // No footer bar and none of its buttons — an empty strip is what this
    // change removes, so both halves are asserted.
    expect(wrapper.find('.live-panel__footer').exists()).toBe(false)
    expect(wrapper.find('.fbtn').exists()).toBe(false)
  })
})
