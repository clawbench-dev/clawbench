import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ref } from 'vue'

/**
 * Free-dialog loop tests.
 *
 * The loop's interesting behaviour is all in the transitions, so the harness
 * fakes the three external systems it talks to: the microphone, the streaming
 * recogniser socket and the TTS player. Nothing here waits on a real timer —
 * `advanceTimersByTime` drives every delay, so a passing test means the
 * transition happened, not that the clock happened to line up.
 */

/** One fake recogniser socket, recorded so a test can drive it. */
interface FakeSocket {
  url: string
  readyState: number
  sent: unknown[]
  onopen: (() => void) | null
  onmessage: ((e: { data: string }) => void) | null
  onerror: (() => void) | null
  onclose: (() => void) | null
  send(data: unknown): void
  close(): void
}

let sockets: FakeSocket[] = []
/** One shared toast spy: the real toast is a single slot, so two calls in the
 *  same tick overwrite each other — the count is what proves they do not. */
let toastShow: ReturnType<typeof vi.fn>
/** The screen lock is a real browser API; only its acquisition matters here. */
let wakeAcquire: ReturnType<typeof vi.fn>
let wakeRelease: ReturnType<typeof vi.fn>
/** Set false to simulate a host with neither the Web API nor the native bridge. */
let wakeHeld: { value: boolean }
/** The cues are the only thing this module needs from the sound layer. */
let cuePlayed: ReturnType<typeof vi.fn>
/** The context unlock requested on entry (see `unlockVoiceAudio`). */
let cueUnlocked: ReturnType<typeof vi.fn>

function installFakes() {
  sockets = []
  class FakeWebSocket {
    static OPEN = 1
    static CONNECTING = 0
    static CLOSED = 3
    url: string
    readyState = 1
    sent: unknown[] = []
    onopen: (() => void) | null = null
    onmessage: ((e: { data: string }) => void) | null = null
    onerror: (() => void) | null = null
    onclose: (() => void) | null = null
    constructor(url: string) {
      this.url = url
      sockets.push(this as unknown as FakeSocket)
      // Deliver `open` asynchronously, like the real thing.
      queueMicrotask(() => this.onopen?.())
    }
    send(data: unknown) { this.sent.push(data) }
    close() {
      this.readyState = 3
      queueMicrotask(() => this.onclose?.())
    }
  }
  vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket)
  // WebSocket.OPEN is read as a static in the code under test.
  ;(globalThis.WebSocket as unknown as { OPEN: number }).OPEN = 1

  vi.stubGlobal('navigator', {
    mediaDevices: {
      getUserMedia: vi.fn().mockResolvedValue({
        getTracks: () => [{ stop: vi.fn() }],
        getAudioTracks: () => [{ readyState: 'live', stop: vi.fn(), addEventListener: vi.fn() }],
      }),
    },
  })

  // The capture graph is not what is under test here; the loop only needs the
  // frames it hands over. `PcmStreamer` is mocked so no AudioContext is needed.
  vi.doMock('@/utils/pcmCapture', () => ({
    PCM_SAMPLE_RATE: 16000,
    PcmStreamer: class {
      stopped = false
      constructor(private readonly opts: { onFrame: (f: ArrayBuffer) => void; onLevel?: (rms: number) => void }) {}
      start() { (globalThis as Record<string, unknown>).__streamer = this; return Promise.resolve() }
      stop() { this.stopped = true }
      resetGate() { /* the real gate state is not under test here */ }
      emit(rms: number) { this.opts.onLevel?.(rms) }
      frame() { this.opts.onFrame(new ArrayBuffer(4)) }
    },
  }))

  // Auto-speech: the loop only reads `enabled`/`state`/`isBusy` and calls
  // `stopAudio`. A controllable stand-in keeps the test about the loop.
  // The factory is async and pulls Vue through importActual: a `require('vue')`
  // inside the factory resolves the CJS build, which is a DIFFERENT reactive
  // system — watchers registered from the ESM build would never see the change
  // and the transition tests would pass on a broken implementation.
  vi.doMock('@/composables/useAutoSpeech', async () => {
    const { ref } = await vi.importActual<typeof import('vue')>('vue')
    const enabled = ref(true)
    const state = ref<'idle' | 'summarizing' | 'synthesizing' | 'playing'>('idle')
    const speakingText = ref('')
    const isBusy = () => state.value !== 'idle'
    // ONE object for every call: the loop and the test must see the same
    // `stopAudio` spy, otherwise "was it called" is unanswerable.
    const api = {
      enabled,
      state,
      speakingText,
      isBusy,
      toggle: vi.fn(() => { enabled.value = !enabled.value }),
      setEnabled: vi.fn((next: boolean) => { enabled.value = next }),
      stopAudio: vi.fn(() => { state.value = 'idle' }),
      speakMessage: vi.fn(),
    }
    return { useAutoSpeech: () => api }
  })

  // NESTED, exactly like `GET /api/config` (`configResponse.STT` is
  // `json:"stt"`). A flat fixture here would let a flat reader pass while the
  // real payload leaves every setting on its default.
  vi.doMock('@/composables/useSettingsConfig', async () => {
    const { ref } = await vi.importActual<typeof import('vue')>('vue')
    return {
      serverConfig: ref({
        stt: {
          voice_gate: true,
          voice_gate_sensitivity: 'standard',
          trigger_words: '发送',
          silence_send_seconds: 3,
          barge_in: 'any',
          barge_in_words: '等等',
          mic_echo_cancel: true,
        },
      }),
    }
  })

  toastShow = vi.fn()
  vi.doMock('@/composables/useToast', () => ({
    useToast: () => ({ show: toastShow }),
  }))

  const held = ref(false)
  wakeAcquire = vi.fn(() => { held.value = true })
  wakeRelease = vi.fn(() => { held.value = false })
  wakeHeld = held
  const api = { held, acquire: wakeAcquire, release: wakeRelease }
  vi.doMock('@/composables/useWakeLock', () => ({ useWakeLock: () => api }))

  // Cues are synthesized audio; only the decision to play one is under test.
  cuePlayed = vi.fn()
  cueUnlocked = vi.fn()
  vi.doMock('@/composables/useNotificationSound', () => ({
    playVoiceModeCue: cuePlayed,
    unlockVoiceAudio: cueUnlocked,
    playNotificationSound: vi.fn(),
  }))
}

/** Pushes one recogniser message into the newest socket. */
function speak(text: string, kind: 'partial' | 'text' | 'done' = 'done') {
  const sock = sockets[sockets.length - 1]
  if (!sock) throw new Error('no socket')
  const payload = kind === 'done' ? { type: 'done', final: text } : { type: kind, text }
  sock.onmessage?.({ data: JSON.stringify(payload) })
}

/** How many `{"type":"end"}` control frames a socket has been given. */
function endFrames(sock: FakeSocket): number {
  return sock.sent.filter((m) => String(m).includes('"type":"end"')).length
}

