import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { extractSpeakableText, useAutoSpeech } from '@/composables/useAutoSpeech'
import { localConfig } from '@/composables/useSettingsConfig'

// vi.hoisted runs before vi.mock hoisting, so the mock factory can reference it
const { toastShowMock, nativeTtsMock } = vi.hoisted(() => ({
  toastShowMock: vi.fn(),
  nativeTtsMock: {
    ttsSpeak: vi.fn(() => 'ready'),
    ttsStatus: vi.fn(() => 'ready'),
    ttsStop: vi.fn(),
  },
}))

// Mock useToast since it's used at module level
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: toastShowMock }),
}))

// Mock useLocale
vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

// Only getNative is replaced: the rest of the module is imported by other
// modules under test (download URLs, wake lock), and stubbing it out entirely
// would leave those imports undefined.
vi.mock('@/utils/clawbenchNative', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/utils/clawbenchNative')>()
  return { ...actual, getNative: () => nativeTtsMock }
})

describe('extractSpeakableText', () => {
  it('extracts text from text blocks', () => {
    const blocks = [
      { type: 'text', text: 'Hello world' },
    ]
    expect(extractSpeakableText(blocks)).toBe('Hello world')
  })

  it('extracts text from multiple text blocks', () => {
    const blocks = [
      { type: 'text', text: 'Hello' },
      { type: 'text', text: 'world' },
    ]
    expect(extractSpeakableText(blocks)).toBe('Hello\nworld')
  })

  it('skips empty text blocks', () => {
    const blocks = [
      { type: 'text', text: 'Hello' },
      { type: 'text', text: '  ' },
      { type: 'text', text: 'world' },
    ]
    expect(extractSpeakableText(blocks)).toBe('Hello\nworld')
  })

  it('extracts questions from AskUserQuestion tool_use blocks', () => {
    const blocks = [
      {
        type: 'tool_use',
        name: 'AskUserQuestion',
        input: {
          questions: [
            {
              question: 'Which approach do you prefer?',
              header: 'Approach',
              options: [
                { label: 'Option A', description: 'Fast but less safe' },
                { label: 'Option B', description: 'Safe but slower' },
              ],
            },
          ],
        },
      },
    ]
    const result = extractSpeakableText(blocks)
    expect(result).toContain('Which approach do you prefer?')
    expect(result).toContain('(Approach)')
    expect(result).toContain('Option A — Fast but less safe')
    expect(result).toContain('Option B — Safe but slower')
  })

  it('handles string options in AskUserQuestion', () => {
    const blocks = [
      {
        type: 'tool_use',
        name: 'AskUserQuestion',
        input: {
          questions: [
            {
              question: 'Choose a color',
              options: ['Red', 'Blue', 'Green'],
            },
          ],
        },
      },
    ]
    const result = extractSpeakableText(blocks)
    expect(result).toContain('Choose a color')
    expect(result).toContain('Red, Blue, Green')
  })

  it('skips options with same label and description', () => {
    const blocks = [
      {
        type: 'tool_use',
        name: 'AskUserQuestion',
        input: {
          questions: [
            {
              question: 'Continue?',
              options: [
                { label: 'Yes', description: 'Yes' },
              ],
            },
          ],
        },
      },
    ]
    const result = extractSpeakableText(blocks)
    // When label === description, only label should be shown
    expect(result).toContain('Yes')
    expect(result).not.toContain('Yes — Yes')
  })

  it('ignores non-AskUserQuestion tool_use blocks', () => {
    const blocks = [
      {
        type: 'tool_use',
        name: 'Read',
        input: { file_path: '/some/file.go' },
      },
    ]
    expect(extractSpeakableText(blocks)).toBe('')
  })

  it('ignores tool_result blocks', () => {
    const blocks = [
      { type: 'tool_result', content: 'some output' },
    ]
    expect(extractSpeakableText(blocks)).toBe('')
  })

  it('handles questions without header', () => {
    const blocks = [
      {
        type: 'tool_use',
        name: 'AskUserQuestion',
        input: {
          questions: [
            { question: 'What is your name?' },
          ],
        },
      },
    ]
    const result = extractSpeakableText(blocks)
    expect(result).toBe('What is your name?')
  })

  it('handles questions without options', () => {
    const blocks = [
      {
        type: 'tool_use',
        name: 'AskUserQuestion',
        input: {
          questions: [
            { question: 'Please confirm?' },
          ],
        },
      },
    ]
    const result = extractSpeakableText(blocks)
    expect(result).toBe('Please confirm?')
  })

  it('mixes text and question blocks', () => {
    const blocks = [
      { type: 'text', text: 'Here is a question:' },
      {
        type: 'tool_use',
        name: 'AskUserQuestion',
        input: {
          questions: [
            { question: 'Continue?', options: ['Yes', 'No'] },
          ],
        },
      },
    ]
    const result = extractSpeakableText(blocks)
    expect(result).toContain('Here is a question:')
    expect(result).toContain('Continue?')
    expect(result).toContain('Yes, No')
  })

  it('returns empty string for empty blocks', () => {
    expect(extractSpeakableText([])).toBe('')
  })

  it('handles blocks with missing text property', () => {
    const blocks = [
      { type: 'text' },
    ]
    expect(extractSpeakableText(blocks)).toBe('')
  })

  it('handles AskUserQuestion with missing input.questions', () => {
    const blocks = [
      {
        type: 'tool_use',
        name: 'AskUserQuestion',
        input: {},
      },
    ]
    expect(extractSpeakableText(blocks)).toBe('')
  })

  it('handles AskUserQuestion with empty options array', () => {
    const blocks = [
      {
        type: 'tool_use',
        name: 'AskUserQuestion',
        input: {
          questions: [
            { question: 'Proceed?', options: [] },
          ],
        },
      },
    ]
    const result = extractSpeakableText(blocks)
    expect(result).toBe('Proceed?')
  })

  it('handles object options without description', () => {
    const blocks = [
      {
        type: 'tool_use',
        name: 'AskUserQuestion',
        input: {
          questions: [
            {
              question: 'Choose:',
              options: [{ label: 'OK' }],
            },
          ],
        },
      },
    ]
    const result = extractSpeakableText(blocks)
    expect(result).toContain('OK')
  })

  it('trims final result', () => {
    const blocks = [
      { type: 'text', text: '  Hello  ' },
    ]
    const result = extractSpeakableText(blocks)
    expect(result).toBe('Hello')
  })
})

