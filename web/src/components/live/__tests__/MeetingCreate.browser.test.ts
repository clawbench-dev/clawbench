import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import { _resetMeetingOptions, useMeetingOptions } from '@/composables/useMeetingOptions'
import { _resetLiveSFUConfigForTest } from '@/composables/useLiveSFUConfig'
import { listCaptureSources } from '@/composables/useLiveShare'
import MeetingCreate from '../MeetingCreate.vue'

/**
 * The publisher setup form in a plain browser.
 *
 * A browser has no source list — its own picker is the source choice — so the
 * form must not read the empty list as "nothing to share", and "start" must be
 * clickable without a tile selected. The other half is the message shown when
 * the page cannot capture at all: an insecure origin is not a stale client, and
 * telling the user to update theirs sends them looking in the wrong place.
 */

vi.mock('lucide-vue-next', async (importOriginal) => {
  const actual = await importOriginal<typeof import('lucide-vue-next')>()
  const stub = (name: string) => ({ name, props: { size: Number }, render: () => null })
  // Spread the real module: a narrow mock turns any icon a shared child adds
  // into `undefined` and the whole form fails to render.
  return {
    ...actual,
    ArrowLeft: stub('ArrowLeft'),
    Check: stub('Check'),
    Mic: stub('Mic'),
    MicOff: stub('MicOff'),
    Monitor: stub('Monitor'),
    MonitorOff: stub('MonitorOff'),
    NotebookPen: stub('NotebookPen'),
    Plus: stub('Plus'),
    Video: stub('Video'),
    VideoOff: stub('VideoOff'),
    Volume2: stub('Volume2'),
  }
})

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// The relay section reads its stored endpoint on mount. Answered here rather
// than left to the real client: jsdom has no server, and an unhandled rejection
// from a background read would fail an unrelated test.
const sfuApi = vi.hoisted(() => ({
  state: { url: '', hasSecret: false, configured: false } as {
    url: string
    hasSecret: boolean
    configured: boolean
  },
}))
vi.mock('@/utils/api', () => ({
  apiGet: vi.fn(async () => ({ ...sfuApi.state })),
  apiPost: vi.fn(async () => ({ success: true, message: 'ok' })),
  apiPatch: vi.fn(async (_url: string, body: Record<string, unknown>) => ({
    url: (body.url as string) ?? '',
    hasSecret: true,
    configured: true,
  })),
  apiPut: vi.fn(),
  apiDelete: vi.fn(),
  cancelChat: vi.fn(),
}))

/**
 * The desktop bridge. Null is a plain browser; a native host that offers a
 * source list is what puts the form in its "ask me for a list" state.
 */
const nativeApi = vi.hoisted(() => ({ value: null as unknown }))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

vi.mock('@/composables/useMarkdownRenderer', () => ({
  renderMarkdownHtml: (text: string) => `<p>${text}</p>`,
}))

vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => nativeApi.value,
}))

vi.mock('@/utils/remoteControlSupport', () => ({
  resolveRemoteControlSupport: async () => false,
}))

// The create page reads the account nickname (user.nickname) for its required
// identity field, and writes an edit back through the settings API.
const settingsApi = vi.hoisted(() => ({
  serverConfig: { value: {} as Record<string, unknown> },
  localConfig: {
    meetingDefaultNotes: true,
    meetingDefaultShots: true,
    meetingDefaultAllowTalk: true,
    meetingDefaultAllowMemberScreen: true,
  } as Record<string, unknown>,
  setServerValue: vi.fn(),
}))
vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    serverConfig: settingsApi.serverConfig,
    localConfig: settingsApi.localConfig,
    setServerValue: settingsApi.setServerValue,
  }),
}))

/** Whether this machine reports a camera; several tests flip it. */
const cameraDevice = vi.hoisted(() => ({ has: true }))

/** The microphones this machine reports; several tests set it. */
const audioDevices = vi.hoisted(() => ({ list: [] as Array<{ id: string; name: string }> }))
const videoDevices = vi.hoisted(() => ({ list: [] as Array<{ id: string; name: string }> }))

