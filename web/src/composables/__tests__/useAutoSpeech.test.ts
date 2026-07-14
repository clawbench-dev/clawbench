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

// ── TTS generation with messageId ──

describe('useAutoSpeech._speak — TTS body includes messageId', () => {
  let fetchSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchSpy = vi.fn()
    globalThis.fetch = fetchSpy
  })

  afterEach(() => {
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
        init: vi.fn(() => ({ play: vi.fn().mockResolvedValue(undefined), pause: vi.fn() })),
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