// ── Regression: MSE streaming with Infinity/0/NaN duration — stall fallback detection ──
// Bug: When MSE endOfStream() is called but audio.duration stays Infinity
// (common on Android WebView), or when duration is 0/NaN on some browsers,
// the old fallback timer silently skipped because of the !isFinite(audio.duration)
// or falsy-duration guard. If onended also didn't fire, the "朗读中" state
// got stuck forever.
// Fix: When duration is 0/NaN/Infinity, detect playback end via currentTime stall
// (with or without buffered info), independent of duration being finite.

describe('useAutoSpeech — regression: duration unavailable stall detection', () => {
  let fetchSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchSpy = vi.fn()
    globalThis.fetch = fetchSpy
    toastShowMock.mockClear()
    mseIsSupportedMock.mockReturnValue(false)
    const { stopAudio } = useAutoSpeech()
    stopAudio()
  })

  afterEach(() => {
    const { stopAudio } = useAutoSpeech()
    stopAudio()
    vi.restoreAllMocks()
  })

  it('duration=0, no buffered info: pure stall detection resets state', async () => {
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ cached: true, audioPath: '/tts/test.mp3' }),
    })
    const timeupdateHandlers: Array<() => void> = []
    const audioInstances: any[] = []
    vi.stubGlobal('Audio', vi.fn(function(this: any) {
      this.play = vi.fn().mockResolvedValue(undefined)
      this.pause = vi.fn()
      this.onended = null
      this.onerror = null
      this.currentTime = 0
      this.duration = 0
      this.buffered = { length: 0 }
      this.addEventListener = vi.fn((event: string, handler: () => void) => {
        if (event === 'timeupdate') timeupdateHandlers.push(handler)
      })
      this.removeEventListener = vi.fn()
      audioInstances.push(this)
    }))

    const { speakText, state, isActive } = useAutoSpeech()
    speakText('300', 'Duration zero test')
    await vi.waitFor(() => expect(audioInstances.length).toBeGreaterThan(0))

    const audio = audioInstances[0]
    expect(state.value).toBe('playing')

    // Simulate: audio has played to some point, now stalled
    audio.currentTime = 5.0
    // First call sets lastCurrentTime=5.0 (no stall yet since lastCurrentTime was 0)
    // Subsequent 5 calls detect stall (currentTime doesn't advance)
    for (let i = 0; i < 5; i++) {
      timeupdateHandlers[0]()
      expect(state.value).toBe('playing')
    }
    // 6th call: stallCount reaches 5, triggers reset
    timeupdateHandlers[0]()
    expect(state.value).toBe('idle')
    expect(isActive('300')).toBe(false)
  })

  it('Infinity duration with buffered info: stall near buffer end resets state', async () => {
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ cached: true, audioPath: '/tts/test.mp3' }),
    })
    const timeupdateHandlers: Array<() => void> = []
    const audioInstances: any[] = []
    vi.stubGlobal('Audio', vi.fn(function(this: any) {
      this.play = vi.fn().mockResolvedValue(undefined)
      this.pause = vi.fn()
      this.onended = null
      this.onerror = null
      this.currentTime = 0
      this.duration = Infinity
      this.buffered = {
        length: 1,
        start: (_i: number) => 0,
        end: (_i: number) => 10,
      }
      this.addEventListener = vi.fn((event: string, handler: () => void) => {
        if (event === 'timeupdate') timeupdateHandlers.push(handler)
      })
      this.removeEventListener = vi.fn()
      audioInstances.push(this)
    }))

    const { speakText, state, isActive } = useAutoSpeech()
    speakText('301', 'Infinity duration buffered test')
    await vi.waitFor(() => expect(audioInstances.length).toBeGreaterThan(0))

    const audio = audioInstances[0]
    expect(state.value).toBe('playing')

    // Simulate: playback reached near buffer end and stalled
    audio.currentTime = 9.8
    // First call sets lastCurrentTime=9.8, subsequent 5 calls detect stall
    for (let i = 0; i < 5; i++) {
      timeupdateHandlers[0]()
      expect(state.value).toBe('playing')
    }
    // 6th call: stallCount reaches 5, triggers reset
    timeupdateHandlers[0]()
    expect(state.value).toBe('idle')
    expect(isActive('301')).toBe(false)
  })

  it('Infinity duration, onended fires: primary mechanism still works', async () => {
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ cached: true, audioPath: '/tts/test.mp3' }),
    })
    const timeupdateHandlers: Array<() => void> = []
    const audioInstances: any[] = []
    vi.stubGlobal('Audio', vi.fn(function(this: any) {
      this.play = vi.fn().mockResolvedValue(undefined)
      this.pause = vi.fn()
      this.onended = null
      this.onerror = null
      this.currentTime = 0
      this.duration = Infinity
      this.buffered = { length: 0 }
      this.addEventListener = vi.fn((event: string, handler: () => void) => {
        if (event === 'timeupdate') timeupdateHandlers.push(handler)
      })
      this.removeEventListener = vi.fn()
      audioInstances.push(this)
    }))

    const { speakText, state, isActive } = useAutoSpeech()
    speakText('302', 'Infinity onended test')
    await vi.waitFor(() => expect(audioInstances.length).toBeGreaterThan(0))

    const audio = audioInstances[0]
    expect(state.value).toBe('playing')

    // onended is the primary reset mechanism and still works
    audio.onended!()
    expect(state.value).toBe('idle')
    expect(isActive('302')).toBe(false)
  })

  it('finite duration: fallback still works with new code structure', async () => {
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ cached: true, audioPath: '/tts/test.mp3' }),
    })
    const timeupdateHandlers: Array<() => void> = []
    const audioInstances: any[] = []
    vi.stubGlobal('Audio', vi.fn(function(this: any) {
      this.play = vi.fn().mockResolvedValue(undefined)
      this.pause = vi.fn()
      this.onended = null
      this.onerror = null
      this.currentTime = 0
      this.duration = 10
      this.buffered = { length: 0 }
      this.addEventListener = vi.fn((event: string, handler: () => void) => {
        if (event === 'timeupdate') timeupdateHandlers.push(handler)
      })
      this.removeEventListener = vi.fn()
      audioInstances.push(this)
    }))

    const { speakText, state, isActive } = useAutoSpeech()
    speakText('303', 'Finite duration fallback test')
    await vi.waitFor(() => expect(audioInstances.length).toBeGreaterThan(0))

    const audio = audioInstances[0]
    expect(state.value).toBe('playing')

    // Near the end of finite-duration audio → fallback detects completion
    audio.currentTime = 9.7
    timeupdateHandlers[0]()

    expect(state.value).toBe('idle')
    expect(isActive('303')).toBe(false)
  })
})