const shareApi = vi.hoisted(() => ({} as Record<string, unknown>))
vi.mock('@/composables/useLiveShare', async () => {
  const { ref: r, shallowRef: sr } = await import('vue')
  Object.assign(shareApi, {
    state: r('idle'),
    token: r(''),
    shareUrl: r(''),
    shareLinks: r([]),
    title: r(''),
    viewers: r([]),
    talkEnabled: r(false),
    screenEnabled: r(false),
    screenSharer: r(''),
    remoteScreenStream: sr(null),
    error: r(''),
    levels: r({}),
    previewStream: sr(null),
    captureKind: r('screen'),
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
    onControl: vi.fn(() => () => undefined),
    switchSource: vi.fn(async () => true),
    // The panel pushes the native injector probe into the room so
    // viewers can grey their "request control" button.
    setControlSupported: vi.fn(),
  })
  return {
    useLiveShare: () => shareApi,
    listCaptureSources: vi.fn(async () => []),
    listAudioInputs: vi.fn(async () => audioDevices.list),
    listVideoInputs: vi.fn(async () => videoDevices.list),
    getAudioCaptureSupport: vi.fn(async () => true),
    hasCameraDevice: vi.fn(async () => cameraDevice.has),
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

/** The cards of one device axis, in order. */
function cards(wrapper: ReturnType<typeof mountPanel>, kind: 'audio' | 'video') {
  return wrapper.findAll(`[data-role="device-choice"][data-kind="${kind}"] .device-choice__card`)
}

/** One card of one axis; `id` is a device id, `'default'` or `'__off__'`. */
function card(wrapper: ReturnType<typeof mountPanel>, kind: 'audio' | 'video', id: string) {
  return wrapper.find(`[data-role="device-choice"][data-kind="${kind}"] [data-device="${id}"]`)
}

function mountPanel() {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(MeetingCreate, {
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

/** Install (or remove) the browser capture API, as a secure/insecure page would. */
function stubCaptureApi(available: boolean): void {
  vi.stubGlobal('navigator', available ? { mediaDevices: { getDisplayMedia: vi.fn() } } : {})
}

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  // The relay configuration is a module-level singleton too.
  _resetLiveSFUConfigForTest()
  sfuApi.state = { url: '', hasSecret: false, configured: false }
  // The capture options are module-level singletons shared with the running
  // meeting; a test that flips the camera would otherwise leak into the next
  // one and make the expectations order-dependent.
  _resetMeetingOptions()
  settingsApi.serverConfig.value = {}
  settingsApi.setServerValue.mockReset()
  settingsApi.setServerValue.mockResolvedValue({ needsRestart: false, changedColdFields: [], warnings: [] })
  ;(shareApi.state as { value: string }).value = 'idle'
  ;(shareApi.viewers as { value: unknown[] }).value = []
  cameraDevice.has = true
  nativeApi.value = null
  audioDevices.list = []
  videoDevices.list = []
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('MeetingCreate in a browser', () => {
  it('offers the picture as frames and keeps start usable with no source list', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    // A browser's own picker IS the source choice, so an empty list is not a
    // problem: it becomes one tile among the frames.
    expect(wrapper.find('.meeting-type-grid').exists()).toBe(false)
    const tiles = wrapper.findAll('.capture-source')
    expect(tiles).toHaveLength(2)
    expect(wrapper.find('[data-source="__off__"]').exists()).toBe(true)
    const browserTile = wrapper.find('[data-source="browser:screen"]')
    expect(browserTile.exists()).toBe(true)
    expect(browserTile.text()).toContain(enMessages.meeting.create.browserSource)
    // Screen sharing stays the default; the camera is opt-in, which the
    // selected "off" card says without a checkbox.
    expect(browserTile.classes()).toContain('capture-source--on')
    expect(card(wrapper, 'video', '__off__').classes()).toContain('device-choice__card--on')
    expect(wrapper.find('[data-role="meeting-screen"]').exists()).toBe(false)

    const start = wrapper.find('[data-action="meeting-start"]')
    expect(start.exists()).toBe(true)
    // The nickname is required, so fill it before checking the start button.
    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')
    expect(start.attributes('disabled')).toBeUndefined()
  })

  it('starts with no picture at all instead of refusing', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')
    const start = wrapper.find('[data-action="meeting-start"]')

    await wrapper.find('[data-source="__off__"]').trigger('click')
    // No type means no invalid combination: a meeting with neither a screen nor
    // a camera is a normal meeting, and the form does not editorialise about it.
    expect(start.attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[data-role="meeting-picture"]').exists()).toBe(false)
    // The choice moved to the "share nothing" frame, and only that one.
    expect(wrapper.find('[data-source="__off__"]').classes()).toContain('capture-source--on')
    expect(wrapper.find('[data-source="browser:screen"]').classes()).not.toContain('capture-source--on')
  })

  // The card groups already show what is being sent. The only thing invisible
  // there is why the camera group is missing entirely — the combination itself
  // is never narrated back.
  it('says only what the cards cannot, and stays silent otherwise', async () => {
    stubCaptureApi(true)
    videoDevices.list = [{ id: 'cam-a', name: 'FaceTime HD' }]
    const wrapper = mountPanel()
    await flushPromises()

    const picture = () => wrapper.find('[data-role="meeting-picture"]')
    // Screen only — repeating the selection back was noise.
    expect(picture().exists()).toBe(false)

    await card(wrapper, 'video', 'cam-a').trigger('click')
    // Both — the layout used to be narrated here; the cards are enough.
    expect(picture().exists()).toBe(false)

    await wrapper.find('[data-source="__off__"]').trigger('click')
    expect(picture().exists()).toBe(false) // camera only

    await card(wrapper, 'video', '__off__').trigger('click')
    expect(picture().exists()).toBe(false) // neither
  })

  it('keeps the picture choice on one tile at a time', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-source="__off__"]').trigger('click')
    expect(wrapper.findAll('.capture-source--on')).toHaveLength(1)

    await wrapper.find('[data-source="browser:screen"]').trigger('click')
    expect(wrapper.findAll('.capture-source--on')).toHaveLength(1)
    expect(wrapper.find('[data-source="browser:screen"]').classes()).toContain('capture-source--on')
  })

  it('offers every axis as cards, leaving 听记 and the permissions as checkboxes', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    // The old form split "what the host sends" under a heading that only
    // repeated the word sound, and put a checkbox above each device list. The
    // checkboxes are gone: the off state is a card now.
    expect(wrapper.text()).not.toContain(enMessages.meeting.create.sound)
    expect(wrapper.text()).toContain(enMessages.meeting.create.av)
    expect(wrapper.findAll('[data-role="device-choice"]')).toHaveLength(2)
    // 听记, key frames, the two audience permissions and the relay are
    // room-level yes/no questions, not capture sources, so they stay checkboxes.
    expect(wrapper.findAll('.meeting-check input').map((i) => i.attributes('data-role'))).toEqual([
      'meeting-notes',
      'meeting-shots',
      'meeting-allow-talk',
      'meeting-allow-member-screen',
      'meeting-relay',
    ])
  })

  // The endpoint and credential are installation-wide, so they live in
  // 设置 → 视频会议. The create form keeps only the per-meeting switch: three
  // inputs that apply to every meeting must not make each small meeting scroll
  // past them, and editing them here would be a second source of truth.
  it('keeps the relay credentials out of the create form', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('[data-role="meeting-relay"]').exists()).toBe(true)
    expect(wrapper.find('[data-role="meeting-relay-url"]').exists()).toBe(false)
    expect(wrapper.find('[data-role="meeting-relay-key"]').exists()).toBe(false)
    expect(wrapper.find('[data-role="meeting-relay-secret"]').exists()).toBe(false)
    expect(wrapper.find('[data-action="meeting-relay-test"]').exists()).toBe(false)

    // Turning it on without a stored endpoint says where the missing half is
    // edited, instead of leaving the disabled start button unexplained.
    await wrapper.find('[data-role="meeting-relay"]').setValue(true)
    expect(wrapper.find('[data-role="meeting-relay-missing"]').text()).toBe(
      enMessages.meeting.create.relayNotConfigured,
    )
  })

  it('blocks starting a relay meeting until the relay is configured', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')

    const startBtn = () => wrapper.find('[data-action="meeting-start"]')
    expect(startBtn().attributes('disabled')).toBeUndefined()

    await wrapper.find('[data-role="meeting-relay"]').setValue(true)
    // No stored endpoint and nothing tested: creating the room would produce one
    // that cannot carry media, so the button refuses instead.
    expect(startBtn().attributes('disabled')).toBeDefined()
  })

  // The other half of the gate: once 设置 → 视频会议 holds a tested endpoint,
  // the switch is enough and the topology travels with the create request.
  it('starts a relay meeting once the endpoint is configured', async () => {
    stubCaptureApi(true)
    sfuApi.state = { url: 'wss://relay.example.com', hasSecret: true, configured: true }
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')

    await wrapper.find('[data-role="meeting-relay"]').setValue(true)
    expect(wrapper.find('[data-role="meeting-relay-missing"]').exists()).toBe(false)
    const startBtn = wrapper.find('[data-action="meeting-start"]')
    expect(startBtn.attributes('disabled')).toBeUndefined()

    await startBtn.trigger('click')
    await flushPromises()
    expect(shareApi.start).toHaveBeenCalledWith(expect.objectContaining({ useRelay: true }))
  })

  // Both permissions start ON: a meeting where nobody may speak and nobody may
  // share would otherwise need the host to go hunting for two switches
  // mid-call. They are the same room-level switches the 会议管理 tab shows, so
  // they travel with the create request rather than a follow-up frame.
  it('starts with both audience permissions on and sends them at create time', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    const talk = wrapper.find('[data-role="meeting-allow-talk"]').element as HTMLInputElement
    const memberScreen = wrapper.find('[data-role="meeting-allow-member-screen"]')
      .element as HTMLInputElement
    expect(talk.checked).toBe(true)
    expect(memberScreen.checked).toBe(true)

    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')
    await wrapper.find('[data-action="meeting-start"]').trigger('click')
    await flushPromises()

    expect(shareApi.start).toHaveBeenCalledWith(
      expect.objectContaining({ allowTalk: true, allowMemberScreen: true }),
    )
  })

  it('sends a permission the host unticked', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-role="meeting-allow-member-screen"]').setValue(false)
    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')
    await wrapper.find('[data-action="meeting-start"]').trigger('click')
    await flushPromises()

    expect(shareApi.start).toHaveBeenCalledWith(
      expect.objectContaining({ allowTalk: true, allowMemberScreen: false }),
    )
  })

  it('does not repeat a description under each simple choice', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    // Nothing to explain in the default state (screen only): the cards say it.
    expect(wrapper.findAll('[data-role="meeting-picture"]')).toHaveLength(0)
    const text = wrapper.text()
    const dropped = ['cameraOn', 'cameraOff', 'micOn', 'micOff'] as const
    for (const key of dropped) {
      expect(text, `meeting.create.${key} should not be rendered`).not.toContain(
        enMessages.meeting.create[key],
      )
    }
  })

  it('opens with the microphone on, and mutes through the card', async () => {
    stubCaptureApi(true)
    // The choice is a singleton shared with the running stage, so a previous
    // meeting may have left it muted. The form must still open with it ON: the
    // host wants to be heard without having to remember to pick a card.
    useMeetingOptions().micChoice.value = ''

    const wrapper = mountPanel()
    await flushPromises()

    expect(card(wrapper, 'audio', 'default').classes()).toContain('device-choice__card--on')
    // A default, not a lock: muting is one click, before and during.
    await card(wrapper, 'audio', '__off__').trigger('click')
    expect(card(wrapper, 'audio', '__off__').classes()).toContain('device-choice__card--on')

    // ...and turning the picture off does not force it back on.
    await wrapper.find('[data-source="__off__"]').trigger('click')
    await flushPromises()
    expect(card(wrapper, 'audio', '__off__').classes()).toContain('device-choice__card--on')
  })

  it('starts a meeting with nothing ticked and no video flag', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-source="__off__"]').trigger('click')
    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')
    await wrapper.find('[data-action="meeting-start"]').trigger('click')
    await flushPromises()

    const args = (shareApi.start as ReturnType<typeof vi.fn>).mock.calls[0][0]
    expect(args).toMatchObject({ screen: false, cameraDeviceId: '', sourceId: '' })
    // There is no meeting type, so no `video` flag is sent at all.
    expect(args).not.toHaveProperty('video')
  })

  it('sends the camera and screen choices it was given', async () => {
    stubCaptureApi(true)
    videoDevices.list = [{ id: 'cam-a', name: 'FaceTime HD' }]
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')
    await card(wrapper, 'video', 'default').trigger('click')
    await wrapper.find('[data-action="meeting-start"]').trigger('click')
    await flushPromises()

    expect(shareApi.start).toHaveBeenCalledWith(
      expect.objectContaining({ screen: true, cameraDeviceId: 'default' }),
    )
  })

  // The transcript labels a viewer by nickname once the join frame has
  // delivered it, so the stored lines (and the minutes built from them) name
  // people instead of numbering them.
  it('labels viewer channels with the nickname once it is known', async () => {
    stubCaptureApi(true)
    ;(shareApi.viewers as { value: Array<{ id: string; name: string; talk: string }> }).value = [
      { id: 'v1', name: '张三', talk: 'muted' },
      { id: 'v2', name: '  ', talk: 'muted' },
    ]
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')
    await wrapper.find('[data-action="meeting-start"]').trigger('click')
    await flushPromises()

    const args = (shareApi.start as ReturnType<typeof vi.fn>).mock.calls[0][0]
    const label = args.notesLabel as (channel: string) => string
    // The publisher is labelled with their own 称谓, not the pronoun 「我」.
    expect(label('mic')).toBe('Alex')
    expect(label('viewer:v1')).toBe('张三')
    // No nickname yet: the ordinal keeps the line attributable.
    expect(label('viewer:v2')).toBe('Viewer 2')

    // Frozen at start: renaming mid-meeting must not split one speaker in two.
    await wrapper.find('[data-role="meeting-nick"]').setValue('Bob')
    expect(label('mic')).toBe('Alex')
  })

  it('keeps the camera off card when the machine has none', async () => {
    stubCaptureApi(true)
    cameraDevice.has = false
    const wrapper = mountPanel()
    await flushPromises()

    // The group stays on screen: 「不共享摄像头」 is still an answer to "which
    // camera", and a missing group reads as a question the form forgot to ask.
    const camera = wrapper.find('[data-role="device-choice"][data-kind="video"]')
    expect(camera.exists()).toBe(true)
    expect(camera.attributes('data-variant')).toBe('tile')
    // Only the off card. A "system default" for hardware that is not there
    // would be a lie, and there is no list to ask the browser for either.
    expect(camera.findAll('[data-device]').map((c) => c.attributes('data-device'))).toEqual([
      '__off__',
    ])
    expect(camera.text()).toContain(enMessages.meeting.create.cameraOffCard)
    // The single description line carries the reason.
    expect(wrapper.find('[data-role="meeting-picture"]').text()).toBe(
      enMessages.meeting.create.cameraNoDevice,
    )

    // The line keeps saying why the camera is unavailable; the picture tiles
    // are the only way to a picture, and "share no screen" still works (a
    // meeting without any picture is allowed).
    await wrapper.find('[data-source="__off__"]').trigger('click')
    expect(wrapper.find('[data-role="meeting-picture"]').text()).toBe(
      enMessages.meeting.create.cameraNoDevice,
    )
  })

  // A camera is a picture, so it is drawn with the screens' frames; a
  // microphone is not, so it keeps the compact row. One decision, one look.
  it('draws the camera in the same frames as the screens', async () => {
    stubCaptureApi(true)
    videoDevices.list = [{ id: 'cam-a', name: 'FaceTime HD' }]
    const wrapper = mountPanel()
    await flushPromises()

    const camera = wrapper.find('[data-role="device-choice"][data-kind="video"]')
    expect(camera.attributes('data-variant')).toBe('tile')
    expect(camera.findAll('[data-device]').map((c) => c.attributes('data-device'))).toEqual([
      '__off__',
      'default',
      'cam-a',
    ])
    expect(camera.find('.device-choice__thumb').exists()).toBe(true)

    const mic = wrapper.find('[data-role="device-choice"][data-kind="audio"]')
    expect(mic.attributes('data-variant')).toBe('row')
    expect(mic.find('.device-choice__thumb').exists()).toBe(false)
  })

  // A machine with a headset AND a built-in mic has a real choice to make. It
  // is a preference, not a list: the meeting publishes one input.
  it('lists the microphones as one choice, defaulting to the system default', async () => {
    stubCaptureApi(true)
    audioDevices.list = [
      { id: 'mic-a', name: 'Built-in Microphone' },
      { id: 'mic-b', name: 'Headset Microphone' },
    ]
    const wrapper = mountPanel()
    await flushPromises()

    // Mute card + system default + the two real inputs.
    expect(cards(wrapper, 'audio')).toHaveLength(4)
    expect(card(wrapper, 'audio', 'default').text()).toContain(enMessages.meeting.create.deviceDefault)
    expect(card(wrapper, 'audio', 'mic-a').text()).toContain('Built-in Microphone')
    // Nothing pinned yet: the system default is the choice, and it is shown.
    expect(card(wrapper, 'audio', 'default').classes()).toContain('device-choice__card--on')

    await card(wrapper, 'audio', 'mic-b').trigger('click')
    expect(wrapper.findAll('[data-role="device-choice"][data-kind="audio"] .device-choice__card--on')).toHaveLength(1)
    expect(card(wrapper, 'audio', 'mic-b').classes()).toContain('device-choice__card--on')

    // Picking another REPLACES it — the meeting never captures two inputs.
    await card(wrapper, 'audio', 'mic-a').trigger('click')
    expect(wrapper.findAll('[data-role="device-choice"][data-kind="audio"] .device-choice__card--on')).toHaveLength(1)
    expect(card(wrapper, 'audio', 'mic-a').classes()).toContain('device-choice__card--on')
  })

  it('sends the chosen microphone and drops it when the microphone is off', async () => {
    stubCaptureApi(true)
    audioDevices.list = [
      { id: 'mic-a', name: 'Built-in Microphone' },
      { id: 'mic-b', name: 'Headset Microphone' },
    ]
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')
    await card(wrapper, 'audio', 'mic-b').trigger('click')
    await wrapper.find('[data-action="meeting-start"]').trigger('click')
    await flushPromises()
    expect((shareApi.start as ReturnType<typeof vi.fn>).mock.calls[0][0]).toMatchObject({
      microphone: true,
      micDeviceId: 'mic-b',
    })

    ;(shareApi.start as ReturnType<typeof vi.fn>).mockClear()
    await card(wrapper, 'audio', '__off__').trigger('click')
    await wrapper.find('[data-action="meeting-start"]').trigger('click')
    await flushPromises()
    // A device for a microphone that is off would be a constraint nobody asked
    // for — and a failed capture if it is unplugged.
    expect((shareApi.start as ReturnType<typeof vi.fn>).mock.calls[0][0]).toMatchObject({
      microphone: false,
      micDeviceId: '',
    })
  })

  it('still offers the mute and default cards on a one-microphone machine', async () => {
    stubCaptureApi(true)
    audioDevices.list = [{ id: 'mic-a', name: 'Built-in Microphone' }]
    const wrapper = mountPanel()
    await flushPromises()

    // Mute + system default + the one device. Hiding the group would hide the
    // mute control with it.
    expect(cards(wrapper, 'audio')).toHaveLength(3)
  })

  // Before the page has microphone access, enumerateDevices answers with ONE
  // nameless placeholder — indistinguishable from a machine with one mic, which
  // is why the list looked missing on a machine with several.
  it('asks for microphone access instead of pretending there is one device', async () => {
    const stop = vi.fn()
    const gum = vi.fn(async () => ({ getTracks: () => [{ stop }] }))
    vi.stubGlobal('navigator', {
      mediaDevices: { getDisplayMedia: vi.fn(), getUserMedia: gum },
    })
    audioDevices.list = [{ id: '', name: 'Microphone 1' }]
    const wrapper = mountPanel()
    await flushPromises()

    const ask = wrapper.find('[data-role="device-choice"][data-kind="audio"] [data-action="device-list-ask"]')
    expect(ask.exists()).toBe(true)
    expect(ask.text()).toContain(enMessages.meeting.create.listDevices)

    // Granting releases the names; the row turns into the real list.
    audioDevices.list = [
      { id: 'mic-a', name: 'Built-in Microphone' },
      { id: 'mic-b', name: 'Headset Microphone' },
    ]
    await ask.trigger('click')
    await flushPromises()

    expect(gum).toHaveBeenCalledTimes(1)
    // ...and the track is released: this is about permission, not capturing.
    expect(stop).toHaveBeenCalled()
    expect(cards(wrapper, 'audio')).toHaveLength(4)
    // Scoped to this axis: the camera group has its own (still unnamed) list.
    expect(
      wrapper.find('[data-role="device-choice"][data-kind="audio"] [data-action="device-list-ask"]').exists(),
    ).toBe(false)
  })

  it('does not ask again when microphone access was denied', async () => {
    vi.stubGlobal('navigator', {
      mediaDevices: { getDisplayMedia: vi.fn(), getUserMedia: vi.fn() },
      permissions: { query: vi.fn(async () => ({ state: 'denied' })) },
    })
    audioDevices.list = [{ id: '', name: 'Microphone 1' }]
    const wrapper = mountPanel()
    await flushPromises()

    // A button that can only fail is worse than no button.
    expect(
      wrapper.find('[data-role="device-choice"][data-kind="audio"] [data-action="device-list-ask"]').exists(),
    ).toBe(false)
  })

  it('drops the browser pseudo-devices that duplicate the system default', async () => {
    stubCaptureApi(true)
    audioDevices.list = [
      { id: 'default', name: 'Default' },
      { id: 'communications', name: 'Communications' },
      { id: 'mic-a', name: 'Built-in Microphone' },
    ]
    const wrapper = mountPanel()
    await flushPromises()

    // One real input is left, and "system default" appears exactly once (the
    // browser's own `default` entry is dropped, not merged into it).
    expect(cards(wrapper, 'audio')).toHaveLength(3)
    expect(wrapper.findAll('[data-role="device-choice"][data-kind="audio"] [data-device="default"]')).toHaveLength(1)
  })

  // A desktop host that has not handed over a list yet (macOS screen recording
  // not granted, a Wayland portal that has not been asked) gets one empty frame
  // that asks for it, rather than a form that silently has no picture.
  it('asks a native host for its source list instead of showing nothing', async () => {
    stubCaptureApi(true)
    const listSources = vi.fn(async () => '[]')
    nativeApi.value = { listCaptureSources: listSources }
    const wrapper = mountPanel()
    await flushPromises()

    const pick = wrapper.find('[data-source="__pick__"]')
    expect(pick.exists()).toBe(true)
    expect(pick.text()).toContain(enMessages.meeting.create.pickSource)
    // No browser fallback tile: the host said it has a list, it just did not
    // produce one yet.
    expect(wrapper.find('[data-source="browser:screen"]').exists()).toBe(false)
    expect(wrapper.find('[data-role="meeting-sources"]').text()).toContain(
      enMessages.liveShare.noSources,
    )

    const before = (listCaptureSources as unknown as ReturnType<typeof vi.fn>).mock.calls.length
    await pick.trigger('click')
    await flushPromises()
    expect(
      (listCaptureSources as unknown as ReturnType<typeof vi.fn>).mock.calls.length,
    ).toBeGreaterThan(before)
  })

  it('cannot start without a nickname, and says why', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    const start = wrapper.find('[data-action="meeting-start"]')
    expect(start.attributes('disabled')).toBeDefined()

    // A disabled button with no explanation is the bug this pins: the field
    // itself carries the reason, in the error colour.
    const hint = () => wrapper.find('[data-role="meeting-nick-hint"]')
    expect(hint().text()).toBe(enMessages.meeting.create.nickRequired)
    expect(hint().classes()).toContain('meeting-hint--required')
    expect(wrapper.find('[data-role="meeting-nick"]').attributes('aria-invalid')).toBe('true')

    // Nothing narrates a filled-in name: the line is a blocked-state message,
    // not a permanent caption, and the old "what a 称谓 is" / "how to pick one"
    // paragraphs are gone.
    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')
    expect(start.attributes('disabled')).toBeUndefined()
    expect(hint().exists()).toBe(false)
    expect(wrapper.find('[data-role="meeting-nick-advice"]').exists()).toBe(false)

    // Whitespace is not a name.
    await wrapper.find('[data-role="meeting-nick"]').setValue('   ')
    expect(start.attributes('disabled')).toBeDefined()
    expect(hint().text()).toBe(enMessages.meeting.create.nickRequired)
  })

  it('asks for the optional name before the required nickname', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    // The optional field first: the required one then sits directly above the
    // explanation of why the footer button is disabled.
    const title = wrapper.find('[data-role="meeting-title"]').element
    const nick = wrapper.find('[data-role="meeting-nick"]').element
    expect(title.compareDocumentPosition(nick) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('pre-fills the account nickname', async () => {
    stubCaptureApi(true)
    settingsApi.serverConfig.value = { user: { nickname: '泷泽' } }
    const wrapper = mountPanel()
    await flushPromises()

    expect((wrapper.find('[data-role="meeting-nick"]').element as HTMLInputElement).value).toBe('泷泽')
  })

  it('does not pre-fill the fallback literal', async () => {
    stubCaptureApi(true)
    settingsApi.serverConfig.value = { user: { nickname: 'User' } }
    const wrapper = mountPanel()
    await flushPromises()

    expect((wrapper.find('[data-role="meeting-nick"]').element as HTMLInputElement).value).toBe('')
  })

  it('explains an insecure origin instead of blaming the client', async () => {
    stubCaptureApi(false)
    // Neither capture API exists on an insecure origin, camera included — the
    // probe goes through the same `navigator.mediaDevices`.
    cameraDevice.has = false
    const original = Object.getOwnPropertyDescriptor(window, 'isSecureContext')
    Object.defineProperty(window, 'isSecureContext', { value: false, configurable: true })
    try {
      const wrapper = mountPanel()
      await flushPromises()

      // An insecure origin is not a stale client: the page offers the one
      // action that fixes it (generate a certificate and restart), and the
      // generic "update your client" line must not appear at all.
      expect(wrapper.find('.meeting-warn').exists()).toBe(false)
      expect(wrapper.find('.https-notice__title').text()).toBe(enMessages.liveShare.httpsNotice.blockTitle)
      expect(wrapper.find('.https-notice__text').text()).toContain(window.location.origin)
      expect(wrapper.find('[data-action="https-notice-generate"]').exists()).toBe(true)
      // Nothing can be started: the button is present but disabled rather than
      // hidden, so the footer does not jump around between hosts.
      expect(wrapper.find('[data-action="meeting-start"]').attributes('disabled')).toBeDefined()
    } finally {
      if (original) Object.defineProperty(window, 'isSecureContext', original)
      else delete (window as unknown as Record<string, unknown>).isSecureContext
    }
  })

  // The desktop shell promotes its own origin, so this form still works on
  // http — but the link it is about to create will not work for guests. The
  // notice sits above the form (not instead of it) and says exactly that.
  //
  // `isSecureContext` is TRUE here on purpose: that is what the shell reports
  // on an http origin, so keying the banner on it would hide it forever.
  it('warns the desktop shell that guests will need HTTPS', async () => {
    stubCaptureApi(false)
    nativeApi.value = { isDesktopApp: () => true, listCaptureSources: vi.fn(async () => '[]') }
    const original = Object.getOwnPropertyDescriptor(window, 'isSecureContext')
    Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
    try {
      const wrapper = mountPanel()
      await flushPromises()

      const notice = wrapper.find('[data-role="https-notice"]')
      expect(notice.exists()).toBe(true)
      expect(notice.classes()).toContain('https-notice--banner')
      expect(notice.find('.https-notice__title').text()).toBe(
        enMessages.liveShare.httpsNotice.bannerTitle,
      )
      // The form is still there: capture works on this machine.
      expect(wrapper.find('[data-role="meeting-nick"]').exists()).toBe(true)
      expect(wrapper.find('.meeting-warn').exists()).toBe(false)
    } finally {
      if (original) Object.defineProperty(window, 'isSecureContext', original)
      else delete (window as unknown as Record<string, unknown>).isSecureContext
    }
  })

  // An old desktop shell that cannot capture either already gets the full
  // "insecure origin" block below. Stacking the banner on top would repeat the
  // same text and the same button twice on one screen.
  it('does not repeat the HTTPS notice when the desktop shell cannot capture either', async () => {
    stubCaptureApi(false)
    cameraDevice.has = false
    nativeApi.value = { isDesktopApp: () => true }
    const original = Object.getOwnPropertyDescriptor(window, 'isSecureContext')
    Object.defineProperty(window, 'isSecureContext', { value: false, configurable: true })
    try {
      const wrapper = mountPanel()
      await flushPromises()

      // One notice only, and it is the full block (both variants share
      // data-role, so the variant class is what tells them apart).
      expect(wrapper.findAll('[data-role="https-notice"]')).toHaveLength(1)
      expect(wrapper.find('.https-notice--banner').exists()).toBe(false)
      expect(wrapper.find('[data-role="https-notice"]').classes()).toContain('https-notice--block')
      expect(wrapper.find('.https-notice__title').text()).toBe(enMessages.liveShare.httpsNotice.blockTitle)
      expect(wrapper.find('[data-action="https-notice-generate"]').exists()).toBe(true)
    } finally {
      if (original) Object.defineProperty(window, 'isSecureContext', original)
      else delete (window as unknown as Record<string, unknown>).isSecureContext
    }
  })
})
