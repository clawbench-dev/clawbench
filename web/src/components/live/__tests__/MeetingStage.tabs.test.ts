import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick, ref, shallowRef } from 'vue'
import enMessages from '@/i18n/locales/en'
import { listAudioInputs, listCaptureSources } from '@/composables/useLiveShare'
import { useMeetingOptions } from '@/composables/useMeetingOptions'
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

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

vi.mock('@/composables/useMarkdownRenderer', () => ({
  renderMarkdownHtml: (text: string) => `<p>${text}</p>`,
}))

const nativeBridge = vi.hoisted(() => ({ current: null as unknown }))
vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => nativeBridge.current,
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
  ;(shareApi.state as { value: string }).value = 'publishing'
  ;(shareApi.screenSharer as { value: string }).value = ''
  ;(shareApi.captureKind as { value: string }).value = 'screen'
  ;(shareApi.captureSourceId as { value: string }).value = ''
  ;(notesApi.phase as { value: string }).value = 'idle'
  ;(notesApi.segments as { value: unknown[] }).value = []
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