// ── TTS generation with messageId ──

describe('useAutoSpeech._speak — TTS body includes messageId', () => {
  let fetchSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchSpy = vi.fn()
    globalThis.fetch = fetchSpy
  })

  afterEach(() => {
    // Stop any audio/state left over from tests
    const { stopAudio } = useAutoSpeech()
    stopAudio()
    vi.restoreAllMocks()
  })

  it('includes messageId in TTS request body when id is numeric', async () => {
    // Mock a cached TTS response (simplest path: no EventSource needed)
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ cached: true, audioPath: '/tts/test.mp3' }),
    })

    // Mock Audio constructor to prevent actual playback
    const mockAudio = { play: vi.fn().mockResolvedValue(undefined), pause: vi.fn() }
    vi.stubGlobal('Audio', vi.fn(() => mockAudio))

    const { speakText } = useAutoSpeech()
    await speakText('42', 'Hello world')

    // Verify fetch was called with messageId in the body
    expect(fetchSpy).toHaveBeenCalledTimes(1)
    const [url, options] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/tts/generate')
    const body = JSON.parse(options.body)
    expect(body.text).toBe('Hello world')
    expect(body.messageId).toBe(42)
  })

  it('omits messageId when id is not a numeric string', async () => {
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ cached: true, audioPath: '/tts/test.mp3' }),
    })

    const mockAudio = { play: vi.fn().mockResolvedValue(undefined), pause: vi.fn() }
    vi.stubGlobal('Audio', vi.fn(() => mockAudio))

    const { speakText } = useAutoSpeech()
    await speakText('abc-123', 'Test text')

    expect(fetchSpy).toHaveBeenCalledTimes(1)
    const [, options] = fetchSpy.mock.calls[0]
    const body = JSON.parse(options.body)
    expect(body.text).toBe('Test text')
    expect(body.messageId).toBeUndefined()
  })
})

// ── Toggle toast notifications ──

describe('useAutoSpeech.toggle — toast notifications', () => {
  beforeEach(() => {
    toastShowMock.mockClear()
  })

  it('shows disabled toast when toggling off', () => {
    const { toggle, enabled } = useAutoSpeech()
    // Start enabled, toggle off
    enabled.value = true
    toggle()
    expect(enabled.value).toBe(false)
    expect(toastShowMock).toHaveBeenCalledWith('autoSpeech.disabled', expect.objectContaining({ icon: 'mute' }))
  })

  it('shows enabled toast when toggling on', () => {
    const { toggle, enabled } = useAutoSpeech()
    // Start disabled, toggle on
    enabled.value = false
    toggle()
    expect(enabled.value).toBe(true)
    expect(toastShowMock).toHaveBeenCalledWith('autoSpeech.enabled', expect.objectContaining({ icon: 'sound' }))
  })
})

// ── Autospeech-change event toast ──

describe('useAutoSpeech — autospeech-change event toast', () => {
  beforeEach(() => {
    toastShowMock.mockClear()
  })

  it('shows enabled toast when event detail is true', () => {
    window.dispatchEvent(new CustomEvent('clawbench-autospeech-change', { detail: true }))
    expect(toastShowMock).toHaveBeenCalledWith('autoSpeech.enabled', expect.objectContaining({ icon: 'sound' }))
  })

  it('shows disabled toast when event detail is false', () => {
    window.dispatchEvent(new CustomEvent('clawbench-autospeech-change', { detail: false }))
    expect(toastShowMock).toHaveBeenCalledWith('autoSpeech.disabled', expect.objectContaining({ icon: 'mute' }))
  })
})

// ── Streaming TTS path ──

const { mseIsSupportedMock } = vi.hoisted(() => ({
  mseIsSupportedMock: vi.fn(() => false),
}))

vi.mock('@/composables/useMseAudio', () => {
  return {
    // Must be constructible (`new MseAudioPlayer()`): an arrow function throws
    // "not a constructor", which would silently divert the WS path into the
    // catch branch. Returning an object from a `function` expression keeps
    // `new` working while still allowing the object to be inspected.
    MseAudioPlayer: Object.assign(
      vi.fn(function (this: unknown) {
        return {
          init: vi.fn(() => ({
            play: vi.fn().mockResolvedValue(undefined),
            pause: vi.fn(),
            onended: null,
            onerror: null,
            currentTime: 0,
            duration: 0,
            addEventListener: vi.fn(),
            removeEventListener: vi.fn(),
          })),
          appendChunk: vi.fn(),
          endOfStream: vi.fn(),
          cleanup: vi.fn(),
          isReady: false,
        }
      }),
      { isSupported: mseIsSupportedMock },
    ),
  }
})

