import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import { _resetMeetingOptions, useMeetingOptions } from '@/composables/useMeetingOptions'
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
  setServerValue: vi.fn(),
}))
vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    serverConfig: settingsApi.serverConfig,
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
    muteAll: vi.fn(),
    setScreenEnabled: vi.fn(),
    revokeScreen: vi.fn(),
    sendControl: vi.fn(),
    sendControlTo: vi.fn(),
    onControl: vi.fn(() => () => undefined),
    switchSource: vi.fn(async () => true),
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
    // a camera is a normal meeting, and the hint says what attendees get.
    expect(start.attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[data-role="meeting-picture"]').text()).toBe(
      enMessages.meeting.create.noPictureHint,
    )
    // The choice moved to the "share nothing" frame, and only that one.
    expect(wrapper.find('[data-source="__off__"]').classes()).toContain('capture-source--on')
    expect(wrapper.find('[data-source="browser:screen"]').classes()).not.toContain('capture-source--on')
  })

  it('describes the camera/screen combination in one line, four states', async () => {
    stubCaptureApi(true)
    videoDevices.list = [{ id: 'cam-a', name: 'FaceTime HD' }]
    const wrapper = mountPanel()
    await flushPromises()

    const picture = () => wrapper.find('[data-role="meeting-picture"]')
    // Screen sharing is on by default (the browser's own picker picks it) and
    // the camera is opt-in, so the form opens on "screen only".
    expect(picture().classes()).toContain('meeting-hint')
    expect(picture().text()).toBe(enMessages.meeting.create.pictureScreen)

    await card(wrapper, 'video', 'cam-a').trigger('click')
    expect(picture().text()).toBe(enMessages.meeting.create.pictureBoth)

    await wrapper.find('[data-source="__off__"]').trigger('click')
    expect(picture().text()).toBe(enMessages.meeting.create.pictureCamera)

    // "Neither" is allowed; the line explains what the audience gets.
    await card(wrapper, 'video', '__off__').trigger('click')
    expect(picture().classes()).toContain('meeting-hint')
    expect(picture().text()).toBe(enMessages.meeting.create.noPictureHint)
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

  it('offers every axis as cards, leaving 听记 as the only checkbox', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    // The old form split "what the host sends" under a heading that only
    // repeated the word sound, and put a checkbox above each device list. The
    // checkboxes are gone: the off state is a card now.
    expect(wrapper.text()).not.toContain(enMessages.meeting.create.sound)
    expect(wrapper.text()).toContain(enMessages.meeting.create.av)
    expect(wrapper.findAll('[data-role="device-choice"]')).toHaveLength(2)
    expect(wrapper.findAll('.meeting-check input').map((i) => i.attributes('data-role'))).toEqual([
      'meeting-notes',
    ])
  })

  it('does not repeat a description under each simple toggle', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    // Only the camera/screen pair needs explaining (their combination decides
    // the picture); the rest name themselves.
    expect(wrapper.findAll('[data-role="meeting-picture"]')).toHaveLength(1)
    const text = wrapper.text()
    const dropped = [
      'cameraOn',
      'cameraOff',
      'micOn',
      'micOff',
      'notesOn',
      'notesOff',
    ] as const
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
    expect(label('mic')).toBe(enMessages.liveShare.notes.speakerMe)
    expect(label('viewer:v1')).toBe('张三')
    // No nickname yet: the ordinal keeps the line attributable.
    expect(label('viewer:v2')).toBe('Viewer 2')
  })

  it('leaves the camera group out when the machine has none', async () => {
    stubCaptureApi(true)
    cameraDevice.has = false
    const wrapper = mountPanel()
    await flushPromises()

    // No cards at all: a list whose only entry is "off" is not a choice.
    expect(wrapper.find('[data-role="device-choice"][data-kind="video"]').exists()).toBe(false)
    // The single description line carries the reason.
    expect(wrapper.find('[data-role="meeting-picture"]').text()).toBe(
      enMessages.meeting.create.cameraNoDevice,
    )

    // The line keeps saying why the camera is unavailable; the picture tiles
    // are the only way to a picture, and "share nothing" still works (a meeting
    // without any picture is allowed).
    await wrapper.find('[data-source="__off__"]').trigger('click')
    expect(wrapper.find('[data-role="meeting-picture"]').text()).toBe(
      enMessages.meeting.create.cameraNoDevice,
    )
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

  it('cannot start without a nickname', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    const start = wrapper.find('[data-action="meeting-start"]')
    expect(start.attributes('disabled')).toBeDefined()

    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')
    expect(start.attributes('disabled')).toBeUndefined()
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

      expect(wrapper.find('.meeting-warn').text()).toBe(enMessages.liveShare.insecureContext)
      // Nothing can be started: the button is present but disabled rather than
      // hidden, so the footer does not jump around between hosts.
      expect(wrapper.find('[data-action="meeting-start"]').attributes('disabled')).toBeDefined()
    } finally {
      if (original) Object.defineProperty(window, 'isSecureContext', original)
      else delete (window as unknown as Record<string, unknown>).isSecureContext
    }
  })
})
