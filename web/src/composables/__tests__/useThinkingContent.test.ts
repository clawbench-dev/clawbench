import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { useThinkingContent } from '@/composables/useThinkingContent'

describe('useThinkingContent', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    useThinkingContent().clearThinkingCache()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('fetches and caches thinking text by think_id', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ think_id: 'th_1', text: 'deep reasoning' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { loadThinking } = useThinkingContent()
    const text = await loadThinking('th_1', 42)
    expect(text).toBe('deep reasoning')
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/ai/chat/thinking?think_id=th_1&message_id=42',
    )

    // Second call hits cache — no second fetch
    const text2 = await loadThinking('th_1', 42)
    expect(text2).toBe('deep reasoning')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('dedupes concurrent fetches for the same think_id', async () => {
    let resolveFetch: (v: unknown) => void
    const fetchMock = vi.fn().mockImplementation(
      () => new Promise((resolve) => {
        resolveFetch = resolve
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    const { loadThinking } = useThinkingContent()
    const p1 = loadThinking('th_1', 42)
    const p2 = loadThinking('th_1', 42)
    resolveFetch!({ ok: true, json: async () => ({ think_id: 'th_1', text: 'x' }) })
    const [r1, r2] = await Promise.all([p1, p2])
    expect(r1).toBe('x')
    expect(r2).toBe('x')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('appends session_id when provided and reports errors', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 404 })
    vi.stubGlobal('fetch', fetchMock)

    const { loadThinking, errors } = useThinkingContent()
    await expect(loadThinking('th_1', 42, 'sess-9')).rejects.toThrow()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/ai/chat/thinking?think_id=th_1&message_id=42&session_id=sess-9',
    )
    expect(errors.value['th_1']).toBeTruthy()
  })

  it('refetches after a failed load (in-flight cleanup)', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: false, status: 404 })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ think_id: 'th_1', text: 'recovered' }) })
    vi.stubGlobal('fetch', fetchMock)

    const { loadThinking } = useThinkingContent()
    await expect(loadThinking('th_1', 42)).rejects.toThrow()
    const text = await loadThinking('th_1', 42)
    expect(text).toBe('recovered')
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('clearThinkingCache clears cached text', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ think_id: 'th_1', text: 'deep reasoning' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { loadThinking, cachedText, clearThinkingCache } = useThinkingContent()
    await loadThinking('th_1', 42)
    expect(cachedText('th_1')).toBe('deep reasoning')
    clearThinkingCache()
    expect(cachedText('th_1')).toBeUndefined()
  })
})