describe('useAutoSpeech — streaming TTS path', () => {
  let fetchSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchSpy = vi.fn()
    globalThis.fetch = fetchSpy
    toastShowMock.mockClear()
    mseIsSupportedMock.mockReturnValue(false) // default: no MSE → SSE fallback
  })

  afterEach(() => {
    // Stop any audio/state left over from tests
    const { stopAudio } = useAutoSpeech()
    stopAudio()
    vi.restoreAllMocks()
  })

  it('creates EventSource for SSE fallback when streaming but no MSE', async () => {
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ streaming: true, jobId: 'deadbeef1234' }),
    })

    const mockEs = { close: vi.fn(), addEventListener: vi.fn() }
    const EventSourceMock = vi.fn(() => mockEs)
    vi.stubGlobal('EventSource', EventSourceMock)

    const { speakText } = useAutoSpeech()
    speakText('1', 'Stream this')

    // _speak is fire-and-forget, so we need to wait for async resolution
    await vi.waitFor(() => {
      expect(fetchSpy).toHaveBeenCalledTimes(1)
      expect(EventSourceMock).toHaveBeenCalledWith('/api/tts/stream/deadbeef1234')
    })

    // Clean up: close the EventSource to prevent open handle leak
    mockEs.close()
  })

  it('uses SSE path when data.streaming is false', async () => {
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ streaming: false, jobId: 'cafebabe5678' }),
    })

    const mockEs = { close: vi.fn(), addEventListener: vi.fn() }
    const EventSourceMock = vi.fn(() => mockEs)
    vi.stubGlobal('EventSource', EventSourceMock)

    const { speakText } = useAutoSpeech()
    speakText('2', 'Non-stream TTS')

    await vi.waitFor(() => {
      expect(EventSourceMock).toHaveBeenCalledWith('/api/tts/stream/cafebabe5678')
    })

    // Clean up: close the EventSource to prevent open handle leak
    mockEs.close()
  })

  it('stopAudio pauses audio and resets state', async () => {
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ cached: true, audioPath: '/tts/test.mp3' }),
    })

    const pauseFn = vi.fn()
    const playFn = vi.fn().mockResolvedValue(undefined)
    // Use a class-style mock so 'new Audio()' works correctly
    const audioInstances: any[] = []
    vi.stubGlobal('Audio', vi.fn(function(this: any) {
      this.play = playFn
      this.pause = pauseFn
      this.onended = null
      this.onerror = null
      this.currentTime = 0
      audioInstances.push(this)
    }))

    const { speakText, stopAudio, state } = useAutoSpeech()
    // Reset any leftover singleton state from previous tests
    stopAudio()

    speakText('3', 'Stop test')

    await vi.waitFor(() => {
      expect(audioInstances.length).toBeGreaterThan(0)
    })

    stopAudio()

    expect(pauseFn).toHaveBeenCalled()
    expect(state.value).toBe('idle')
  })
})

// ── WebSocket teardown without a terminal frame ──
//
// Regression: the server can close the TTS WebSocket without ever sending a
// done/error frame (e.g. the terminal frame was dropped in the handler's
// teardown race). Previously ws.onclose only nulled currentWs, leaving the UI
// stuck on "合成中" forever. It must now reset state and surface an error.
describe('useAutoSpeech — TTS WebSocket closes without a terminal frame', () => {
  let fetchSpy: ReturnType<typeof vi.fn>

  /** Minimal fake WebSocket that lets the test drive lifecycle events. */
  class FakeWebSocket {
    static instances: FakeWebSocket[] = []
    binaryType = ''
    onopen: (() => void) | null = null
    onmessage: ((e: { data: unknown }) => void) | null = null
    onerror: (() => void) | null = null
    onclose: (() => void) | null = null
    sent: string[] = []
    closed = false

    constructor(public url: string) {
      FakeWebSocket.instances.push(this)
    }
    send(data: string) { this.sent.push(data) }
    close() { this.closed = true }
  }

  beforeEach(() => {
    FakeWebSocket.instances = []
    vi.stubGlobal('WebSocket', FakeWebSocket)
    fetchSpy = vi.fn()
    globalThis.fetch = fetchSpy
    toastShowMock.mockClear()
    mseIsSupportedMock.mockReturnValue(true) // force the WS path
    const { stopAudio } = useAutoSpeech()
    stopAudio()
  })

  afterEach(() => {
    const { stopAudio } = useAutoSpeech()
    stopAudio()
    vi.restoreAllMocks()
  })

  async function startStreaming(): Promise<FakeWebSocket> {
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ streaming: true, jobId: 'deadbeef1234' }),
    })
    const { speakText } = useAutoSpeech()
    speakText('1', 'Stream this')
    await vi.waitFor(() => {
      expect(FakeWebSocket.instances.length).toBe(1)
    })
    return FakeWebSocket.instances[0]
  }

  it('resets state and reports an error when closed before any frame', async () => {
    const ws = await startStreaming()
    const { state, lastError } = useAutoSpeech()
    expect(state.value).toBe('synthesizing')

    ws.onclose?.()

    expect(state.value).toBe('idle')
    expect(lastError.value).toBe('autoSpeech.generateFailedGeneric')
    expect(toastShowMock).toHaveBeenCalled()
  })

  it('does not double-report when a terminal error frame was received', async () => {
    const ws = await startStreaming()
    const { state } = useAutoSpeech()

    ws.onmessage?.({ data: JSON.stringify({ type: 'error', message: 'boom' }) })
    expect(state.value).toBe('idle')
    const callsAfterFrame = toastShowMock.mock.calls.length

    // The subsequent close must not fire a second toast.
    ws.onclose?.()
    expect(toastShowMock.mock.calls.length).toBe(callsAfterFrame)
  })

  it('does not reset when audio already started streaming', async () => {
    const ws = await startStreaming()
    const { state } = useAutoSpeech()

    // First binary chunk advances to 'playing'.
    ws.onmessage?.({ data: new ArrayBuffer(8) })
    expect(state.value).toBe('playing')
    const callsBeforeClose = toastShowMock.mock.calls.length

    // A close after audio began is normal completion territory — leave it to
    // the playback-end timer, don't yank the state to idle.
    ws.onclose?.()
    expect(state.value).toBe('playing')
    expect(toastShowMock.mock.calls.length).toBe(callsBeforeClose)
  })
})

// ── Error paths and exported query functions ──

