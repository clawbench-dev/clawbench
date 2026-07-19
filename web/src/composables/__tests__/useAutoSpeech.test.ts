import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { extractSpeakableText, useAutoSpeech } from '@/composables/useAutoSpeech'

// vi.hoisted runs before vi.mock hoisting, so the mock factory can reference it
const { toastShowMock } = vi.hoisted(() => ({
  toastShowMock: vi.fn(),
}))

// Mock useToast since it's used at module level
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: toastShowMock }),
}))

// Mock useLocale
vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

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
    expect(toastShowMock).toHaveBeenCalledWith('autoSpeech.disabled', expect.objectContaining({ icon: '🔇' }))
  })

  it('shows enabled toast when toggling on', () => {
    const { toggle, enabled } = useAutoSpeech()
    // Start disabled, toggle on
    enabled.value = false
    toggle()
    expect(enabled.value).toBe(true)
    expect(toastShowMock).toHaveBeenCalledWith('autoSpeech.enabled', expect.objectContaining({ icon: '🔊' }))
  })
})

// ── Autospeech-change event toast ──

describe('useAutoSpeech — autospeech-change event toast', () => {
  beforeEach(() => {
    toastShowMock.mockClear()
  })

  it('shows enabled toast when event detail is true', () => {
    window.dispatchEvent(new CustomEvent('clawbench-autospeech-change', { detail: true }))
    expect(toastShowMock).toHaveBeenCalledWith('autoSpeech.enabled', expect.objectContaining({ icon: '🔊' }))
  })

  it('shows disabled toast when event detail is false', () => {
    window.dispatchEvent(new CustomEvent('clawbench-autospeech-change', { detail: false }))
    expect(toastShowMock).toHaveBeenCalledWith('autoSpeech.disabled', expect.objectContaining({ icon: '🔇' }))
  })
})

// ── Streaming TTS path ──

const { mseIsSupportedMock } = vi.hoisted(() => ({
  mseIsSupportedMock: vi.fn(() => false),
}))

vi.mock('@/composables/useMseAudio', () => {
  return {
    MseAudioPlayer: Object.assign(
      vi.fn(() => ({
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
      })),
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

