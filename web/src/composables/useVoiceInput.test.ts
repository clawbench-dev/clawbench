import { describe, it, expect, vi, beforeEach } from 'vitest'

describe('useVoiceInput', () => {
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

  it('appendText appends to inputText with newline separator', async () => {
    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    v.setInputText('你好')
    v.appendText('世界')
    expect(v.inputText.value).toBe('你好\n世界')
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
    await vi.waitFor(() => expect(track.stop).toHaveBeenCalled())
    expect(v.state.value).toBe('done')
    expect(v.isRecording.value).toBe(false)
  })

  it('streaming start passes chunk_ms as the MediaRecorder timeslice', async () => {
    const { serverConfig } = await import('./useSettingsConfig')
    serverConfig.value = { 'stt.streaming': true, 'stt.chunk_ms': 500 }

    const track = { stop: vi.fn() }
    const stream = { getTracks: () => [track] }
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: vi.fn().mockResolvedValue(stream) },
    })

    const startArg: { current: number | undefined } = { current: undefined }
    vi.stubGlobal('MediaRecorder', class {
      state = 'recording'
      ondataavailable: unknown = null
      onstop: unknown = null
      start(timeslice?: number) { startArg.current = timeslice; this.state = 'recording' }
      stop() { this.state = 'inactive' }
      static isTypeSupported = vi.fn(() => true)
    })

    let wsInstance: {
      onopen: (() => void) | null
      onmessage: unknown
      onerror: unknown
      onclose: unknown
    } | undefined
    vi.stubGlobal('WebSocket', class {
      readyState = 1
      static OPEN = 1
      onopen: (() => void) | null = null
      onmessage: unknown = null
      onerror: unknown = null
      onclose: unknown = null
      send = vi.fn()
      close = vi.fn()
      constructor() { wsInstance = this }
    })

    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    await v.start()
    wsInstance!.onopen!()
    expect(startArg.current).toBe(500)
    v.cancel()
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