describe('useAutoSpeech — error paths and query functions', () => {
  let fetchSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchSpy = vi.fn()
    globalThis.fetch = fetchSpy
    toastShowMock.mockClear()
    mseIsSupportedMock.mockReturnValue(false)
    // Reset singleton state before each test
    const { stopAudio } = useAutoSpeech()
    stopAudio()
  })

  afterEach(() => {
    const { stopAudio } = useAutoSpeech()
    stopAudio()
    vi.clearAllMocks()
  })

  it('speakMessage returns early when disabled', async () => {
    const { speakMessage, enabled, stopAudio } = useAutoSpeech()
    stopAudio()
    enabled.value = false
    await speakMessage('1', 'Hello')
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  it('speakMessage calls _speak when enabled', async () => {
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ cached: true, audioPath: '/tts/test.mp3' }),
    })
    const mockAudio = { play: vi.fn().mockResolvedValue(undefined), pause: vi.fn(), onended: null, onerror: null }
    vi.stubGlobal('Audio', vi.fn(() => mockAudio))

    const { speakMessage, enabled, stopAudio } = useAutoSpeech()
    stopAudio()
    enabled.value = true
    await speakMessage('2', 'Hello')
    expect(fetchSpy).toHaveBeenCalledTimes(1)
  })

  it('shows error when fetch returns non-OK response', async () => {
    fetchSpy.mockResolvedValueOnce({
      ok: false,
      status: 500,
      text: async () => JSON.stringify({ error: { detail: 'Server error' } }),
    })

    const { speakText, stopAudio } = useAutoSpeech()
    stopAudio()
    await speakText('1', 'Test error')

    expect(toastShowMock).toHaveBeenCalled()
  })

  it('shows error toast on network failure', async () => {
    fetchSpy.mockRejectedValueOnce(new TypeError('Failed to fetch'))

    const { speakText, stopAudio } = useAutoSpeech()
    stopAudio()
    await speakText('1', 'Network error')

    expect(toastShowMock).toHaveBeenCalled()
  })

  it('isActive returns false for non-matching id', () => {
    const { isActive } = useAutoSpeech()
    expect(isActive('999')).toBe(false)
  })

  it('getSummary returns empty string when not active', () => {
    const { getSummary } = useAutoSpeech()
    expect(getSummary('nonexistent')).toBe('')
  })

  it('handles empty text in _speak', async () => {
    const { speakText, stopAudio } = useAutoSpeech()
    stopAudio()
    await speakText('1', '')
    // Should return early without calling fetch
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  it('getPhaseLabel returns empty string for idle state', () => {
    const { getPhaseLabel, stopAudio } = useAutoSpeech()
    stopAudio()
    expect(getPhaseLabel('1')).toBe('')
  })
})

// ── Regression: TTS "朗读中" stuck state (ended event not firing) ──
// Bug: After playback finishes, the "朗读中" state never clears because
// some browsers (notably Android WebView) don't fire the 'ended' event.
// Fix: Fallback timer + timeupdate listener detect playback completion.

describe('useAutoSpeech — regression: state resets when ended event missed', () => {
  let fetchSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchSpy = vi.fn()
    globalThis.fetch = fetchSpy
    toastShowMock.mockClear()
    mseIsSupportedMock.mockReturnValue(false)
    const { stopAudio } = useAutoSpeech()
    stopAudio()
  })

  afterEach(() => {
    const { stopAudio } = useAutoSpeech()
    stopAudio()
    vi.restoreAllMocks()
  })

  // Helper: set up a cached TTS response and mock Audio that captures
  // timeupdate handlers so we can invoke them directly in tests.
  // Must use 'function' syntax for vi.fn to properly mock 'new Audio()'.
  function setupCachedAudioWithHandlers() {
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ cached: true, audioPath: '/tts/test.mp3' }),
    })
    const timeupdateHandlers: Array<() => void> = []
    const audioInstances: any[] = []
    vi.stubGlobal('Audio', vi.fn(function(this: any) {
      this.play = vi.fn().mockResolvedValue(undefined)
      this.pause = vi.fn()
      this.onended = null
      this.onerror = null
      this.currentTime = 0
      this.duration = 10
      this.addEventListener = vi.fn((event: string, handler: () => void) => {
        if (event === 'timeupdate') timeupdateHandlers.push(handler)
      })
      this.removeEventListener = vi.fn()
      audioInstances.push(this)
    }))
    return { audioInstances, timeupdateHandlers }
  }

  it('playAudio: fallback handler resets state when currentTime near duration', async () => {
    const { audioInstances, timeupdateHandlers } = setupCachedAudioWithHandlers()

    const { speakText, state, isActive } = useAutoSpeech()
    speakText('100', 'Fallback test')

    // Wait for async chain to complete and audio to start playing
    await vi.waitFor(() => expect(audioInstances.length).toBeGreaterThan(0))

    const audio = audioInstances[0]
    expect(state.value).toBe('playing')

    // Verify a timeupdate handler was registered
    expect(timeupdateHandlers.length).toBeGreaterThan(0)

    // Simulate playback reaching near the end (within 0.5s threshold)
    audio.currentTime = 9.7

    // Manually invoke the fallback handler (simulates timeupdate/setInterval)
    timeupdateHandlers[0]()

    expect(state.value).toBe('idle')
    expect(isActive('100')).toBe(false)
  })

  it('playAudio: fallback handler does not reset state during mid-playback', async () => {
    const { audioInstances, timeupdateHandlers } = setupCachedAudioWithHandlers()

    const { speakText, state, isActive } = useAutoSpeech()
    speakText('101', 'Mid-playback test')
    await vi.waitFor(() => expect(audioInstances.length).toBeGreaterThan(0))

    const audio = audioInstances[0]
    expect(state.value).toBe('playing')

    // currentTime is 0, far from duration=10
    audio.currentTime = 3.0

    // Invoke the handler — should NOT reset because currentTime < duration - 0.5
    timeupdateHandlers[0]()

    expect(state.value).toBe('playing')
    expect(isActive('101')).toBe(true)
  })

  it('playAudio: fallback timer is cleaned up by stopAudio', async () => {
    const { audioInstances, timeupdateHandlers } = setupCachedAudioWithHandlers()

    const { speakText, stopAudio, state } = useAutoSpeech()
    speakText('102', 'Stop cleanup test')
    await vi.waitFor(() => expect(audioInstances.length).toBeGreaterThan(0))

    expect(state.value).toBe('playing')

    stopAudio()

    // Even if we manually invoke the old handler, the identity guard
    // (currentAudioEl === audio) should prevent state corruption
    audioInstances[0].currentTime = 9.8
    timeupdateHandlers[0]()

    expect(state.value).toBe('idle')
  })

  it('playAudio: ended event resets state and clears fallback timer', async () => {
    const { audioInstances } = setupCachedAudioWithHandlers()

    const { speakText, state, isActive } = useAutoSpeech()
    speakText('103', 'Ended event test')
    await vi.waitFor(() => expect(audioInstances.length).toBeGreaterThan(0))

    expect(state.value).toBe('playing')

    // Simulate the 'ended' event firing normally
    audioInstances[0].onended!()

    expect(state.value).toBe('idle')
    expect(isActive('103')).toBe(false)
  })
})

