import { describe, it, expect, vi, beforeEach, beforeAll } from 'vitest'

describe('useVoiceInput', () => {
  // Pay the module-graph transform once, outside any single test's budget.
  // `beforeEach` resets the module registry, so every case re-imports, but only
  // the FIRST import of the file is slow (it compiles the whole dependency
  // graph). Under full-suite load that cold start exceeded the 5s default and
  // failed `starts idle` — a timeout that said nothing about the behaviour
  // under test.
  beforeAll(async () => {
    await import('./useVoiceInput')
  }, 30000)

  beforeEach(() => {
    vi.resetModules()
  })

  it('starts idle', async () => {
    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    expect(v.state.value).toBe('idle')
  })

  it('setState transitions the state machine', async () => {
    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    v.setState('recording')
    expect(v.state.value).toBe('recording')
    v.setState('transcribing')
    expect(v.state.value).toBe('transcribing')
    v.setState('done')
    expect(v.state.value).toBe('done')
  })

  // Chinese/Japanese/Korean do not separate words with spaces, so two CJK
  // fragments must stay glued. (This case used to be titled "concatenates
  // without inserting a separator", which was true for every script and is
  // exactly what ran English words together.)
  it('appendText joins two CJK fragments without a space', async () => {
    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    v.setInputText('你好')
    v.appendText('世界')
    expect(v.inputText.value).toBe('你好世界')
  })

  // Regression: the buffered engine emits one transcript per audio segment and
  // appendText glued them verbatim, so dictating into a box that already had
  // text produced "Please reviewthe diff" in English.
  it('appendText separates two English fragments with a space', async () => {
    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    v.setInputText('Please review')
    v.appendText('the diff')
    expect(v.inputText.value).toBe('Please review the diff')
  })

  it('appendText adds no space when the transcript opens with punctuation', async () => {
    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    v.setInputText('hello')
    v.appendText(', world')
    expect(v.inputText.value).toBe('hello, world')
  })

  it('appendText adds no leading space when the box is empty', async () => {
    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    v.appendText('hello')
    expect(v.inputText.value).toBe('hello')
  })

  it('appendText keeps the existing text byte-for-byte (no trim)', async () => {
    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    v.setInputText('hello ')
    v.appendText('world')
    expect(v.inputText.value).toBe('hello world')
  })

  it('appendText ignores empty text', async () => {
    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    v.setInputText('你好')
    v.appendText('')
    expect(v.inputText.value).toBe('你好')
  })

  it('toggle with denied mic returns to idle', async () => {
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: vi.fn().mockRejectedValue(new Error('denied')) },
    })
    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    await v.toggle()
    expect(v.state.value).toBe('idle')
    expect(v.error.value).toBeTruthy()
  })

  it('reports a clear error when mediaDevices is missing (insecure context)', async () => {
    vi.stubGlobal('navigator', {}) // no mediaDevices → non-secure context
    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    await v.toggle()
    expect(v.state.value).toBe('idle')
    expect(v.error.value).toContain('HTTPS')
  })

  it('non-streaming release stops media tracks and nulls the stream', async () => {
    const track = { stop: vi.fn() }
    const stream = { getTracks: () => [track] }
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: vi.fn().mockResolvedValue(stream) },
    })
    vi.stubGlobal('MediaRecorder', class {
      state = 'recording'
      ondataavailable: ((e: { data: { size: number } }) => void) | null = null
      onstop: (() => void) | null = null
      start() { this.state = 'recording' }
      stop() { this.state = 'inactive'; this.onstop?.() }
      static isTypeSupported = vi.fn(() => true)
    })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ text: '识别结果' }),
    }))

    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    await v.start()
    v.stop()
    // Wait for the actual completion, not for a side effect that now happens
    // earlier (stop() releases the mic before the transcription finishes).
    await vi.waitFor(() => expect(v.state.value).toBe('idle'))
    expect(track.stop).toHaveBeenCalled()
    expect(v.isRecording.value).toBe(false)
  })

  it('returns to idle after non-streaming completion so recording can start again', async () => {
    const track = { stop: vi.fn() }
    const stream = { getTracks: () => [track] }
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: vi.fn().mockResolvedValue(stream) },
    })
    vi.stubGlobal('MediaRecorder', class {
      state = 'recording'
      ondataavailable: unknown = null
      onstop: (() => void) | null = null
      start() { this.state = 'recording' }
      stop() { this.state = 'inactive'; this.onstop?.() }
      static isTypeSupported = vi.fn(() => true)
    })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ text: '识别结果' }),
    }))

    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    await v.start()
    v.stop()
    // After completion state must be back to idle so a new recording can start.
    await vi.waitFor(() => expect(v.state.value).toBe('idle'))
    expect(track.stop).toHaveBeenCalled()
    // A second toggle must proceed (call getUserMedia again), not early-return.
    await v.start()
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledTimes(2)
  })

  it('does not echo the previous recording into a new non-streaming transcription', async () => {
    const track = { stop: vi.fn() }
    const stream = { getTracks: () => [track] }
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: vi.fn().mockResolvedValue(stream) },
    })
    const texts = ['first recording', 'second recording']
    let call = 0
    vi.stubGlobal('MediaRecorder', class {
      state = 'recording'
      ondataavailable: unknown = null
      onstop: (() => void) | null = null
      start() { this.state = 'recording' }
      stop() { this.state = 'inactive'; this.onstop?.() }
      static isTypeSupported = vi.fn(() => true)
    })
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() =>
      Promise.resolve({ ok: true, json: async () => ({ text: texts[call++] }) })
    ))

    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()

    // Recording 1 → "first recording"
    await v.start()
    v.stop()
    await vi.waitFor(() => expect(v.inputText.value).toBe('first recording'))

    // Recording 2 → must be a fresh transcription, NOT "first recording\nsecond recording"
    await v.start()
    v.stop()
    await vi.waitFor(() => expect(v.inputText.value).toBe('second recording'))
    expect(v.inputText.value).not.toContain('first recording')
  })

  it('start(prefix) keeps the text already in the input box and appends to it', async () => {
    const track = { stop: vi.fn() }
    const stream = { getTracks: () => [track] }
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: vi.fn().mockResolvedValue(stream) },
    })
    vi.stubGlobal('MediaRecorder', class {
      state = 'recording'
      ondataavailable: unknown = null
      onstop: (() => void) | null = null
      start() { this.state = 'recording' }
      stop() { this.state = 'inactive'; this.onstop?.() }
      static isTypeSupported = vi.fn(() => true)
    })
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() =>
      Promise.resolve({ ok: true, json: async () => ({ text: '识别结果' }) })
    ))

    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()

    // Dictation must append to what the user already typed, not replace it.
    await v.start('已输入的文字')
    v.stop()
    await vi.waitFor(() => expect(v.inputText.value).toBe('已输入的文字识别结果'))
  })

  // The composer is emptied when the message is sent, but a dictation started
  // from that text is still in flight. Its snapshot has to be dropped at that
  // point: the late transcript would otherwise rebuild "sent text + transcript"
  // and drop it back into the box — the message looks unsent, and the next tap
  // sends it a second time.
  it('setStartText("") keeps a late transcript from resurrecting the sent text', async () => {
    const track = { stop: vi.fn() }
    const stream = { getTracks: () => [track] }
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: vi.fn().mockResolvedValue(stream) },
    })
    vi.stubGlobal('MediaRecorder', class {
      state = 'recording'
      ondataavailable: unknown = null
      onstop: (() => void) | null = null
      start() { this.state = 'recording' }
      stop() { this.state = 'inactive'; this.onstop?.() }
      static isTypeSupported = vi.fn(() => true)
    })
    // Hold the response until the composer has been cleared, so the ordering
    // matches the real send (the POST is in flight when clearInput() runs).
    let resolveFetch!: (v: unknown) => void
    vi.stubGlobal('fetch', vi.fn(() => new Promise((resolve) => { resolveFetch = resolve })))

    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()

    await v.start('请看一下这个改动')
    v.stop()
    // The message went out → the composer is empty.
    v.setStartText('')
    resolveFetch({ ok: true, json: async () => ({ text: '听写' }) })

    await vi.waitFor(() => expect(v.state.value).toBe('idle'))
    expect(v.inputText.value).toBe('听写')
    expect(v.inputText.value).not.toContain('请看一下这个改动')
  })

  // Same hazard on the PCM path, where the partial is rebuilt from
  // `voicePrefix` rather than appended — clearing only `inputText` there would
  // leave the sent text in front of every partial.
  it('setStartText("") also drops the PCM prefix that partials are built on', async () => {
    const { serverConfig } = await import('./useSettingsConfig')
    serverConfig.value = { stt: { streaming: true, provider: 'doubao' } }
    const env = stubStreamingEnv()

    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    await v.start('请看一下这个改动')
    env.getWS()!.onopen!()
    await vi.waitFor(() => expect(env.getWS()!.send).toHaveBeenCalled())

    env.getWS()!.onmessage!({ data: JSON.stringify({ type: 'partial', text: '听写' }) })
    expect(v.inputText.value).toBe('请看一下这个改动听写')

    // The message was sent: the box is empty and the snapshot is gone.
    v.setStartText('')
    env.getWS()!.onmessage!({ data: JSON.stringify({ type: 'partial', text: '听写' }) })
    expect(v.inputText.value).toBe('听写')

    v.cancel()
  })

  // ── streaming (PCM + AudioWorklet) ───────────────────────────────

  interface FakeWS {
    readyState: number
    onopen: (() => void) | null
    onmessage: ((e: { data: string }) => void) | null
    onerror: (() => void) | null
    onclose: (() => void) | null
    send: ReturnType<typeof vi.fn>
    close: ReturnType<typeof vi.fn>
  }

  /** Installs fake WebSocket / AudioContext / AudioWorkletNode globals. */
  function stubStreamingEnv(initialReadyState = 1) {
    const track = { stop: vi.fn() }
    const stream = { getTracks: () => [track] }
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: vi.fn().mockResolvedValue(stream) },
    })

    let wsInstance: FakeWS | undefined
    vi.stubGlobal('WebSocket', class {
      static CONNECTING = 0
      static OPEN = 1
      readyState = initialReadyState
      onopen: (() => void) | null = null
      onmessage: ((e: { data: string }) => void) | null = null
      onerror: (() => void) | null = null
      onclose: (() => void) | null = null
      send = vi.fn()
      close = vi.fn()
      constructor() { wsInstance = this as unknown as FakeWS }
    })

    const workletNode = {
      port: { onmessage: null as ((e: { data: ArrayBuffer }) => void) | null },
      connect: vi.fn(),
      disconnect: vi.fn(),
    }
    vi.stubGlobal('AudioWorkletNode', class {
      port = workletNode.port
      connect = workletNode.connect
      disconnect = workletNode.disconnect
    })
    const source = { connect: vi.fn(), disconnect: vi.fn() }
    vi.stubGlobal('AudioContext', class {
      sampleRate = 16000
      destination = {}
      audioWorklet = { addModule: vi.fn().mockResolvedValue(undefined) }
      createMediaStreamSource = vi.fn(() => source)
      close = vi.fn().mockResolvedValue(undefined)
    })
    vi.stubGlobal('URL', Object.assign(URL, {
      createObjectURL: vi.fn(() => 'blob:worklet'),
      revokeObjectURL: vi.fn(),
    }))

    return {
      getWS: () => wsInstance,
      workletNode,
      track,
    }
  }

  it('streaming start sends a start frame with the PCM format and typed text as context', async () => {
    const { serverConfig } = await import('./useSettingsConfig')
    serverConfig.value = { stt: { streaming: true, provider: 'doubao' } }
    const env = stubStreamingEnv()

    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    await v.start('帮我总结')
    env.getWS()!.onopen!()
    await vi.waitFor(() => expect(env.getWS()!.send).toHaveBeenCalled())

    const first = JSON.parse(env.getWS()!.send.mock.calls[0][0] as string)
    expect(first.type).toBe('start')
    expect(first.sampleRate).toBe(16000)
    expect(first.channels).toBe(1)
    expect(first.bits).toBe(16)
    // The text already in the box becomes recognition context.
    expect(first.context).toBe('帮我总结')
    v.cancel()
  })

  it('streaming forwards PCM frames as binary and ends with an end control', async () => {
    const { serverConfig } = await import('./useSettingsConfig')
    serverConfig.value = { stt: { streaming: true, provider: 'doubao' } }
    const env = stubStreamingEnv()

    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    await v.start()
    env.getWS()!.onopen!()
    await vi.waitFor(() => expect(env.workletNode.port.onmessage).toBeTypeOf('function'))

    // Loud enough to clear the voice gate: a frame of near-zero samples is
    // silence and is deliberately NOT forwarded (see the gate tests below).
    const frame = new Int16Array(160).fill(8000).buffer
    env.workletNode.port.onmessage!({ data: frame })
    expect(env.getWS()!.send).toHaveBeenCalledWith(frame)

    await v.stop()
    const last = env.getWS()!.send.mock.calls.at(-1)![0] as string
    expect(JSON.parse(last)).toEqual({ type: 'end' })
    // Releasing the button must also release the microphone.
    expect(env.track.stop).toHaveBeenCalled()
    v.cancel()
  })

  it('stop() while the socket is still connecting releases the mic and never starts capture', async () => {
    const { serverConfig } = await import('./useSettingsConfig')
    serverConfig.value = { stt: { streaming: true, provider: 'doubao' } }
    const env = stubStreamingEnv(0) // CONNECTING

    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    await v.start()
    await v.stop()

    expect(env.track.stop).toHaveBeenCalled()

    // The socket opens only after the button was released: it must not start
    // capturing (the mic indicator stays off) and must not send a start frame.
    env.getWS()!.onopen!()
    expect(env.workletNode.port.onmessage).toBeNull()
    expect(env.getWS()!.send).not.toHaveBeenCalled()
    expect(v.state.value).toBe('idle')
    expect(v.isRecording.value).toBe(false)
  })

  it('drops a silent PCM frame instead of sending it to the engine', async () => {
    const { serverConfig } = await import('./useSettingsConfig')
    serverConfig.value = { stt: { streaming: true, provider: 'doubao', voice_gate: true } }
    const env = stubStreamingEnv()

    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    await v.start()
    env.getWS()!.onopen!()
    await vi.waitFor(() => expect(env.workletNode.port.onmessage).toBeTypeOf('function'))

    const sendsBefore = env.getWS()!.send.mock.calls.length
    // Pure silence: not a single sample above the floor.
    env.workletNode.port.onmessage!({ data: new Int16Array(160).buffer })
    expect(env.getWS()!.send.mock.calls.length).toBe(sendsBefore)
    v.cancel()
  })

  it('forwards a silent frame when the voice gate is switched off', async () => {
    const { serverConfig } = await import('./useSettingsConfig')
    serverConfig.value = { stt: { streaming: true, provider: 'doubao', voice_gate: false } }
    const env = stubStreamingEnv()

    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    await v.start()
    env.getWS()!.onopen!()
    await vi.waitFor(() => expect(env.workletNode.port.onmessage).toBeTypeOf('function'))

    const frame = new Int16Array(160).buffer
    env.workletNode.port.onmessage!({ data: frame })
    expect(env.getWS()!.send).toHaveBeenCalledWith(frame)
    v.cancel()
  })

  it('opens the microphone with the configured preprocessing constraints', async () => {
    const { serverConfig } = await import('./useSettingsConfig')
    serverConfig.value = {
      stt: {
        streaming: true,
        provider: 'doubao',
        mic_echo_cancel: true,
        mic_noise_suppress: false,
        mic_auto_gain: true,
      },
    }
    const env = stubStreamingEnv()

    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    await v.start()
    const constraints = (navigator.mediaDevices.getUserMedia as ReturnType<typeof vi.fn>).mock.calls[0][0]
    expect(constraints.audio.echoCancellation).toBe(true)
    expect(constraints.audio.noiseSuppression).toBe(false)
    expect(constraints.audio.autoGainControl).toBe(true)
    v.cancel()
    void env
  })

  it('uses MediaRecorder when the provider cannot decode PCM, even with streaming on', async () => {
    const { serverConfig } = await import('./useSettingsConfig')
    serverConfig.value = { stt: { streaming: true, provider: 'vllm' } }

    const track = { stop: vi.fn() }
    const stream = { getTracks: () => [track] }
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: vi.fn().mockResolvedValue(stream) },
    })
    const wsCtor = vi.fn()
    vi.stubGlobal('WebSocket', class {
      static CONNECTING = 0
      static OPEN = 1
      constructor() { wsCtor() }
    })
    const audioCtor = vi.fn()
    vi.stubGlobal('AudioContext', class {
      constructor() { audioCtor() }
    })
    vi.stubGlobal('MediaRecorder', class {
      state = 'recording'
      ondataavailable: unknown = null
      onstop: (() => void) | null = null
      start() { this.state = 'recording' }
      stop() { this.state = 'inactive'; this.onstop?.() }
      static isTypeSupported = vi.fn(() => true)
    })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ text: '识别结果' }),
    }))

    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    await v.start()

    // Raw PCM is only meaningful to an engine that decodes it; sending it to
    // Whisper makes every request fail.
    expect(wsCtor).not.toHaveBeenCalled()
    // An AudioContext IS created on this path — but for the level meter that
    // decides whether the clip has any speech, not for PCM streaming. What must
    // not happen is a recogniser socket.
    expect(audioCtor).toHaveBeenCalled()

    v.stop()
    await vi.waitFor(() => expect(v.inputText.value).toBe('识别结果'))
  })

  it('uses the PCM stream when the provider is doubao and streaming is on', async () => {
    const { serverConfig } = await import('./useSettingsConfig')
    serverConfig.value = { stt: { streaming: true, provider: 'doubao' } }
    const env = stubStreamingEnv()

    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    await v.start()

    expect(env.getWS()).toBeDefined()
    env.getWS()!.onopen!()
    await vi.waitFor(() => expect(env.workletNode.port.onmessage).toBeTypeOf('function'))
    v.cancel()
  })

  it('uses the PCM stream when the provider is alibaba and streaming is on', async () => {
    const { serverConfig } = await import('./useSettingsConfig')
    serverConfig.value = { stt: { streaming: true, provider: 'alibaba' } }
    const env = stubStreamingEnv()

    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    await v.start()

    expect(env.getWS()).toBeDefined()
    env.getWS()!.onopen!()
    await vi.waitFor(() => expect(env.workletNode.port.onmessage).toBeTypeOf('function'))
    v.cancel()
  })

  it('partial messages replace the transcript while keeping the typed prefix', async () => {
    const { serverConfig } = await import('./useSettingsConfig')
    serverConfig.value = { stt: { streaming: true, provider: 'doubao' } }
    const env = stubStreamingEnv()

    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    await v.start('前缀')
    env.getWS()!.onopen!()
    await vi.waitFor(() => expect(env.getWS()!.send).toHaveBeenCalled())

    env.getWS()!.onmessage!({ data: JSON.stringify({ type: 'partial', text: '你好' }) })
    expect(v.inputText.value).toBe('前缀你好')
    // A later revision replaces rather than appends.
    env.getWS()!.onmessage!({ data: JSON.stringify({ type: 'partial', text: '你好世界' }) })
    expect(v.inputText.value).toBe('前缀你好世界')

    env.getWS()!.onmessage!({ data: JSON.stringify({ type: 'done', final: '你好世界。' }) })
    expect(v.inputText.value).toBe('前缀你好世界。')
    expect(v.state.value).toBe('idle')
  })

  it('uses fallback MediaRecorder construction when webm is unsupported', async () => {
    const track = { stop: vi.fn() }
    const stream = { getTracks: () => [track] }
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: vi.fn().mockResolvedValue(stream) },
    })
    const ctor = vi.fn()
    vi.stubGlobal('MediaRecorder', class {
      state = 'inactive'
      ondataavailable = null
      onstop = null
      start() { this.state = 'recording' }
      stop() { this.state = 'inactive' }
      constructor(_s: unknown, opts: unknown) {
        ctor(opts)
      }
      static isTypeSupported = vi.fn(() => false)
    })

    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    await v.start()
    expect(ctor).toHaveBeenCalledWith(undefined)
  })
})
