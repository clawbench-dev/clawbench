import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick, ref, shallowRef } from 'vue'
import enMessages from '@/i18n/locales/en'
import { appLog } from '@/utils/appLog'
import { listAudioInputs, listCaptureSources } from '@/composables/useLiveShare'
import { useMeetingOptions } from '@/composables/useMeetingOptions'
import { LIVE_CHAT_MAX } from '@/share/liveControl'
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

vi.mock('lucide-vue-next', async (importOriginal) => {
  const actual = await importOriginal<typeof import('lucide-vue-next')>()
  const stub = (name: string) => ({ name, props: { size: Number }, render: () => null })
  // Spread the real module: a narrow mock turns any icon a shared child adds
  // (the source grid's check mark, the microphone list's) into `undefined` and
  // the whole panel fails to render.
  return {
    ...actual,
    Monitor: stub('Monitor'),
    MonitorUp: stub('MonitorUp'),
    MonitorOff: stub('MonitorOff'),
    Check: stub('Check'),
    Plus: stub('Plus'),
    Mic: stub('Mic'),
    NotebookPen: stub('NotebookPen'),
    Video: stub('Video'),
    Volume2: stub('Volume2'),
    X: stub('X'),
  }
})

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const toastShow = vi.hoisted(() => vi.fn())
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: toastShow }),
}))

/** Ending the meeting is behind a confirm; tests decide which answer it gives. */
const confirmMock = vi.hoisted(() => vi.fn(async (_message: string, _opts?: unknown) => true))
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: confirmMock }),
}))

/** The chat rows the server already archived for this meeting. */
const archivedChat = vi.hoisted(() => ({
  messages: [] as Array<Record<string, unknown>>,
  /** Set by a test that wants the history request to stay in flight. */
  pending: null as Promise<void> | null,
}))
// importOriginal, not a bare replacement: the stage also imports
// `meetingQuoteLabel` from this module (the quote card's label), and a
// whitelist mock that omits it makes the component fail to mount — the whole
// suite then reports "no export" instead of an assertion.
vi.mock('@/composables/useMeetingHistory', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useMeetingHistory')>()
  return {
    ...actual,
    useMeetingHistory: () => ({
      chat: async () => {
        if (archivedChat.pending) await archivedChat.pending
        return { messages: archivedChat.messages, count: archivedChat.messages.length }
      },
    }),
  }
})

vi.mock('@/composables/useMarkdownRenderer', () => ({
  renderMarkdownHtml: (text: string) => `<p>${text}</p>`,
}))

const nativeBridge = vi.hoisted(() => ({ current: null as unknown }))
vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => nativeBridge.current,
  // These tests run in a browser, not the desktop shell: no popup window.
  isDesktopApp: () => false,
}))

/** What the native injector probe answers; tests flip it to prove the panel
 *  forwards the RESULT (not a constant) into the room. */
const remoteControlOk = vi.hoisted(() => ({ value: false }))

vi.mock('@/utils/remoteControlSupport', () => ({
  resolveRemoteControlSupport: async () => remoteControlOk.value,
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
    captureSourceId: r(''),
    videoEnabled: r(true),
    cameraEnabled: r(false),
    previewCameraStream: sr(null),
    micEnabled: r(true),
    setMicEnabled: vi.fn(),
    setMicChoice: vi.fn(async () => true),
    retryMicrophone: vi.fn(),
    micRecovering: r(false),
    activeMicDevice: r(''),
    setCameraDevice: vi.fn(async () => true),
    startScreen: vi.fn(async () => true),
    stopScreen: vi.fn(async () => undefined),
    startNotes: vi.fn(() => true),
    stopNotes: vi.fn(async () => undefined),
    start: vi.fn(async () => true),
    stop: vi.fn(async () => undefined),
    setTalkEnabled: vi.fn(),
    revokeTalk: vi.fn(),
    setViewerMuted: vi.fn(),
    kickViewer: vi.fn(),
    muteAll: vi.fn(),
    setScreenEnabled: vi.fn(),
    revokeScreen: vi.fn(),
    sendControl: vi.fn(),
    sendControlTo: vi.fn(),
    onControl: onControlMock,
    switchSource: vi.fn(async () => true),
    // The panel pushes the native injector probe into the room so
    // viewers can grey their "request control" button.
    setControlSupported: vi.fn(),
  })
  return {
    useLiveShare: () => shareApi,
    listCaptureSources: vi.fn(async () => []),
    hasCameraDevice: vi.fn(async () => true),
    listAudioInputs: vi.fn(async () => []),
    listVideoInputs: vi.fn(async () => []),
  }
})