// ── Regression: WS error paths must reset state ──
// Bug: mse.cleanup() was called but state/activeId were not reset,
// so the UI stayed stuck showing "朗读中" forever.

describe('useAutoSpeech — regression: WS error paths reset state', () => {
  let fetchSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchSpy = vi.fn()
    globalThis.fetch = fetchSpy
    toastShowMock.mockClear()
    mseIsSupportedMock.mockReturnValue(true)
    const { stopAudio } = useAutoSpeech()
    stopAudio()
  })

  afterEach(() => {
    const { stopAudio } = useAutoSpeech()
    stopAudio()
    vi.restoreAllMocks()
  })

  // Note: SSE/WS paths are hard to mock in jsdom (no EventSource, WebSocket
  // mock unreliable). We verify error state-reset behavior through the
  // _speak catch path and the SSE path with a stubbed EventSource.

  it('fetch error resets state to idle', async () => {
    fetchSpy.mockRejectedValueOnce(new TypeError('Failed to fetch'))

    const { speakText, state, isActive } = useAutoSpeech()
    await speakText('200', 'Fetch error test')

    expect(state.value).toBe('idle')
    expect(isActive('200')).toBe(false)
    expect(toastShowMock).toHaveBeenCalled()
  })

  it('non-OK response resets state to idle', async () => {
    fetchSpy.mockResolvedValueOnce({
      ok: false,
      status: 500,
      text: async () => JSON.stringify({ error: 'Server error' }),
    })

    const { speakText, state, isActive } = useAutoSpeech()
    await speakText('201', 'Non-OK test')

    expect(state.value).toBe('idle')
    expect(isActive('201')).toBe(false)
  })

  it('new _speak after error starts fresh', async () => {
    // First speak fails
    fetchSpy.mockResolvedValueOnce({
      ok: false,
      status: 500,
      text: async () => JSON.stringify({ error: 'Server error' }),
    })

    const { speakText, state, isActive } = useAutoSpeech()
    await speakText('202', 'First attempt fails')
    expect(state.value).toBe('idle')

    // Second speak also gets an error
    fetchSpy.mockResolvedValueOnce({
      ok: false,
      status: 503,
      text: async () => JSON.stringify({ error: 'Service unavailable' }),
    })

    await speakText('203', 'Second attempt')
    expect(fetchSpy).toHaveBeenCalledTimes(2)
    expect(state.value).toBe('idle')
    expect(isActive('203')).toBe(false)
    expect(isActive('202')).toBe(false)
  })
})


// ── Auto-speech sequential queue (group chat: 轮流播放) ──
// A group run triggers auto-speech once per member. Without a queue each new
// trigger preempted the previous one, so only the last speaker was ever heard.

describe('useAutoSpeech — sequential auto-speech queue', () => {
  let fetchSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchSpy = vi.fn()
    globalThis.fetch = fetchSpy
    toastShowMock.mockClear()
    mseIsSupportedMock.mockReturnValue(false)
    const { stopAudio } = useAutoSpeech()
    stopAudio()
  })

  afterEach(() => {
    const { stopAudio } = useAutoSpeech()
    stopAudio()
    vi.restoreAllMocks()
  })

  // Helper: mock Audio capturing instances + their onended handlers.
  function setupAudio() {
    const audioInstances: any[] = []
    vi.stubGlobal('Audio', vi.fn(function(this: any) {
      this.play = vi.fn().mockResolvedValue(undefined)
      this.pause = vi.fn()
      this.onended = null
      this.onerror = null
      this.currentTime = 0
      this.duration = 10
      this.addEventListener = vi.fn()
      this.removeEventListener = vi.fn()
      audioInstances.push(this)
    }))
    return audioInstances
  }

  it('plays a second auto trigger only after the first finishes', async () => {
    const audioInstances = setupAudio()
    // Two cached responses, one per speaker.
    fetchSpy.mockResolvedValue({
      ok: true,
      json: async () => ({ cached: true, audioPath: '/tts/x.mp3' }),
    })

    const { speakMessage, enabled, stopAudio } = useAutoSpeech()
    stopAudio()
    enabled.value = true

    speakMessage('1', 'speaker one')
    await vi.waitFor(() => expect(audioInstances.length).toBe(1))

    // Second speaker triggers while the first is still playing → must QUEUE,
    // not preempt (no second fetch yet).
    speakMessage('2', 'speaker two')
    expect(fetchSpy).toHaveBeenCalledTimes(1)

    // Finishing the first hands off to the queued second.
    audioInstances[0].onended()
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(2))
    await vi.waitFor(() => expect(audioInstances.length).toBe(2))
  })

  it('forwards speakerName to the backend only for the speaker that has one', async () => {
    setupAudio()
    fetchSpy.mockResolvedValue({
      ok: true,
      json: async () => ({ cached: true, audioPath: '/tts/x.mp3' }),
    })

    const { speakMessage, enabled, stopAudio } = useAutoSpeech()
    stopAudio()
    enabled.value = true

    speakMessage('7', 'hello', '张三')
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1))
    const body = JSON.parse(fetchSpy.mock.calls[0][1].body)
    expect(body.speakerName).toBe('张三')

    // No speakerName ⇒ key absent (single chat / manual read-aloud unchanged).
    const { speakText } = useAutoSpeech()
    speakText('8', 'single chat')
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(2))
    const body2 = JSON.parse(fetchSpy.mock.calls[1][1].body)
    expect(body2.speakerName).toBeUndefined()
  })

  it('manual speakText clears the pending auto queue (deliberate preempt)', async () => {
    const audioInstances = setupAudio()
    fetchSpy.mockResolvedValue({
      ok: true,
      json: async () => ({ cached: true, audioPath: '/tts/x.mp3' }),
    })

    const { speakMessage, speakText, enabled, stopAudio } = useAutoSpeech()
    stopAudio()
    enabled.value = true

    speakMessage('1', 'auto one')
    await vi.waitFor(() => expect(audioInstances.length).toBe(1))
    speakMessage('2', 'auto two') // queued

    // User clicks read-aloud on some message → preempts and drops the queue.
    speakText('9', 'manual')
    await vi.waitFor(() => expect(audioInstances.length).toBe(2))

    // Finish the manual playback: the queued auto-two must NOT play.
    audioInstances[1].onended()
    await new Promise((r) => setTimeout(r, 20))
    expect(audioInstances.length).toBe(2)
  })

  it('stopAudio clears the queue so a stopped run does not resume', async () => {
    const audioInstances = setupAudio()
    fetchSpy.mockResolvedValue({
      ok: true,
      json: async () => ({ cached: true, audioPath: '/tts/x.mp3' }),
    })

    const { speakMessage, stopAudio, enabled } = useAutoSpeech()
    stopAudio()
    enabled.value = true

    speakMessage('1', 'auto one')
    await vi.waitFor(() => expect(audioInstances.length).toBe(1))
    speakMessage('2', 'auto two') // queued

    stopAudio() // user stops
    // Even if the old audio were to end, nothing queued remains.
    expect(audioInstances.length).toBe(1)
    audioInstances[0].onended?.()
    await new Promise((r) => setTimeout(r, 20))
    expect(audioInstances.length).toBe(1)
  })
})

