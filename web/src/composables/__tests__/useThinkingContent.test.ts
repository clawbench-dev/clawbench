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

  it('does not repopulate the cache from a request in flight across a clear', async () => {
    // content_reset deletes the message's chat_thinking rows, so a request that
    // was already in flight must not write its result afterwards — that would
    // resurrect exactly the reasoning that was just discarded.
    let resolveFetch: (v: unknown) => void
    const fetchMock = vi.fn().mockImplementation(
      () => new Promise((resolve) => { resolveFetch = resolve }),
    )
    vi.stubGlobal('fetch', fetchMock)

    const { loadThinking, cachedText, clearThinkingCache } = useThinkingContent()
    const p = loadThinking('th_1', 42)
    // The clear lands while the fetch is still pending.
    clearThinkingCache()
    resolveFetch!({ ok: true, json: async () => ({ think_id: 'th_1', text: 'stale' }) })

    await expect(p).resolves.toBe('stale')
    expect(cachedText('th_1'), 'a pre-clear response must not repopulate the cache').toBeUndefined()
  })

  it('a provisional fetch is refetched by a later final request', async () => {
    // A fetch taken while the block streams returns only what chat_thinking had
    // at that instant. It must be cached (the live block needs to render) but
    // flagged, so the final request replaces it instead of returning the prefix.
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ think_id: 'th_1', text: 'PARTIAL' }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ think_id: 'th_1', text: 'PARTIAL-AND-REST' }) })
    vi.stubGlobal('fetch', fetchMock)

    const { loadThinking, cachedText, isProvisional } = useThinkingContent()
    await expect(loadThinking('th_1', 42, undefined, true)).resolves.toBe('PARTIAL')
    expect(cachedText('th_1')).toBe('PARTIAL')
    expect(isProvisional('th_1'), 'mid-stream snapshot must be flagged').toBe(true)

    await expect(loadThinking('th_1', 42, undefined, false)).resolves.toBe('PARTIAL-AND-REST')
    expect(cachedText('th_1')).toBe('PARTIAL-AND-REST')
    expect(isProvisional('th_1'), 'final text is no longer provisional').toBe(false)
  })

  it('a final request does not adopt an in-flight provisional result', async () => {
    // Otherwise the block would "refetch" and get the same partial prefix back.
    let resolveFirst: (v: unknown) => void
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve }))
      .mockResolvedValueOnce({ ok: true, json: async () => ({ think_id: 'th_1', text: 'FULL' }) })
    vi.stubGlobal('fetch', fetchMock)

    const { loadThinking, cachedText } = useThinkingContent()
    const provisionalP = loadThinking('th_1', 42, undefined, true)
    const finalP = loadThinking('th_1', 42, undefined, false)

    resolveFirst!({ ok: true, json: async () => ({ think_id: 'th_1', text: 'PARTIAL' }) })
    await expect(provisionalP).resolves.toBe('PARTIAL')
    await expect(finalP).resolves.toBe('FULL')
    expect(cachedText('th_1')).toBe('FULL')
  })

  it('a provisional request reuses an in-flight provisional request', async () => {
    // The auto-load watcher re-fires on every blocks change; without in-flight
    // dedup a streaming turn would issue a request per frame.
    let resolveFetch: (v: unknown) => void
    const fetchMock = vi.fn().mockImplementation(
      () => new Promise((resolve) => { resolveFetch = resolve }),
    )
    vi.stubGlobal('fetch', fetchMock)

    const { loadThinking } = useThinkingContent()
    const p1 = loadThinking('th_1', 42, undefined, true)
    const p2 = loadThinking('th_1', 42, undefined, true)
    resolveFetch!({ ok: true, json: async () => ({ think_id: 'th_1', text: 'x' }) })
    await Promise.all([p1, p2])
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('clearThinkingCache also drops provisional flags', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true, json: async () => ({ think_id: 'th_1', text: 'x' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { loadThinking, clearThinkingCache, isProvisional } = useThinkingContent()
    await loadThinking('th_1', 42, undefined, true)
    expect(isProvisional('th_1')).toBe(true)
    clearThinkingCache()
    expect(isProvisional('th_1')).toBe(false)
  })
})
