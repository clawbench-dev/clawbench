import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

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

  vi.doMock('@/composables/useToast', () => ({
    useToast: () => ({ show: vi.fn() }),
  }))
}

/** Pushes one recogniser message into the newest socket. */
function speak(text: string, kind: 'partial' | 'text' | 'done' = 'done') {
  const sock = sockets[sockets.length - 1]
  if (!sock) throw new Error('no socket')
  const payload = kind === 'done' ? { type: 'done', final: text } : { type: kind, text }
  sock.onmessage?.({ data: JSON.stringify(payload) })
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
    vi.doUnmock('@/utils/pcmCapture')
    vi.doUnmock('@/composables/useAutoSpeech')
    vi.doUnmock('@/composables/useSettingsConfig')
    vi.doUnmock('@/composables/useToast')
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

  it('leaves read-aloud on after the mode stops', async () => {
    // It was turned on because the user started the mode; reversing their
    // preference on exit is a surprise, not a courtesy.
    const mod = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    autoSpeech.enabled.value = false
    await mod.startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    mod.stop()
    expect(autoSpeech.enabled.value).toBe(true)
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

  it('sends after a pause long enough to count as done', async () => {
    const onSend = vi.fn()
    const { startFreeDialog } = await import('../useFreeDialog')
    await startFreeDialog({ sessionId: 's1', onSend })
    await flush()
    // Recognition confirms a sentence, no trigger word in it.
    speak('这个文件改坏了', 'text')
    expect(onSend).not.toHaveBeenCalled()
    vi.advanceTimersByTime(3000)
    expect(onSend).toHaveBeenCalledWith('这个文件改坏了')
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
    expect(onSend).toHaveBeenCalledWith('第一句话')
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
    // 400 ms in: still settling. Reopening here captures the speaker's tail.
    // The flush matters: `openListening` awaits `getUserMedia`, so without it a
    // zero-delay implementation would still read as 'speaking' and the test
    // would pass on either behaviour.
    vi.advanceTimersByTime(400)
    await flush()
    expect(useFreeDialog().phase.value).toBe('speaking')
    vi.advanceTimersByTime(200)
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

  it('stops dictation before the assistant starts talking', async () => {
    // The user can tap read-aloud on any message while the mode is listening.
    // The dictation capture must go first, or the recogniser transcribes the
    // assistant and a second microphone is opened on top of it.
    const { startFreeDialog, useFreeDialog } = await import('../useFreeDialog')
    const autoSpeech = (await import('@/composables/useAutoSpeech')).useAutoSpeech()
    await startFreeDialog({ sessionId: 's1', onSend: vi.fn() })
    await flush()
    expect(useFreeDialog().phase.value).toBe('listening')
    const streamer = (globalThis as Record<string, { stopped: boolean }>).__streamer
    autoSpeech.state.value = 'playing'
    await flush()
    await flush()
    expect(streamer.stopped).toBe(true)
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
})