describe('useAutoSpeech — setEnabled', () => {
  it('flips the switch and confirms with a toast', () => {
    const { enabled, setEnabled } = useAutoSpeech()
    enabled.value = false
    toastShowMock.mockClear()
    setEnabled(true)
    expect(enabled.value).toBe(true)
    expect(toastShowMock).toHaveBeenCalledWith('autoSpeech.enabled', expect.anything())
  })

  it('stays silent when asked to', () => {
    // Free-dialog mode shows one combined message instead. The toast slot holds
    // a single message, so a second call would overwrite the first and the user
    // would only see whichever ran last.
    const { enabled, setEnabled } = useAutoSpeech()
    enabled.value = false
    toastShowMock.mockClear()
    setEnabled(true, { silent: true })
    expect(enabled.value).toBe(true)
    expect(toastShowMock).not.toHaveBeenCalled()
  })

  it('does nothing when the value is already right', () => {
    const { enabled, setEnabled } = useAutoSpeech()
    enabled.value = true
    toastShowMock.mockClear()
    setEnabled(true)
    expect(enabled.value).toBe(true)
    expect(toastShowMock).not.toHaveBeenCalled()
  })

  it('toggle passes the silent flag through', () => {
    const { enabled, toggle } = useAutoSpeech()
    enabled.value = false
    toastShowMock.mockClear()
    toggle({ silent: true })
    expect(enabled.value).toBe(true)
    expect(toastShowMock).not.toHaveBeenCalled()
  })
})

// ── Android system speech engine ───────────────────────────────────────────
// When the device reads with its own engine, the server is asked for the
// condensed text only. Getting this wrong is expensive in a way that is easy
// to miss: a cloud synthesis is paid for, the audio is thrown away, and the
// user hears a different voice than the one they chose.

describe('useAutoSpeech — Android system speech engine', () => {
  let fetchSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchSpy = vi.fn()
    globalThis.fetch = fetchSpy
    localConfig.systemSpeech = true
    toastShowMock.mockClear()
    nativeTtsMock.ttsSpeak.mockClear()
    nativeTtsMock.ttsStop.mockClear()
    nativeTtsMock.ttsStatus.mockReset()
    nativeTtsMock.ttsStatus.mockReturnValue('ready')
    nativeTtsMock.ttsSpeak.mockReturnValue('ready')
    useAutoSpeech().stopAudio()
  })

  afterEach(() => {
    useAutoSpeech().stopAudio()
    localConfig.systemSpeech = false
  })

  it('asks for the summary only and reads it with the phone engine', async () => {
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ summary: '核心结论', speakText: '核心结论' }),
    })

    const api = useAutoSpeech()
    api.speakText('42', '一段很长的原文')

    await vi.waitFor(() => expect(nativeTtsMock.ttsSpeak).toHaveBeenCalled())

    const body = JSON.parse(fetchSpy.mock.calls[0][1].body)
    expect(body.summarizeOnly).toBe(true)
    expect(body.messageId).toBe(42)
    expect(body.text).toBe('一段很长的原文')

    expect(nativeTtsMock.ttsSpeak).toHaveBeenCalledWith('核心结论', '42')
    expect(api.getSummary('42')).toBe('核心结论')
  })

  it('speaks speakText but shows the clean summary', async () => {
    // Group chat: the server prefixes the SPOKEN text with "<name>说：" and
    // keeps the summary clean for the UI chip.
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ summary: '同意', speakText: '小明说：同意' }),
    })

    const api = useAutoSpeech()
    api.speakText('7', '我同意')

    await vi.waitFor(() => expect(nativeTtsMock.ttsSpeak).toHaveBeenCalledWith('小明说：同意', '7'))
    expect(api.getSummary('7')).toBe('同意')
  })

  it('matches native events when the message id is a number', async () => {
    // Chat messages carry `string | number` ids, but the bridge echoes a
    // string. Comparing them with `!==` drops every event, so the read looks
    // like it never starts and only the watchdog fires.
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ summary: '结论', speakText: '结论' }),
    })

    const api = useAutoSpeech()
    api.speakText(42 as any, '原文')
    await vi.waitFor(() => expect(nativeTtsMock.ttsSpeak).toHaveBeenCalledWith('结论', '42'))

    window.dispatchEvent(new CustomEvent('clawbench-tts-state', { detail: { id: '42', state: 'start' } }))
    expect(api.isPlayingAudio(42 as any)).toBe(true)

    window.dispatchEvent(new CustomEvent('clawbench-tts-state', { detail: { id: '42', state: 'done' } }))
    await vi.waitFor(() => expect(api.isActive(42 as any)).toBe(false))
  })

  it('goes to playing on start and finishes on done', async () => {
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ summary: '结论', speakText: '结论' }),
    })

    const api = useAutoSpeech()
    api.speakText('42', '原文')
    await vi.waitFor(() => expect(nativeTtsMock.ttsSpeak).toHaveBeenCalled())

    window.dispatchEvent(new CustomEvent('clawbench-tts-state', { detail: { id: '42', state: 'start' } }))
    expect(api.isPlayingAudio('42')).toBe(true)

    window.dispatchEvent(new CustomEvent('clawbench-tts-state', { detail: { id: '42', state: 'done' } }))
    await vi.waitFor(() => expect(api.isActive('42')).toBe(false))
  })

  it('surfaces the engine\'s reason in the failure message', async () => {
    // A generic "read-aloud failed" cannot be diagnosed from the screen; the
    // native reason is what tells us whether it is the voice, the text or the
    // engine.
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ summary: '结论', speakText: '结论' }),
    })

    const api = useAutoSpeech()
    api.speakText('42', '原文')
    await vi.waitFor(() => expect(nativeTtsMock.ttsSpeak).toHaveBeenCalled())

    window.dispatchEvent(new CustomEvent('clawbench-tts-state', {
      detail: { id: '42', state: 'error', message: 'speak_failed' },
    }))

    expect(toastShowMock).toHaveBeenCalledWith(
      'autoSpeech.systemSpeechFailed（speak_failed）',
      expect.anything(),
    )
    expect(api.isActive('42')).toBe(false)
    // The choice stays as the user made it — no silent switch to the server.
    expect(localConfig.systemSpeech).toBe(true)
  })

  it('ignores events for a different utterance id', async () => {
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ summary: '结论', speakText: '结论' }),
    })

    const api = useAutoSpeech()
    api.speakText('42', '原文')
    await vi.waitFor(() => expect(nativeTtsMock.ttsSpeak).toHaveBeenCalled())

    window.dispatchEvent(new CustomEvent('clawbench-tts-state', { detail: { id: '99', state: 'done' } }))
    expect(api.isActive('42')).toBe(true)
  })

  it('stops the phone engine when playback is stopped', async () => {
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ summary: '结论', speakText: '结论' }),
    })

    const api = useAutoSpeech()
    api.speakText('42', '原文')
    await vi.waitFor(() => expect(nativeTtsMock.ttsSpeak).toHaveBeenCalled())

    api.stopAudio()
    expect(nativeTtsMock.ttsStop).toHaveBeenCalled()
  })

  it('never falls back to the server when the phone engine is unavailable', async () => {
    // The user picked the phone voice on purpose. Switching to the server
    // behind their back makes it impossible to tell which voice spoke, so a
    // failure must end the read instead.
    nativeTtsMock.ttsStatus.mockReturnValue('unavailable')

    const api = useAutoSpeech()
    api.speakText('42', '原文')

    await vi.waitFor(() => expect(toastShowMock).toHaveBeenCalledWith(
      'settings.items.systemSpeechNoEngine',
      expect.anything(),
    ))

    expect(fetchSpy).not.toHaveBeenCalled()
    expect(nativeTtsMock.ttsSpeak).not.toHaveBeenCalled()
    expect(localConfig.systemSpeech).toBe(true)
    expect(api.isActive('42')).toBe(false)
  })
})