const notesApi = vi.hoisted(() => ({} as Record<string, unknown>))
vi.mock('@/composables/useLiveNotes', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useLiveNotes')>()
  const { ref: r } = await import('vue')
  Object.assign(notesApi, {
    phase: r('idle'),
    segments: r([]),
    liveLines: r({}),
    minutes: r(''),
    minutesPhase: r('idle'),
    revisePhase: r('idle'),
    reviseError: r(''),
    artifacts: r({}),
    error: r(''),
    pending: r(0),
    lost: r(0),
    truncated: r(false),
    health: r('ok'),
    silentForMs: r(0),
    deadChannels: r([]),
    connection: r('online'),
    audioSuspended: r(false),
    setConnectionState: vi.fn(),
    setAudioSuspended: vi.fn(),
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

function mountPanel(opts: { realChat?: boolean } = {}) {
  // A fresh instance per mount: installing the same one twice trips
  // vue-i18n's "already set up" guard.
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(MeetingStage, {
    global: {
      plugins: [i18n],
      stubs: {
        ShareAddressList: true,
        // The real panel is mounted by the mirroring test below; everything
        // else only needs the tab surface to exist.
        LiveChatPanel: opts.realChat ? false : true,
        LoadingIndicator: true,
        LiveAnnotateOverlay: { template: '<div data-role="annotate-overlay" />' },
        SpeakerAvatar: { props: ['name'], template: '<span class="sa" :data-name="name" />' },
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
  nativeBridge.current = null
  archivedChat.messages = []
  archivedChat.pending = null
  ;(shareApi.state as { value: string }).value = 'publishing'
  ;(shareApi.screenSharer as { value: string }).value = ''
  ;(shareApi.captureKind as { value: string }).value = 'screen'
  ;(shareApi.captureSourceId as { value: string }).value = ''
  ;(notesApi.phase as { value: string }).value = 'idle'
  ;(notesApi.segments as { value: unknown[] }).value = []
  ;(notesApi.health as { value: string }).value = 'ok'
  ;(notesApi.silentForMs as { value: number }).value = 0
  ;(notesApi.pending as { value: number }).value = 0
  ;(notesApi.lost as { value: number }).value = 0
  ;(shareApi.micRecovering as { value: boolean }).value = false
  // The chosen input is a module singleton shared with the create form.
  useMeetingOptions().micChoice.value = 'default'
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

  // Leaving the stage for the record list is navigation, not an exit — the
  // room lives outside this component. The gesture existed already; a desktop
  // host had no control for it.
  it('offers a visible way back to the record list', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const back = wrapper.find('.live-panel__header [data-action="stage-back"]')
    expect(back.exists()).toBe(true)
    expect(back.attributes('title')).toBe(enMessages.meeting.stage.back)

    await back.trigger('click')
    expect(wrapper.emitted('back')).toHaveLength(1)
  })

  // Remote control is the publisher's capability, and viewers grey their button
  // on what the room advertises. The panel is the only place that knows whether
  // the shell has an input injector, so it has to push the probe result through.
  it('tells the room what the injector probe answered', async () => {
    remoteControlOk.value = true
    mountPanel()
    await flushPromises()

    expect(shareApi.setControlSupported).toHaveBeenCalledWith(true)
    remoteControlOk.value = false
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
    expect(tab(wrapper, 'basic').text()).toBe('Manage meeting')
    // The headcount rides in the same label, so compare the label alone.
    expect(tab(wrapper, 'participants').text()).toContain('Participants')
    expect(tab(wrapper, 'notes').text()).toBe('Transcript')
    expect(tab(wrapper, 'chat').text()).toBe('Chat')
  })

  it('shows the headcount on the participants tab, host included', async () => {
    ;(shareApi.viewers as { value: unknown[] }).value = [
      { id: 'v1', name: '', talk: 'muted' },
      { id: 'v2', name: '', talk: 'muted' },
    ]
    try {
      const wrapper = mountPanel()
      await flushPromises()

      // Two viewers plus the host. The number is on the tab so the host can see
      // it without opening the pane, and the same count is printed in the pane.
      expect(tab(wrapper, 'participants').find('[data-role="participants-count"]').text()).toBe('3')

      await tab(wrapper, 'participants').trigger('click')
      expect(wrapper.find('[data-role="participants-total"]').text()).toBe('3 in the meeting')
    } finally {
      ;(shareApi.viewers as { value: unknown[] }).value = [{ id: 'v1', name: '', talk: 'muted' }]
    }
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

  it('gives the participants their own tab, right after the setup tab', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const keys = wrapper.findAll('[data-live-tab]').map((b) => b.attributes('data-live-tab'))
    expect(keys).toEqual(['basic', 'participants', 'notes', 'chat'])

    // The list used to be a section at the bottom of 会议管理, where it sat
    // below a wall of device cards.
    expect(wrapper.find('[data-role="participants"]').exists()).toBe(false)
    await tab(wrapper, 'participants').trigger('click')
    expect(wrapper.find('[data-role="participants"]').exists()).toBe(true)
    expect(wrapper.find('share-address-list-stub').exists()).toBe(false)
  })

  it('badges the participants tab while an approval is waiting', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    // A request nobody notices is the same as a refusal: the badge is the only
    // signal outside the tab.
    expect(tab(wrapper, 'participants').find('.live-count').exists()).toBe(false)

    controlRelay()({ t: 'ctrl-req', from: 'v1' })
    await nextTick()
    expect(tab(wrapper, 'participants').find('[data-role="participants-badge"]').text()).toBe('1')

    // Answering it clears the badge without leaving the tab.
    controlRelay()({ t: 'ctrl-release', from: 'v1' })
    await nextTick()
    expect(tab(wrapper, 'participants').find('.live-count').exists()).toBe(false)
  })

  it('keeps 听记, 允许成员发言 and 允许成员共享屏幕 in one switch group', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    // User requirement: the three room-level switches read as one decision, so
    // they sit together instead of being scattered down the form.
    const group = wrapper.findAll('.meeting-checks')[0]
    expect(group.find('[data-role="stage-notes"]').exists()).toBe(true)
    expect(group.find('[data-role="stage-talk"]').exists()).toBe(true)
    expect(group.find('[data-role="stage-screen-share"]').exists()).toBe(true)
    expect(wrapper.findAll('.meeting-checks')).toHaveLength(1)
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

    const pane = wrapper.find('[data-role="notes-pane"]')
    expect(pane.text()).toContain('AI notes were off')
    // The same centred card the record's tabs draw, not a muted line in the
    // corner — that read as a pane that failed to render.
    expect(pane.find('[data-role="notes-off"]').classes()).toContain('meeting-empty')
  })

  it('draws the listening card while 听记 runs but nothing was transcribed yet', async () => {
    ;(notesApi.phase as { value: string }).value = 'recording'

    const wrapper = mountPanel()
    await flushPromises()
    await tab(wrapper, 'notes').trigger('click')

    const pane = wrapper.find('[data-role="notes-pane"]')
    const card = pane.find('[data-role="notes-listening"]')
    expect(card.exists()).toBe(true)
    expect(card.classes()).toContain('meeting-empty')
    expect(card.text()).toContain('Listening')
    // Not the "it was off" copy: the recorder is running, the lines just have
    // not arrived.
    expect(pane.text()).not.toContain('AI notes were off')
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

  /**
   * The pane used to look identical whether nobody had spoken yet or the
   * microphone had died — the same empty card, with no way to tell which. The
   * bar is the difference, and it must appear without any new line arriving.
   */
  it('says the microphone is gone instead of drawing the listening card', async () => {
    ;(notesApi.phase as { value: string }).value = 'recording'
    ;(notesApi.health as { value: string }).value = 'micDead'

    const wrapper = mountPanel()
    await flushPromises()
    await tab(wrapper, 'notes').trigger('click')

    const pane = wrapper.find('[data-role="notes-pane"]')
    expect(pane.find('[data-role="notes-status"]').attributes('data-health')).toBe('micDead')
    expect(pane.text()).toContain('The microphone disconnected')
    // "Listening…" would be a lie, and it is the lie this replaces.
    expect(pane.find('[data-role="notes-listening"]').exists()).toBe(false)
  })

  it('offers the reconnect action when recovery gave up', async () => {
    ;(notesApi.phase as { value: string }).value = 'recording'
    ;(notesApi.health as { value: string }).value = 'micDead'

    const wrapper = mountPanel()
    await flushPromises()
    await tab(wrapper, 'notes').trigger('click')
    await wrapper.find('[data-role="notes-retry-mic"]').trigger('click')

    expect(shareApi.retryMicrophone).toHaveBeenCalled()
  })

  it('turns the tab dot into an alert while the recorder is not producing', async () => {
    ;(notesApi.phase as { value: string }).value = 'recording'
    ;(notesApi.health as { value: string }).value = 'silent'

    const wrapper = mountPanel()
    await flushPromises()

    // Visible from another tab: that is the point of putting it here.
    expect(tab(wrapper, 'notes').find('.live-tab__rec--alert').exists()).toBe(true)
  })

  it('keeps the status bar out of the way when nothing has happened yet', async () => {
    ;(notesApi.phase as { value: string }).value = 'recording'

    const wrapper = mountPanel()
    await flushPromises()
    await tab(wrapper, 'notes').trigger('click')

    // Healthy and empty: the listening card says it, one voice is enough.
    expect(wrapper.find('[data-role="notes-status"]').exists()).toBe(false)
    expect(wrapper.find('[data-role="notes-listening"]').exists()).toBe(true)
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

  it('puts 结束会议 and 标注画面 in the panel footer, not the header or a form tab', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const footer = wrapper.find('.meeting-footer')
    expect(footer.exists()).toBe(true)

    // The destructive action is the shared red pill, and it lives in the bar at
    // the foot — the header is status only (title + 共享中).
    const end = footer.find('[data-action="panel-end"]')
    expect(end.exists()).toBe(true)
    expect(end.classes()).toContain('fbtn-danger')
    expect(wrapper.find('.live-panel__header [data-action="panel-end"]').exists()).toBe(false)

    // 标注画面 is a meeting-level tool, reachable from every tab: a host who is
    // presenting must not have to open 会议管理 to draw on the picture.
    expect(footer.find('[data-action="annotate"]').exists()).toBe(true)
    expect(wrapper.find('[data-live-tab="basic"] [data-action="annotate"]').exists()).toBe(false)
  })

  it('ends the meeting from the footer pill, after the destructive confirm', async () => {
    confirmMock.mockResolvedValueOnce(true)
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-action="panel-end"]').trigger('click')
    await flushPromises()

    // The pill sits next to 标注画面 and the click disconnects everyone: it
    // must ask first, and the question must be flagged destructive.
    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(confirmMock.mock.calls[0][1]).toEqual({ dangerous: true })
    expect(shareApi.stop).toHaveBeenCalled()
    expect(wrapper.emitted('ended')).toEqual([['tok-1']])
  })

  it('keeps the meeting running when the confirm is dismissed', async () => {
    confirmMock.mockResolvedValueOnce(false)
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-action="panel-end"]').trigger('click')
    await flushPromises()

    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(shareApi.stop).not.toHaveBeenCalled()
    expect(wrapper.emitted('ended')).toBeUndefined()
  })
})

/**
 * There is no meeting type: the picture is a runtime state.
 *
 * A camera picture has no desktop behind it, so everything that assumes
 * desktop coordinates — annotate, switch source, remote control — must stay
 * away; a screen picture brings them back. Both can change mid-meeting.
 */
describe('MeetingStage picture toggles', () => {
  it('offers the camera toggle and self-view, without the screen-only tools', async () => {
    ;(shareApi.captureKind as { value: unknown }).value = null
    ;(shareApi.previewCameraStream as { value: unknown }).value = { id: 'cam' }
    ;(shareApi.cameraEnabled as { value: boolean }).value = true
    try {
      const wrapper = mountPanel()
      await flushPromises()

      expect(wrapper.find('[data-role="device-choice"][data-kind="video"]').exists()).toBe(true)
      expect(wrapper.find('[data-role="camera-self"]').exists()).toBe(true)
      // A camera image has no desktop behind it.
      expect(wrapper.find('[data-action="annotate"]').exists()).toBe(false)
      // ...but the picture grid stays: it is how a camera-only meeting adds a
      // screen back, not a screen-only tool.
      expect(wrapper.find('.capture-sources').exists()).toBe(true)
      // The audience may still be allowed to share theirs: the two are
      // independent, and the switch no longer disappears with the picture.
      expect(wrapper.text()).toContain(enMessages.liveShare.allowScreenShare)
    } finally {
      ;(shareApi.captureKind as { value: unknown }).value = 'screen'
      ;(shareApi.previewCameraStream as { value: unknown }).value = null
      ;(shareApi.cameraEnabled as { value: boolean }).value = false
    }
  })

  it('keeps the screen-only tools for a screen meeting', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    // Positive control for the test above: same mount, screen capture kind.
    expect(wrapper.find('[data-action="annotate"]').exists()).toBe(true)
  })

  it('turns the camera off through the mute card', async () => {
    ;(shareApi.captureKind as { value: unknown }).value = null
    ;(shareApi.previewCameraStream as { value: unknown }).value = { id: 'cam' }
    ;(shareApi.cameraEnabled as { value: boolean }).value = true
    try {
      const wrapper = mountPanel()
      await flushPromises()

      await wrapper.find('[data-role="device-choice"][data-kind="video"] [data-device="__off__"]').trigger('click')
      await flushPromises()
      expect(shareApi.setCameraDevice).toHaveBeenCalledWith('')
    } finally {
      ;(shareApi.captureKind as { value: unknown }).value = 'screen'
      ;(shareApi.previewCameraStream as { value: unknown }).value = null
      ;(shareApi.cameraEnabled as { value: boolean }).value = false
    }
  })

  it('starts and stops the screen from the same picture grid the create form uses', async () => {
    nativeBridge.current = { listCaptureSources: vi.fn() }
    ;(listCaptureSources as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce([
      { id: 'screen:0:0', name: 'Entire Screen', kind: 'screen', thumbnail: '' },
      { id: 'window:1:0', name: 'Terminal', kind: 'window', thumbnail: '' },
    ])
    ;(shareApi.captureSourceId as { value: string }).value = 'screen:0:0'
    const wrapper = mountPanel()
    await flushPromises()
    // The list is only asked for when the host asks to share.
    await wrapper.find('[data-source="__pick__"]').trigger('click')
    await flushPromises()

    // Sharing: the live tile is the selected one, not the "off" tile.
    expect(wrapper.find('[data-source="screen:0:0"]').classes()).toContain('capture-source--on')
    expect(wrapper.find('[data-source="__off__"]').classes()).not.toContain('capture-source--on')

    // 不共享 is a tile, and clicking it stops the picture without ending the
    // meeting — the same control the create form draws.
    await wrapper.find('[data-source="__off__"]').trigger('click')
    expect(shareApi.stopScreen).toHaveBeenCalledTimes(1)

    // Not sharing: picking a tile starts the capture with THAT id.
    ;(shareApi.captureKind as { value: unknown }).value = null
    ;(shareApi.captureSourceId as { value: string }).value = ''
    await flushPromises()
    expect(wrapper.find('[data-source="__off__"]').classes()).toContain('capture-source--on')

    await wrapper.find('[data-source="window:1:0"]').trigger('click')
    await flushPromises()
    expect(shareApi.startScreen).toHaveBeenCalledWith('window:1:0')
  })

  it('swaps to another tile without stopping the meeting', async () => {
    nativeBridge.current = { listCaptureSources: vi.fn() }
    ;(listCaptureSources as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce([
      { id: 'screen:0:0', name: 'Entire Screen', kind: 'screen', thumbnail: '' },
      { id: 'window:1:0', name: 'Terminal', kind: 'window', thumbnail: '' },
    ])
    ;(shareApi.captureSourceId as { value: string }).value = 'screen:0:0'
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.find('[data-source="__pick__"]').trigger('click')
    await flushPromises()

    await wrapper.find('[data-source="window:1:0"]').trigger('click')
    await flushPromises()

    // A different tile while a screen is live is a swap, not a restart: the
    // viewers' connections stay up.
    expect(shareApi.switchSource).toHaveBeenCalledWith('window:1:0')
    expect(shareApi.stopScreen).not.toHaveBeenCalled()
  })
})

describe('MeetingStage transcript rows', () => {
  it('draws one avatar per speaker turn, beside the name and the text', async () => {
    ;(notesApi.phase as { value: string }).value = 'recording'
    ;(notesApi.segments as { value: unknown[] }).value = [
      { id: 1, channel: 'mic', speaker: '我', startedAt: 1_000, durationMs: 2_000, text: '第一句' },
      { id: 2, channel: 'viewer:v1', speaker: '小明', startedAt: 9_000, durationMs: 2_000, text: '第二句' },
    ]
    const wrapper = mountPanel()
    await flushPromises()
    await tab(wrapper, 'notes').trigger('click')

    const rows = wrapper.findAll('.transcript__row')
    expect(rows).toHaveLength(2)
    // The avatar is what tells two participants apart when the same names
    // repeat down a long transcript, so every turn carries its own.
    expect(rows[0].find('.sa').attributes('data-name')).toBe('我')
    expect(rows[1].find('.sa').attributes('data-name')).toBe('小明')
    expect(rows[0].find('.transcript__speaker').text()).toBe('我')
    expect(rows[0].find('.transcript__text').text()).toBe('第一句')
  })
})

describe('MeetingStage microphone input', () => {
  const devices = [
    { id: 'mic-a', name: 'Built-in Microphone' },
    { id: 'mic-b', name: 'Headset Microphone' },
  ]

  it('moves the capture to the card that was clicked, and remembers it', async () => {
    ;(listAudioInputs as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(devices)
    const wrapper = mountPanel()
    await flushPromises()

    // Mute card + system default + the two real inputs.
    const cards = wrapper.findAll('[data-role="device-choice"][data-kind="audio"] .device-choice__card')
    expect(cards).toHaveLength(4)
    await wrapper.find('[data-role="device-choice"][data-kind="audio"] [data-device="mic-b"]').trigger('click')
    await flushPromises()

    expect(shareApi.setMicChoice).toHaveBeenCalledWith('mic-b')
    expect(useMeetingOptions().micChoice.value).toBe('mic-b')
  })

  it('keeps the previous device when the switch fails', async () => {
    ;(listAudioInputs as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(devices)
    ;(shareApi.setMicChoice as ReturnType<typeof vi.fn>).mockResolvedValueOnce(false)
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-role="device-choice"][data-kind="audio"] [data-device="mic-b"]').trigger('click')
    await flushPromises()

    // A device that refuses to open must not be remembered as the choice: the
    // meeting is still capturing from the old one.
    expect(useMeetingOptions().micChoice.value).toBe('default')
  })

  it('shows the mute card as selected while the microphone is off', async () => {
    ;(listAudioInputs as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(devices)
    ;(shareApi.micEnabled as { value: boolean }).value = false
    const wrapper = mountPanel()
    await flushPromises()

    // Muting is a CARD now, so the group stays on screen and says which state
    // it is in — hiding it would leave no way back.
    const grid = wrapper.find('[data-role="device-choice"][data-kind="audio"]')
    expect(grid.exists()).toBe(true)
    expect(grid.find('[data-device="__off__"]').classes()).toContain('device-choice__card--on')
    ;(shareApi.micEnabled as { value: boolean }).value = true
  })
})

describe('MeetingStage chat sides', () => {
  it('puts the local user’s own message on the other side of the pane', async () => {
    const wrapper = mountPanel({ realChat: true })
    await flushPromises()
    await tab(wrapper, 'chat').trigger('click')

    // Someone else speaks, then we do.
    controlRelay()({ t: 'chat', id: 'm1', from: 'v1', name: '小明', text: '早', at: 1_700_000_001_000 })
    await nextTick()
    const input = wrapper.find('[data-role="chat-input"]')
    await input.setValue('早啊')
    await wrapper.find('form').trigger('submit')
    await nextTick()

    const rows = wrapper.findAll('.chat-row')
    expect(rows).toHaveLength(2)
    expect(rows[0].classes()).not.toContain('chat-row--self')
    expect(rows[1].classes()).toContain('chat-row--self')
    expect(rows[1].find('.chat-row__text').text()).toBe('早啊')
  })
})

/**
 * The chat lives in this component, so anything that removes the component —
 * or a client that joined the meeting late — must not lose it. Two different
 * paths: the lines the server already archived, and the failures that used to
 * be silent.
 */
describe('MeetingStage chat archive', () => {
  it('restores the lines archived before this client was on the stage', async () => {
    archivedChat.messages = [
      { id: 1, clientId: 'c1', speaker: '小明', viewerId: 'v1', text: '早', at: 1_700_000_001_000 },
      { id: 2, clientId: 'c2', speaker: 'Alex', text: '早啊', at: 1_700_000_002_000 },
    ]
    const wrapper = mountPanel({ realChat: true })
    await flushPromises()
    await tab(wrapper, 'chat').trigger('click')

    const rows = wrapper.findAll('.chat-row')
    expect(rows).toHaveLength(2)
    expect(rows[0].find('.chat-row__text').text()).toBe('早')
    // A stored row with no viewerId is the host's own line: it belongs on the
    // right, exactly like the live one.
    expect(rows[1].classes()).toContain('chat-row--self')
  })

  it('does not duplicate a line that arrives while the history is loading', async () => {
    let release = (): void => {}
    archivedChat.pending = new Promise<void>((resolve) => {
      release = resolve
    })
    const wrapper = mountPanel({ realChat: true })
    await flushPromises()

    // The archived request is still open when the live line lands: the row it
    // also carries must not be appended twice.
    controlRelay()({ t: 'chat', id: 'm1', from: 'v1', name: '小明', text: '早', at: 1_700_000_001_000 })
    await nextTick()
    archivedChat.messages = [
      { id: 1, clientId: 'm1', speaker: '小明', viewerId: 'v1', text: '早', at: 1_700_000_001_000 },
    ]
    release()
    await flushPromises()

    await tab(wrapper, 'chat').trigger('click')
    expect(wrapper.findAll('.chat-row')).toHaveLength(1)
  })

  it('reports a chat line the server refused to archive', async () => {
    const fetchMock = vi.fn(async () => ({ ok: false, status: 500, json: async () => ({}) }))
    vi.stubGlobal('fetch', fetchMock)
    try {
      const wrapper = mountPanel()
      await flushPromises()
      controlRelay()({ t: 'chat', id: 'm1', from: 'v1', name: '小明', text: '早', at: Date.now() })
      await flushPromises()

      // A 4xx/5xx is not a transport error, so the old `.catch` never ran: the
      // host saw the line in its own pane while the record silently lost it.
      expect(appLog.w).toHaveBeenCalledWith('MeetingStage', 'persist chat failed', expect.any(Error))
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it('says so when the shell refuses to inject a remote-control event', async () => {
    remoteControlOk.value = true
    const remoteControl = vi.fn(async () => {
      throw new Error('injector blocked')
    })
    nativeBridge.current = { remoteControl }
    try {
      const wrapper = mountPanel()
      await flushPromises()
      controlRelay()({ t: 'ctrl-req', from: 'v1' })
      await nextTick()
      await tab(wrapper, 'participants').trigger('click')
      await wrapper.find('[data-action="control-allow"]').trigger('click')
      await flushPromises()

      controlRelay()({ t: 'ctrl-ev', from: 'v1', kind: 'move', x: 0.1, y: 0.2 })
      await flushPromises()

      // Every mouse move would otherwise be an unhandled rejection: the control
      // is dead and the host is told nothing.
      expect(appLog.w).toHaveBeenCalledWith('MeetingStage', 'remote control injection failed', expect.any(Error))
      expect(toastShow).toHaveBeenCalledWith(enMessages.liveShare.controlUnsupported, expect.anything())
    } finally {
      remoteControlOk.value = false
    }
  })
})

describe('MeetingStage chat transcript bound', () => {
  it('keeps only the newest lines of a long meeting', async () => {
    // The publisher's array is the source of every replay and every render, and
    // a meeting runs for hours: without a bound it grows for the whole session.
    const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }))
    vi.stubGlobal('fetch', fetchMock)
    try {
      const wrapper = mountPanel({ realChat: true })
      await flushPromises()
      const relay = controlRelay()
      const total = LIVE_CHAT_MAX + 20
      for (let i = 0; i < total; i++) {
        relay({ t: 'chat', id: `m${i}`, from: 'v1', name: '小明', text: `line ${i}`, at: i })
      }
      await nextTick()
      await tab(wrapper, 'chat').trigger('click')

      const rows = wrapper.findAll('.chat-row')
      expect(rows).toHaveLength(LIVE_CHAT_MAX)
      // The OLDEST are the ones dropped, not the newest.
      expect(rows[0].find('.chat-row__text').text()).toBe(`line ${total - LIVE_CHAT_MAX}`)
      expect(rows[rows.length - 1].find('.chat-row__text').text()).toBe(`line ${total - 1}`)
    } finally {
      vi.unstubAllGlobals()
    }
  })
})