async function flush() {
  await Promise.resolve()
  await Promise.resolve()
  await Promise.resolve()
}

describe('useFreeDialog', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.useFakeTimers()
    installFakes()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.doUnmock('@/utils/pcmCapture')
    vi.doUnmock('@/composables/useAutoSpeech')
    vi.doUnmock('@/composables/useSettingsConfig')
    vi.doUnmock('@/composables/useToast')
    vi.doUnmock('@/composables/useWakeLock')
    vi.doUnmock('@/composables/useNotificationSound')
  })

  it('keeps the screen awake for the whole time the mode is on', async () => {
    // The loop lives in the page: the moment the screen turns itself off the
    // app is in the background and Android takes the microphone away, so the
    // mode stops listening without saying anything. A user thinking out loud
    // never touches the screen, which is exactly when the system timeout hits.
    const mod = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    expect(wakeAcquire).toHaveBeenCalledWith('free-dialog')

    // The lock must survive the whole cycle, not just the two endpoints: a
    // release slipped into a phase transition would otherwise go unnoticed.
    wakeRelease.mockClear()
    speak('做点事 发送')
    mod.setFreeDialogThinking(false)
    autoSpeech.state.value = 'playing'
    await flush()
    autoSpeech.state.value = 'idle'
    await flush()
    vi.advanceTimersByTime(500)
    await flush()
    expect(mod.useFreeDialog().phase.value).toBe('listening')
    expect(wakeRelease).not.toHaveBeenCalled()

    mod.stop()
    expect(wakeRelease).toHaveBeenCalledWith('free-dialog')
  })

  it('keeps the screen awake when it starts while a turn is already running', async () => {
    // That path returns before `openListening`; a lock taken at the end of the
    // function would leave this variant without one.
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn(), busy: true })
    await flush()
    expect(wakeAcquire).toHaveBeenCalledWith('free-dialog')
    mod.stop()
    expect(wakeRelease).toHaveBeenCalledWith('free-dialog')
  })

  it('releases the screen lock when the start fails', async () => {
    // A denied microphone ends the mode through stop(); leaving the lock held
    // would keep the display on for a mode that is not running.
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: vi.fn().mockRejectedValue(new Error('denied')) },
    })
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    expect(mod.useFreeDialog().phase.value).toBe('idle')
    // It must actually have taken the lock first: without this the test also
    // passes when the lock is never acquired at all (release is a no-op then).
    expect(wakeAcquire).toHaveBeenCalledWith('free-dialog')
    expect(wakeRelease).toHaveBeenCalledWith('free-dialog')
    // The LAST release is the one from the failure path; the first belongs to
    // startFreeDialog's own reset, which runs before the lock is taken.
    expect(wakeAcquire.mock.invocationCallOrder[0])
      .toBeLessThan(wakeRelease.mock.invocationCallOrder.at(-1)!)
  })

  it('warns when this host cannot keep the screen awake', async () => {
    // Plain http on a phone: no `navigator.wakeLock` (secure-context only) and
    // no native bridge. Without a word the user believes the screen is pinned
    // on and watches the mode die at the system timeout.
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    wakeHeld.value = false
    toastShow.mockClear()
    vi.advanceTimersByTime(2600)
    expect(toastShow).toHaveBeenCalledTimes(1)
    expect(String(toastShow.mock.calls[0][0])).toMatch(/锁屏|screen/i)
  })

  it('stays quiet when the screen lock is actually held', async () => {
    // The warning must be a report of a real failure, not a timer that always
    // fires — otherwise every start would show two toasts.
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    toastShow.mockClear()
    vi.advanceTimersByTime(2600)
    expect(toastShow).not.toHaveBeenCalled()
  })

  it('stops when the microphone is taken away', async () => {
    // The OS can revoke the device mid-session (a call, another recorder, a
    // Bluetooth disconnect). The track ends, nothing is captured, but the
    // socket and the 'listening' phase would keep pretending otherwise.
    const track = { stop: vi.fn(), addEventListener: vi.fn() }
    vi.stubGlobal('navigator', {
      mediaDevices: {
        getUserMedia: vi.fn().mockResolvedValue({
          getTracks: () => [track],
          getAudioTracks: () => [track],
        }),
      },
    })
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    expect(mod.useFreeDialog().phase.value).toBe('listening')

    const onEnded = track.addEventListener.mock.calls.find(([ev]) => ev === 'ended')?.[1] as (() => void) | undefined
    expect(onEnded).toBeTypeOf('function')
    toastShow.mockClear()
    onEnded!()

    expect(mod.useFreeDialog().phase.value).toBe('idle')
    expect(toastShow).toHaveBeenCalledTimes(1)
    expect(String(toastShow.mock.calls[0][0])).toMatch(/麦克风|microphone/i)
  })

  it('turns read-aloud on when the mode starts', async () => {
    // The loop waits for playback before reopening the microphone; starting
    // with read-aloud off leaves a mode that sends and listens with no voice.
    const { startFreeDialog } = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    autoSpeech.enabled.value = false
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    expect(autoSpeech.enabled.value).toBe(true)
  })

  it('restores read-aloud to the state it found on exit', async () => {
    // The mode turns read-aloud on because the loop is built around the reply
    // being spoken, but that is a change the user did not ask for: leaving it
    // on means their "read-aloud off" setting silently became "on".
    const mod = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    autoSpeech.enabled.value = false
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    expect(autoSpeech.enabled.value).toBe(true)
    mod.stop()
    expect(autoSpeech.enabled.value).toBe(false)
  })

  it('leaves read-aloud alone when it was already on', async () => {
    // Nothing was changed on entry, so nothing may be changed on exit.
    const mod = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    autoSpeech.enabled.value = true
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    mod.stop()
    expect(autoSpeech.enabled.value).toBe(true)
  })

  it('does not switch read-aloud back on when the user turned it off mid-dialog', async () => {
    // They changed it themselves; undoing that on exit would be the mode
    // overriding a live choice.
    const mod = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    autoSpeech.enabled.value = false
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    autoSpeech.setEnabled(false)
    ;(autoSpeech.setEnabled as ReturnType<typeof vi.fn>).mockClear()
    mod.stop()
    expect((autoSpeech.setEnabled as ReturnType<typeof vi.fn>)).not.toHaveBeenCalled()
    expect(autoSpeech.enabled.value).toBe(false)
  })

  it('waits for an interrupt instead of listening when a turn is already running', async () => {
    // The user is still waiting for this answer. Collecting their words as the
    // NEXT message would queue a second turn behind the one they are watching.
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn(), busy: true, onInterrupt: vi.fn() })
    await flush()
    expect(mod.useFreeDialog().phase.value).toBe('speaking')
    // One socket — the interrupt listener. A dictation socket would mean speech
    // is being turned into a message.
    expect(sockets.length).toBe(1)
  })

  it('cancels the running turn when the user interrupts it', async () => {
    const onInterrupt = vi.fn()
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn(), busy: true, onInterrupt })
    await flush()
    speak('等等', 'partial')
    // Nothing is being spoken yet, so muting would do nothing: the turn itself
    // has to be cancelled.
    expect(onInterrupt).toHaveBeenCalledTimes(1)
  })

  it('mutes playback instead of cancelling once the reply is being read', async () => {
    const onInterrupt = vi.fn()
    const mod = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn(), busy: true, onInterrupt })
    await flush()
    autoSpeech.state.value = 'playing'
    await flush()
    speak('等等', 'partial')
    expect(autoSpeech.stopAudio).toHaveBeenCalled()
    expect(onInterrupt).not.toHaveBeenCalled()
  })

  it('starts listening once the running turn ends', async () => {
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn(), busy: true })
    await flush()
    expect(mod.useFreeDialog().phase.value).toBe('speaking')
    mod.setFreeDialogThinking(false)
    // Same grace window as a turn that started in this mode: playback may be
    // about to begin, so arming dictation on the turn-end edge would transcribe
    // the reply. The flush matters — openListening awaits the capture, so
    // without it the phase reads 'speaking' whatever the implementation does.
    vi.advanceTimersByTime(600)
    await flush()
    expect(mod.useFreeDialog().phase.value).not.toBe('listening')
    vi.advanceTimersByTime(250)
    await flush()
    expect(mod.useFreeDialog().phase.value).toBe('listening')
  })

  // ── interrupting the generation, not only the read-aloud ──
  //
  // The reply is produced long before it is spoken, and that is exactly when
  // "等等，不是这个意思" needs to land.

  it('arms the interrupt listener while the reply is still being generated', async () => {
    const onInterrupt = vi.fn()
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn(), onInterrupt })
    await flush()
    speak('做点事 发送')
    expect(mod.useFreeDialog().phase.value).toBe('thinking')
    vi.advanceTimersByTime(500)
    await flush()
    // Two sockets: the dictation one (closed when the turn was sent) and the
    // interrupt one armed for the generation window.
    expect(sockets.length).toBe(2)
    speak('等等', 'partial')
    // Nothing is playing yet, so the turn itself is what has to be cancelled.
    expect(onInterrupt).toHaveBeenCalledTimes(1)
  })

  it('does not arm the listener while the "took it" cue is still ringing', async () => {
    // Under the default "any speech" setting the cue would be transcribed as
    // the user talking, cancelling the turn that was just sent.
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn(), onInterrupt: vi.fn() })
    await flush()
    speak('做点事 发送')
    vi.advanceTimersByTime(300)
    await flush()
    expect(sockets.length).toBe(1)
  })

  it('opens no generation-phase listener when interrupting is switched off', async () => {
    vi.doMock('@/composables/useSettingsConfig', async () => {
      const { ref } = await vi.importActual<typeof import('vue')>('vue')
      return { serverConfig: ref({ stt: { barge_in: 'off', trigger_words: '发送', silence_send_seconds: 3 } }) }
    })
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn(), onInterrupt: vi.fn() })
    await flush()
    speak('做点事 发送')
    vi.advanceTimersByTime(5000)
    await flush()
    expect(sockets.length).toBe(1)
    expect(mod.useFreeDialog().phase.value).toBe('thinking')
  })

  it('does not arm a listener for a turn that has already ended', async () => {
    // The reply can finish inside the arming delay (an instant failure, a
    // cancel). Opening a recogniser then would transcribe whatever plays next
    // as the user's speech.
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn(), onInterrupt: vi.fn() })
    await flush()
    speak('做点事 发送')
    mod.setFreeDialogThinking(false)
    // Still inside the settle window, so the phase is still 'thinking' when
    // the arming delay elapses: nothing may be opened at that point.
    vi.advanceTimersByTime(400)
    await flush()
    expect(sockets.length).toBe(1)
  })

  it('gives the read-aloud a fresh listener instead of reusing the generation one', async () => {
    // Partials are cumulative within a recogniser session: carrying the
    // generation-phase transcript over would let an already-heard phrase fire
    // again the moment playback starts.
    const mod = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn(), onInterrupt: vi.fn() })
    await flush()
    speak('做点事 发送')
    vi.advanceTimersByTime(500)
    await flush()
    const generation = sockets[sockets.length - 1]
    mod.setFreeDialogThinking(false)
    autoSpeech.state.value = 'playing'
    await flush()
    await flush()
    expect(generation.readyState).toBe(3)
    expect(sockets.length).toBe(3)
  })

  it('does not read out a reply the user just cancelled', async () => {
    // The truncated answer was never handed to TTS, so the panel's
    // "already spoken" set knows nothing about it. Without this the loop would
    // read the half-answer out right after the user asked it to stop.
    const onSpeakPendingReply = vi.fn(() => true)
    const onInterrupt = vi.fn()
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn(), onInterrupt, onSpeakPendingReply })
    await flush()
    speak('做点事 发送')
    vi.advanceTimersByTime(500)
    await flush()
    speak('等等', 'partial')
    expect(onInterrupt).toHaveBeenCalledTimes(1)
    // The owner cancels the turn; `loading` goes false.
    mod.setFreeDialogThinking(false)
    vi.advanceTimersByTime(600)
    await flush()
    expect(onSpeakPendingReply).not.toHaveBeenCalled()
    vi.advanceTimersByTime(250)
    await flush()
    expect(mod.useFreeDialog().phase.value).toBe('listening')
  })

  it('reports whether speaking can interrupt, for the status strip', async () => {
    // The strip says "AI 正在思考…" unless the user can cut in; a hint nobody
    // can act on would be worse than none.
    const mod = await import('../useFreeDialog')
    expect(mod.useFreeDialog().canInterrupt.value).toBe(false)
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    expect(mod.useFreeDialog().canInterrupt.value).toBe(true)
    mod.stop()
    expect(mod.useFreeDialog().canInterrupt.value).toBe(false)
  })

  it('reports no interruption when the setting is off', async () => {
    vi.doMock('@/composables/useSettingsConfig', async () => {
      const { ref } = await vi.importActual<typeof import('vue')>('vue')
      return { serverConfig: ref({ stt: { barge_in: 'off', trigger_words: '发送', silence_send_seconds: 3 } }) }
    })
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    expect(mod.useFreeDialog().canInterrupt.value).toBe(false)
  })

  it('releases the interrupt microphone before listening starts', async () => {
    // The interrupt recogniser must be gone before dictation starts, or two
    // sockets each apply a transcript to the same utterance.
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn(), busy: true, onInterrupt: vi.fn() })
    await flush()
    speak('等等', 'partial')
    mod.setFreeDialogThinking(false)
    vi.advanceTimersByTime(600)
    vi.advanceTimersByTime(250)
    await flush()
    expect(mod.useFreeDialog().phase.value).toBe('listening')
    // Two sockets: the interrupt one (now closed) and the dictation one.
    expect(sockets.length).toBe(2)
    expect(sockets[0].readyState).toBe(3)
  })

  it('waits for the running turn when an earlier reply finishes playing', async () => {
    // The mode can be opened while a turn is running AND a previous reply is
    // still being read out. When that playback ends the turn is still going:
    // reopening the microphone would queue the user's next words behind the
    // answer they are waiting for.
    const mod = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn(), busy: true, onInterrupt: vi.fn() })
    await flush()
    autoSpeech.state.value = 'playing'
    await flush()
    autoSpeech.state.value = 'idle'
    await flush()
    vi.advanceTimersByTime(5000)
    await flush()
    expect(mod.useFreeDialog().phase.value).toBe('thinking')
  })

  it('closes a microphone that arrives after the mode was stopped', async () => {
    // getUserMedia can resolve after the user closed the mode. stopCapture()
    // ran before the stream existed, so this is the only place that can close
    // it — otherwise the microphone indicator stays on with nobody reading it.
    const track = { stop: vi.fn() }
    let resolveMic: (s: unknown) => void = () => {}
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: vi.fn(() => new Promise((r) => { resolveMic = r })) },
    })
    const mod = await import('../useFreeDialog')
    const started = mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    mod.stop()
    resolveMic({ getTracks: () => [track], getAudioTracks: () => [] })
    await started
    expect(track.stop).toHaveBeenCalled()
    expect(mod.useFreeDialog().phase.value).toBe('idle')
  })

  it('opens one microphone when two phase changes overlap', async () => {
    // Entering during a running turn opens the interrupt listener, and playback
    // starting asks for a listener again. Both want the microphone; acquiring
    // it twice would leave a second track running that nothing can stop, and
    // the device churn at playback start is what swallowed the first syllables
    // of the reply on Android.
    const pending: Array<(s: unknown) => void> = []
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: vi.fn(() => new Promise((r) => { pending.push(r) })) },
    })
    const trackA = { stop: vi.fn() }
    const mod = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn(), busy: true, onInterrupt: vi.fn() })
    await flush()
    expect(pending.length).toBe(1)
    autoSpeech.state.value = 'playing'
    await flush()
    // The second listener shares the acquisition already in flight.
    expect(pending.length).toBe(1)

    pending[0]({ getTracks: () => [trackA], getAudioTracks: () => [trackA] })
    await flush()

    expect(trackA.stop).not.toHaveBeenCalled()
    // The live one is still reachable and is what stop() closes.
    mod.stop()
    expect(trackA.stop).toHaveBeenCalled()
  })

  it('just waits when interruption is turned off', async () => {
    // "Never interrupt" means no microphone at all until the turn is over —
    // opening one to listen for an interrupt the user disabled would be wrong.
    const settings = await import('@/composables/useSettingsConfig')
    settings.serverConfig.value = {
      stt: {
        voice_gate: true,
        voice_gate_sensitivity: 'standard',
        trigger_words: '发送',
        silence_send_seconds: 3,
        barge_in: 'off',
        barge_in_words: '等等',
        mic_echo_cancel: true,
      },
    }
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn(), busy: true })
    await flush()
    expect(mod.useFreeDialog().phase.value).toBe('thinking')
    expect(sockets.length).toBe(0)
  })

  it('shows ONE toast on entry, not one per switch it flipped', async () => {
    // The toast slot holds a single message: showing "read-aloud on" and then
    // "dialog on" means the first is visible for a few milliseconds.
    const mod = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    autoSpeech.enabled.value = false
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    expect(toastShow).toHaveBeenCalledTimes(1)
    // And read-aloud was flipped without its own toast.
    expect(autoSpeech.setEnabled).toHaveBeenCalledWith(true, { silent: true })
  })

  it('shows one toast on exit and says read-aloud was restored', async () => {
    const mod = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    autoSpeech.enabled.value = false
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    toastShow.mockClear()
    mod.stop()
    expect(toastShow).toHaveBeenCalledTimes(1)
    expect(String(toastShow.mock.calls[0][0])).toMatch(/朗读|read-aloud/i)
  })

  it('stays quiet when a failure has already explained itself', async () => {
    // A microphone error shows its own toast; a "dialog closed" message right
    // after it would replace the reason the user needs to read.
    const mod = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    autoSpeech.enabled.value = false
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    toastShow.mockClear()
    mod.stop({ notify: false })
    expect(toastShow).not.toHaveBeenCalled()
  })

  it('starts idle', async () => {
    const { useFreeDialog } = await import('../useFreeDialog')
    expect(useFreeDialog().phase.value).toBe('idle')
  })

  it('enters listening and opens a recogniser socket', async () => {
    const { startFreeDialog, useFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    expect(useFreeDialog().phase.value).toBe('listening')
    expect(sockets.length).toBe(1)
    // The start control frame carries the PCM format, as the server requires.
    expect(sockets[0].sent[0]).toContain('"type":"start"')
  })

  it('sends the message when a trigger word is recognised', async () => {
    const onSend = vi.fn()
    const { startFreeDialog, useFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend })
    await flush()
    speak('帮我看下这个报错 发送')
    expect(onSend).toHaveBeenCalledWith('帮我看下这个报错')
    // The microphone is released before the turn starts: the reply will be read
    // aloud, and an open mic would hear it.
    expect(useFreeDialog().phase.value).toBe('thinking')
  })

  it('does not send a bare trigger word (nothing to send)', async () => {
    const onSend = vi.fn()
    const { startFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend })
    await flush()
    speak('发送')
    expect(onSend).not.toHaveBeenCalled()
  })

  it('sends the finished sentence, not the half-revised partial', async () => {
    // The bug this guards: the microphone gate stops feeding audio 600 ms after
    // the last loud frame while a streaming engine needs ~800 ms of silence to
    // settle a sentence — so the partial in hand is missing its last few
    // characters, and closing the socket there (what this used to do) lost them
    // for good. `end` makes the engine flush and answer with the finished
    // sentence, which is what gets sent.
    const onSend = vi.fn()
    const { startFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend })
    await flush()
    const sock = sockets[0]
    // Recognition confirms a sentence, no trigger word in it.
    speak('这个文件改坏了', 'text')
    expect(onSend).not.toHaveBeenCalled()
    vi.advanceTimersByTime(3000)
    // The pause fired: the engine is asked to finish the sentence instead of
    // the revision being sent as-is.
    expect(onSend).not.toHaveBeenCalled()
    expect(endFrames(sock)).toBe(1)
    speak('这个文件改坏了，你看看', 'done')
    expect(onSend).toHaveBeenCalledWith('这个文件改坏了，你看看')
  })

  it('sends the text in hand when the engine never answers', async () => {
    // A hung engine must cost a beat, not the whole turn.
    const onSend = vi.fn()
    const { startFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend })
    await flush()
    speak('这个文件改坏了', 'text')
    vi.advanceTimersByTime(3000)
    expect(onSend).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1000)
    expect(onSend).toHaveBeenCalledWith('这个文件改坏了')
  })

  it('asks the engine for the finished sentence only once', async () => {
    // Revisions keep arriving while the engine chews on the tail. A second
    // `end` (and a second send) would duplicate the turn.
    const onSend = vi.fn()
    const { startFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend })
    await flush()
    const sock = sockets[0]
    speak('帮我看下这个报错 发送', 'partial')
    speak('帮我看下这个报错 发送', 'partial')
    speak('帮我看下这个报错 发送', 'partial')
    expect(endFrames(sock)).toBe(1)
    // The answer still ends with the trigger word; it must not fire again.
    speak('帮我看下这个报错 发送', 'done')
    expect(endFrames(sock)).toBe(1)
    expect(onSend).toHaveBeenCalledTimes(1)
    expect(onSend).toHaveBeenCalledWith('帮我看下这个报错')
  })

  it('keeps the turn when the engine closes right after answering "end"', async () => {
    // The relay finishes the session on `end` and closes the connection. That
    // close is the normal end of a finalize, not a reason to leave the mode.
    const onSend = vi.fn()
    const { startFreeDialog, useFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend })
    await flush()
    const sock = sockets[0]
    speak('这个文件改坏了', 'text')
    vi.advanceTimersByTime(3000)
    sock.onclose?.()
    expect(useFreeDialog().phase.value).toBe('listening')
    vi.advanceTimersByTime(1000)
    expect(onSend).toHaveBeenCalledWith('这个文件改坏了')
  })

  it('does not send a sentence the user abandoned by closing the mode', async () => {
    const onSend = vi.fn()
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend })
    await flush()
    speak('这个文件改坏了', 'text')
    vi.advanceTimersByTime(3000)
    mod.stop()
    vi.advanceTimersByTime(2000)
    expect(onSend).not.toHaveBeenCalled()
  })

  it('restarts the pause timer when more words are recognised', async () => {
    const onSend = vi.fn()
    const { startFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend })
    await flush()
    speak('第一句话', 'partial')
    vi.advanceTimersByTime(2000)
    // More words arrived: the user is still talking, so the pause starts over —
    // otherwise a slow speaker gets cut off mid-sentence.
    speak('第一句话，还有', 'partial')
    vi.advanceTimersByTime(2000)
    expect(onSend).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1000)
    // The pause fired; the engine then answers with the finished sentence.
    speak('第一句话，还有', 'done')
    expect(onSend).toHaveBeenCalledWith('第一句话，还有')
  })

  it('ignores room noise: the pause is measured from recognised words', async () => {
    // The bug this guards. The timer used to restart on every microphone frame
    // whose level was above zero, and a microphone in a normal room never goes
    // quiet — a fan, a keyboard, traffic — so the pause never fired and nothing
    // was ever auto-sent.
    const onSend = vi.fn()
    const { startFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend })
    await flush()
    speak('这个文件改坏了', 'text')
    const streamer = (globalThis as Record<string, { emit: (n: number) => void }>).__streamer
    for (let i = 0; i < 20; i++) {
      vi.advanceTimersByTime(200)
      streamer.emit(0.02)
    }
    // The pause fired during the loop; the engine answers with the sentence.
    speak('这个文件改坏了', 'done')
    expect(onSend).toHaveBeenCalledWith('这个文件改坏了')
  })

  it('does not push the deadline out when the engine repeats a partial', async () => {
    // Streaming engines re-send the same cumulative transcript while they chew
    // on audio. Re-arming on every message would mean the timer only ever fires
    // after the engine goes completely silent.
    const onSend = vi.fn()
    const { startFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend })
    await flush()
    speak('第一句话', 'partial')
    vi.advanceTimersByTime(2000)
    speak('第一句话', 'partial')
    vi.advanceTimersByTime(1000)
    speak('第一句话', 'done')
    expect(onSend).toHaveBeenCalledWith('第一句话')
  })

  it('leaves the mode when nothing is recognised for the configured span', async () => {
    // The failure this prevents: the user walks away with the mode on, the
    // microphone stays open (and the recogniser keeps billing) forever.
    const settings = await import('@/composables/useSettingsConfig')
    settings.serverConfig.value = { stt: { silence_send_seconds: 0, idle_exit_seconds: 5 } }
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    expect(mod.useFreeDialog().phase.value).toBe('listening')
    vi.advanceTimersByTime(5000)
    await flush()
    expect(mod.useFreeDialog().phase.value).toBe('idle')
  })

  it('pushes the idle exit back while words keep arriving', async () => {
    const settings = await import('@/composables/useSettingsConfig')
    settings.serverConfig.value = { stt: { silence_send_seconds: 0, idle_exit_seconds: 5 } }
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    vi.advanceTimersByTime(4000)
    speak('还在说', 'partial')
    vi.advanceTimersByTime(4000)
    // 4s since the last word: still inside the window.
    expect(mod.useFreeDialog().phase.value).toBe('listening')
    vi.advanceTimersByTime(1500)
    expect(mod.useFreeDialog().phase.value).toBe('idle')
  })

  it('never leaves on its own when the idle exit is set to 0', async () => {
    const settings = await import('@/composables/useSettingsConfig')
    settings.serverConfig.value = { stt: { silence_send_seconds: 0, idle_exit_seconds: 0 } }
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    vi.advanceTimersByTime(10 * 60 * 1000)
    await flush()
    expect(mod.useFreeDialog().phase.value).toBe('listening')
  })

  it('does not send on silence alone (no speech was ever recognised)', async () => {
    const onSend = vi.fn()
    const { startFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend })
    await flush()
    vi.advanceTimersByTime(30000)
    expect(onSend).not.toHaveBeenCalled()
  })

  it('resumes listening after a turn when auto-speech is off', async () => {
    const { startFreeDialog, useFreeDialog, setFreeDialogThinking } = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    autoSpeech.enabled.value = false
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    speak('做点事 发送')
    expect(useFreeDialog().phase.value).toBe('thinking')
    setFreeDialogThinking(false)
    // The turn end opens a grace window (the TTS POST is async, so "is the
    // reply going to be read aloud?" is not answerable yet), then the reopen
    // delay.
    vi.advanceTimersByTime(1500)
    vi.advanceTimersByTime(500)
    await flush()
    expect(useFreeDialog().phase.value).toBe('listening')
  })

  it('waits for playback before resuming listening', async () => {
    const { startFreeDialog, useFreeDialog, setFreeDialogThinking } = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    speak('做点事 发送')
    // The turn finished and playback started: the microphone must stay closed
    // until the audio ends.
    setFreeDialogThinking(false)
    autoSpeech.state.value = 'playing'
    await flush()
    expect(useFreeDialog().phase.value).toBe('speaking')
    vi.advanceTimersByTime(10000)
    expect(useFreeDialog().phase.value).toBe('speaking')
    // Playback ends → reopen after the tail-settling delay.
    autoSpeech.state.value = 'idle'
    await flush()
    vi.advanceTimersByTime(500)
    await flush()
    expect(useFreeDialog().phase.value).toBe('listening')
  })

  it('keeps the same microphone across a whole turn', async () => {
    // One capture for the session. Re-acquiring it at the moment playback
    // begins changes the audio route on Android and swallows the first
    // syllables of the reply; the same acquisition is also what made "listen
    // again once it stops talking" feel like dead air.
    const { startFreeDialog, useFreeDialog, setFreeDialogThinking } = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    const getUserMedia = navigator.mediaDevices.getUserMedia as ReturnType<typeof vi.fn>
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    const streamer = (globalThis as Record<string, { stopped: boolean }>).__streamer
    speak('做点事 发送')
    setFreeDialogThinking(false)
    autoSpeech.state.value = 'playing'
    await flush()
    autoSpeech.state.value = 'idle'
    await flush()
    vi.advanceTimersByTime(250)
    await flush()
    expect(useFreeDialog().phase.value).toBe('listening')
    expect(getUserMedia).toHaveBeenCalledTimes(1)
    expect(streamer.stopped).toBe(false)
  })

  it('does not reopen the microphone before the settling delay elapses', async () => {
    const { startFreeDialog, useFreeDialog, setFreeDialogThinking } = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    speak('做点事 发送')
    setFreeDialogThinking(false)
    autoSpeech.state.value = 'playing'
    await flush()
    autoSpeech.state.value = 'idle'
    await flush()
    // 200 ms in: still settling. Reopening here captures the speaker's tail.
    // The flush matters: `openListening` awaits the capture, so without it a
    // zero-delay implementation would still read as 'speaking' and the test
    // would pass on either behaviour.
    vi.advanceTimersByTime(200)
    await flush()
    expect(useFreeDialog().phase.value).toBe('speaking')
    vi.advanceTimersByTime(100)
    await flush()
    expect(useFreeDialog().phase.value).toBe('listening')
  })

  it('ignores the assistant\'s own voice while listening for a barge-in', async () => {
    const { startFreeDialog, useFreeDialog, setFreeDialogThinking } = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    autoSpeech.speakingText.value = '好的我来帮你检查一下这个配置文件里的端口设置'
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    speak('做点事 发送')
    setFreeDialogThinking(false)
    autoSpeech.state.value = 'playing'
    await flush()
    await flush()
    // The barge listener is a second socket opened for the playback window.
    const barge = sockets[sockets.length - 1]
    // What the microphone hears through the speakers: the assistant's own words.
    barge.onmessage?.({ data: JSON.stringify({ type: 'partial', text: '帮你检查一下这个配置文件' }) })
    expect(autoSpeech.stopAudio).not.toHaveBeenCalled()
    expect(useFreeDialog().phase.value).toBe('speaking')
  })

  it('stops the read-aloud when the user speaks over it', async () => {
    const { startFreeDialog, setFreeDialogThinking } = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    autoSpeech.speakingText.value = '好的我来帮你检查一下这个配置文件里的端口设置'
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    speak('做点事 发送')
    setFreeDialogThinking(false)
    autoSpeech.state.value = 'playing'
    await flush()
    await flush()
    expect(sockets.length).toBe(2)
    const barge = sockets[sockets.length - 1]
    barge.onmessage?.({ data: JSON.stringify({ type: 'partial', text: '等一下先别改那个文件' }) })
    expect(autoSpeech.stopAudio).toHaveBeenCalled()
  })

  it('opens no barge listener when interrupting is switched off', async () => {
    vi.doMock('@/composables/useSettingsConfig', async () => {
      const { ref } = await vi.importActual<typeof import('vue')>('vue')
      return { serverConfig: ref({ stt: { barge_in: 'off', trigger_words: '发送', silence_send_seconds: 3 } }) }
    })
    const { startFreeDialog, setFreeDialogThinking, useFreeDialog } = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    speak('做点事 发送')
    setFreeDialogThinking(false)
    autoSpeech.state.value = 'playing'
    await flush()
    await flush()
    // Only the original listening socket exists — no second microphone was
    // opened while the reply is being read out.
    expect(sockets.length).toBe(1)
    expect(useFreeDialog().phase.value).toBe('speaking')
  })

  it('fires the trigger from a live partial, not only from "done"', async () => {
    // The streaming relay only emits `done` after the client sends `end`, and
    // free-dialog mode never sends `end` while listening — so a trigger that
    // waited for `done` would never fire at all.
    const onSend = vi.fn()
    const { startFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend })
    await flush()
    speak('帮我看下这个报错 发送', 'partial')
    // Matched on the partial, but the sentence is not settled yet: the engine
    // is asked to finish it first, so the tail is not lost.
    expect(onSend).not.toHaveBeenCalled()
    expect(endFrames(sockets[0])).toBe(1)
    speak('帮我看下这个报错 发送', 'done')
    // The trigger word is stripped again from the finished sentence.
    expect(onSend).toHaveBeenCalledWith('帮我看下这个报错')
  })

  it('does not fire while the trigger word is mid-sentence', async () => {
    // Partial results are revised as the user keeps talking; a trigger matched
    // anywhere would send the moment the word is heard, then keep talking.
    const onSend = vi.fn()
    const { startFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend })
    await flush()
    speak('把那个文件改好了', 'partial')
    expect(onSend).not.toHaveBeenCalled()
    speak('把那个文件改好了再告诉我', 'partial')
    expect(onSend).not.toHaveBeenCalled()
  })

  it('reopens the microphone when the reply has nothing to read out', async () => {
    // auto-speech is on, but the reply has no speakable text (or TTS failed):
    // playback never starts, so the loop must not wait for it forever.
    const { startFreeDialog, useFreeDialog, setFreeDialogThinking } = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    autoSpeech.enabled.value = true
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    speak('做点事 发送')
    setFreeDialogThinking(false)
    // No playback ever begins.
    vi.advanceTimersByTime(1500)
    vi.advanceTimersByTime(500)
    await flush()
    expect(useFreeDialog().phase.value).toBe('listening')
  })

  it('does not reopen the microphone while a queued speaker is still playing', async () => {
    // Group runs read several replies in sequence: state goes playing → idle →
    // synthesizing between items. Reopening on that dip transcribes item 2.
    const { startFreeDialog, useFreeDialog, setFreeDialogThinking } = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    speak('做点事 发送')
    setFreeDialogThinking(false)
    autoSpeech.state.value = 'playing'
    await flush()
    // Hand-off between two queued speakers.
    autoSpeech.state.value = 'idle'
    await flush()
    autoSpeech.state.value = 'synthesizing'
    await flush()
    vi.advanceTimersByTime(5000)
    await flush()
    expect(useFreeDialog().phase.value).toBe('speaking')
  })

  it('stops feeding dictation before the assistant starts talking', async () => {
    // The user can tap read-aloud on any message while the mode is listening.
    // The dictation recogniser must stop receiving frames, or it transcribes
    // the assistant as the user's next message.
    const { startFreeDialog, useFreeDialog } = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    const getUserMedia = navigator.mediaDevices.getUserMedia as ReturnType<typeof vi.fn>
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    expect(useFreeDialog().phase.value).toBe('listening')
    expect(getUserMedia).toHaveBeenCalledTimes(1)
    const dictation = sockets[0]
    autoSpeech.state.value = 'playing'
    await flush()
    await flush()
    expect(dictation.readyState).toBe(3)
    // The device is NOT reopened: the same capture now feeds the interrupt
    // listener. Re-acquiring here is what made playback start on a freshly
    // configured audio route and lose its first syllables.
    expect(getUserMedia).toHaveBeenCalledTimes(1)
    expect(useFreeDialog().phase.value).toBe('speaking')
  })

  it('closes the barge-in socket when playback ends', async () => {
    const { startFreeDialog, setFreeDialogThinking } = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    speak('做点事 发送')
    setFreeDialogThinking(false)
    autoSpeech.state.value = 'playing'
    await flush()
    await flush()
    expect(sockets.length).toBe(2)
    const barge = sockets[sockets.length - 1]
    autoSpeech.state.value = 'idle'
    await flush()
    // Without this the recogniser session (and its server-side engine session)
    // is leaked once per read-aloud.
    expect(barge.readyState).toBe(3)
  })

  it('stops cleanly and returns to idle', async () => {
    const { startFreeDialog, useFreeDialog, stop } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    expect(useFreeDialog().phase.value).toBe('listening')
    ;(await import('../useFreeDialog')).stop()
    expect(useFreeDialog().phase.value).toBe('idle')
    expect(stop).toBeTypeOf('function')
  })

  it('reports the bound session id and clears it when idle', async () => {
    const mod = await import('../useFreeDialog')
    expect(mod.freeDialogSessionId()).toBe('')
    await mod.startFreeDialog({ sessionId: 's9', onSend: vi.fn() })
    await flush()
    expect(mod.freeDialogSessionId()).toBe('s9')
    mod.stop()
    expect(mod.freeDialogSessionId()).toBe('')
  })

  it('ignores a trigger word that arrives after the mode was stopped', async () => {
    const onSend = vi.fn()
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend })
    await flush()
    const sock = sockets[0]
    mod.stop()
    // A late frame from the old socket must not send into a mode the user
    // already left.
    sock.onmessage?.({ data: JSON.stringify({ type: 'done', final: '做点事 发送' }) })
    expect(onSend).not.toHaveBeenCalled()
  })

  // ── audible feedback ──
  //
  // The user is not looking at the screen in this mode, so "the sentence was
  // taken" and "the mode is off" both need a sound.

  it('beeps when it takes a spoken sentence and sends it', async () => {
    const { startFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    cuePlayed.mockClear()
    speak('帮我看下这个报错 发送')
    expect(cuePlayed).toHaveBeenCalledWith('send')
  })

  it('does not beep when there was nothing to send', async () => {
    // A bare trigger word sends nothing, so a "sent" cue would be a lie.
    const { startFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    cuePlayed.mockClear()
    speak('发送')
    expect(cuePlayed).not.toHaveBeenCalled()
  })

  it('beeps when the user cuts the generation short', async () => {
    // "It went quiet" is indistinguishable from a dropped connection or a TTS
    // failure, and the user is not looking at the screen: the cut-in needs its
    // own confirmation.
    const onInterrupt = vi.fn()
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn(), onInterrupt })
    await flush()
    speak('做点事 发送')
    vi.advanceTimersByTime(500)
    await flush()
    cuePlayed.mockClear()
    speak('等等', 'partial')
    expect(onInterrupt).toHaveBeenCalledTimes(1)
    expect(cuePlayed).toHaveBeenCalledWith('interrupt')
  })

  it('beeps when the user cuts the read-aloud short', async () => {
    const mod = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn(), onInterrupt: vi.fn() })
    await flush()
    speak('做点事 发送')
    mod.setFreeDialogThinking(false)
    autoSpeech.state.value = 'playing'
    await flush()
    await flush()
    cuePlayed.mockClear()
    speak('等一下，先别改那个文件', 'partial')
    expect(autoSpeech.stopAudio).toHaveBeenCalled()
    expect(cuePlayed).toHaveBeenCalledWith('interrupt')
  })

  it('unlocks the cue audio while the starting tap is still the gesture', async () => {
    // The cues fire from speech recognition and turn events — never from a tap.
    // A context first created there stays `suspended` under a strict autoplay
    // policy, and every cue in the session is silent while the mode otherwise
    // looks perfectly healthy. The unlock must happen synchronously in start().
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    expect(cueUnlocked).toHaveBeenCalledTimes(1)
  })

  it('does not beep when the heard speech is ignored', async () => {
    // The cue means "your interruption landed". Playing it for a fragment the
    // loop decided to ignore (the assistant's own voice here) would tell the
    // user they had been heard when nothing happened.
    const { startFreeDialog, setFreeDialogThinking } = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    autoSpeech.speakingText.value = '好的我来帮你检查一下这个配置文件里的端口设置'
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    speak('做点事 发送')
    setFreeDialogThinking(false)
    autoSpeech.state.value = 'playing'
    await flush()
    await flush()
    cuePlayed.mockClear()
    const barge = sockets[sockets.length - 1]
    barge.onmessage?.({ data: JSON.stringify({ type: 'partial', text: '帮你检查一下这个配置文件' }) })
    expect(autoSpeech.stopAudio).not.toHaveBeenCalled()
    expect(cuePlayed).not.toHaveBeenCalled()
  })

  it('beeps when the mode closes', async () => {
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    cuePlayed.mockClear()
    mod.stop()
    expect(cuePlayed).toHaveBeenCalledWith('exit')
  })

  it('beeps when the mode closes itself after a long silence', async () => {
    // The automatic exit is exactly the case where nothing on screen is
    // noticed: the user walked away believing the mode was still listening.
    const settings = await import('@/composables/useSettingsConfig')
    settings.serverConfig.value = { stt: { silence_send_seconds: 0, idle_exit_seconds: 5 } }
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    cuePlayed.mockClear()
    vi.advanceTimersByTime(5000)
    await flush()
    expect(mod.useFreeDialog().phase.value).toBe('idle')
    expect(cuePlayed).toHaveBeenCalledWith('exit')
  })

  it('stays silent on the internal reset that start() performs', async () => {
    // start() first stops any previous run; a "closed" beep a millisecond
    // before "opened" would describe the opposite of what is happening.
    const mod = await import('../useFreeDialog')
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    cuePlayed.mockClear()
    await mod.startFreeDialog({ sessionId: 's2', onSend: vi.fn() })
    await flush()
    expect(cuePlayed).not.toHaveBeenCalled()
  })

  // ── a reply whose `done` never arrived ──

  it('asks the owner to read a reply that never reached TTS', async () => {
    // The terminal `done` is the only thing that normally starts read-aloud,
    // and it is lost when the app was backgrounded or the subscription
    // dropped — the reply then only appears in the history reload. Without
    // this the loop reopens the microphone and the user never hears it.
    const onSpeakPendingReply = vi.fn(() => true)
    const { startFreeDialog, useFreeDialog, setFreeDialogThinking } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn(), onSpeakPendingReply })
    await flush()
    speak('做点事 发送')
    setFreeDialogThinking(false)
    vi.advanceTimersByTime(600)
    await flush()
    expect(onSpeakPendingReply).toHaveBeenCalledTimes(1)
    // Playback is (about to be) running: the microphone must stay closed.
    expect(useFreeDialog().phase.value).not.toBe('listening')
  })

  it('still resumes listening when there is nothing to read', async () => {
    // A tool-only reply, or read-aloud off: the loop must not stall waiting
    // for a playback that will never start.
    const onSpeakPendingReply = vi.fn(() => false)
    const { startFreeDialog, useFreeDialog, setFreeDialogThinking } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn(), onSpeakPendingReply })
    await flush()
    speak('做点事 发送')
    setFreeDialogThinking(false)
    vi.advanceTimersByTime(600)
    await flush()
    expect(onSpeakPendingReply).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(250)
    await flush()
    expect(useFreeDialog().phase.value).toBe('listening')
  })

  it('does not ask again once playback has started normally', async () => {
    // The normal path hands the reply to TTS at `done`, which marks auto-speech
    // busy synchronously — the settle window must then bail out, not fire a
    // second read of the same reply.
    const onSpeakPendingReply = vi.fn(() => true)
    const { startFreeDialog, setFreeDialogThinking } = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn(), onSpeakPendingReply })
    await flush()
    speak('做点事 发送')
    autoSpeech.state.value = 'synthesizing'
    setFreeDialogThinking(false)
    vi.advanceTimersByTime(5000)
    await flush()
    expect(onSpeakPendingReply).not.toHaveBeenCalled()
  })

  it('re-acquires the microphone when the capture stops producing frames', async () => {
    // The reported failure: after an interrupt the graph can stall — the track
    // stays `live`, the socket is open, the phase is 'listening', but no frame
    // ever arrives again. Nothing in the loop can tell that apart from a device
    // that went away, and only a fresh device + AudioContext pair brings it
    // back. (A graph that keeps producing ZERO-VALUED frames is NOT this case:
    // see the digital-silence test below.)
    const getUserMedia = navigator.mediaDevices.getUserMedia as ReturnType<typeof vi.fn>
    const { startFreeDialog, useFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    const streamer = (globalThis as Record<string, { emit: (n: number) => void }>).__streamer
    streamer.emit(0.02)
    expect(getUserMedia).toHaveBeenCalledTimes(1)

    // The frames stop coming entirely — no `emit` at all from here on.
    for (let i = 0; i < 5; i++) vi.advanceTimersByTime(1000)
    // Two flushes: the recovery re-acquires, then opens a socket whose `open`
    // arrives on its own microtask.
    await flush()
    await flush()

    expect(getUserMedia).toHaveBeenCalledTimes(2)
    // Still listening, and on a recogniser that is fed by the NEW graph.
    expect(useFreeDialog().phase.value).toBe('listening')
    expect(sockets.at(-1)!.sent.some((m) => String(m).includes('"type":"start"'))).toBe(true)
  })

  it('leaves a phone that reports exact digital silence alone', async () => {
    // The regression this locks down. With noise suppression and echo
    // cancellation on — the shipped defaults — a healthy phone hands over
    // frames of EXACT zeros whenever the user is not speaking. The old watchdog
    // read "three seconds of zeros" as a dead device, tore the capture down
    // twice and then left the mode with an error toast, about ten seconds after
    // every start and always right after the user's first sentence had been
    // sent. Silence is not a fault.
    const getUserMedia = navigator.mediaDevices.getUserMedia as ReturnType<typeof vi.fn>
    const { startFreeDialog, useFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    const streamer = (globalThis as Record<string, { emit: (n: number) => void }>).__streamer
    toastShow.mockClear()

    // Say one thing, then sit in silence for far longer than the watchdog span.
    streamer.emit(0.03)
    for (let i = 0; i < 20; i++) {
      vi.advanceTimersByTime(1000)
      streamer.emit(0)
    }
    await flush()

    expect(getUserMedia).toHaveBeenCalledTimes(1)
    expect(useFreeDialog().phase.value).toBe('listening')
    expect(toastShow).not.toHaveBeenCalled()
  })

  it('leaves a quiet room alone: room tone is not a dead capture', async () => {
    // Room tone (a fan, a fridge, traffic) must not be re-acquired on either —
    // that would drop a moment of audio every few seconds for every user in a
    // quiet room.
    const getUserMedia = navigator.mediaDevices.getUserMedia as ReturnType<typeof vi.fn>
    const { startFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    const streamer = (globalThis as Record<string, { emit: (n: number) => void }>).__streamer
    for (let i = 0; i < 8; i++) {
      vi.advanceTimersByTime(500)
      streamer.emit(0.0012)
    }
    await flush()
    expect(getUserMedia).toHaveBeenCalledTimes(1)
  })

  it('stops with a reason when re-acquiring twice produces no frames at all', async () => {
    // A capture that delivers no frame whatsoever IS provably broken (a
    // suspended AudioContext, a device the OS took away). Two fresh captures
    // that still deliver nothing mean the page cannot fix it, and silently
    // "listening" to nothing is the exact behaviour being fixed. Say it out
    // loud instead.
    const { startFreeDialog, useFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    let streamer = (globalThis as Record<string, { emit: (n: number) => void }>).__streamer
    streamer.emit(0.02)
    toastShow.mockClear()

    // Three spans in a row where the graph delivers nothing at all: the third
    // one exhausts the budget.
    for (let round = 0; round < 3; round++) {
      vi.advanceTimersByTime(4000)
      await flush()
      streamer = (globalThis as Record<string, { emit: (n: number) => void }>).__streamer
    }

    expect(useFreeDialog().phase.value).toBe('idle')
    expect(toastShow).toHaveBeenCalled()
    expect(String(toastShow.mock.calls.at(-1)![0])).toMatch(/麦克风|microphone/i)
  })
})