// ── Session switch ──

describe('useAutoSpeech — switching sessions stops the read-aloud', () => {
  /**
   * Playback is a module singleton with ONE queue. Leaving it running across a
   * session switch made the NEW session's reply queue up behind the old one
   * instead of being spoken, so the conversation on screen went silent while
   * the abandoned one kept talking — reported as "read-aloud stopped working".
   */
  async function switchTo(sessionId: string) {
    const { useSessionIdentity } = await import('@/composables/useSessionIdentity')
    useSessionIdentity().currentSessionId.value = sessionId
    // The watcher is default-flush (pre), so it runs on the microtask queue.
    await Promise.resolve()
    await Promise.resolve()
  }

  it('stops a read-aloud that belongs to the session being left', async () => {
    const fetchSpy = vi.fn()
    globalThis.fetch = fetchSpy
    fetchSpy.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ cached: true, audioPath: '/tts/a.mp3' }),
    })
    const pause = vi.fn()
    vi.stubGlobal('Audio', vi.fn(function (this: any) {
      this.play = vi.fn().mockResolvedValue(undefined)
      this.pause = pause
      this.currentTime = 0
      this.duration = 0
      this.buffered = { length: 0 }
      this.addEventListener = vi.fn()
      this.removeEventListener = vi.fn()
      this.onended = null
      this.onerror = null
    }))

    const api = useAutoSpeech()
    api.enabled.value = true
    api.stopAudio()
    await switchTo('session-a')
    api.speakMessage('1', '会话 A 的回复')
    await vi.waitFor(() => expect(api.isBusy()).toBe(true))

    await switchTo('session-b')

    expect(api.isBusy(), 'the old session must not keep the player busy').toBe(false)
    expect(pause, 'the audio element must actually be paused').toHaveBeenCalled()
    vi.unstubAllGlobals()
  })

  it('frees the player so the new session is not queued behind the old one', async () => {
    // The observable bug: with the old read-aloud still "busy", the new reply
    // was appended to the queue and never played until the previous one
    // drained. The player must be idle the moment the switch happens, so the
    // next `speakMessage` starts immediately instead of queueing.
    const fetchSpy = vi.fn()
    globalThis.fetch = fetchSpy
    fetchSpy.mockResolvedValue({ cached: true, audioPath: '/tts/x.mp3' })
    vi.stubGlobal('Audio', vi.fn(function (this: any) {
      this.play = vi.fn().mockResolvedValue(undefined)
      this.pause = vi.fn()
      this.currentTime = 0
      this.duration = 0
      this.buffered = { length: 0 }
      this.addEventListener = vi.fn()
      this.removeEventListener = vi.fn()
      this.onended = null
      this.onerror = null
    }))

    const api = useAutoSpeech()
    api.enabled.value = true
    api.stopAudio()
    await switchTo('session-a')
    api.speakMessage('1', 'A 的回复')
    await vi.waitFor(() => expect(api.isBusy()).toBe(true))

    await switchTo('session-b')

    // Nothing of A's is in flight any more, so B's reply cannot be queued.
    expect(api.isBusy()).toBe(false)
    expect(api.state.value).toBe('idle')
    vi.unstubAllGlobals()
  })
})
