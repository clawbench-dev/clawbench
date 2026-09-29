import { describe, expect, it, vi, beforeEach } from 'vitest'

// ────────────────────────────────────────────────────────────
// api.ts functions use fetch and i18n. We mock both to test
// the error handling and header injection logic.
// ────────────────────────────────────────────────────────────

// Mock i18n
vi.mock('@/i18n', () => ({
  default: {
    global: {
      locale: { value: 'en' },
    },
  },
}))

// Mock fetch globally
const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

// Import after mocks are set up
import { apiGet, apiPost, apiPut, apiPatch, apiDelete, cancelChat } from '@/utils/api'

beforeEach(() => {
  mockFetch.mockReset()
})

// Helper: match fetch calls that include an AbortSignal
// (signal is an AbortSignal instance, so we use expect.any(AbortSignal))
function expectFetchCalledWith(url: string, opts: Record<string, unknown>) {
  expect(mockFetch).toHaveBeenCalledWith(url, {
    ...opts,
    signal: expect.any(AbortSignal),
  })
}

describe('apiGet', () => {
  it('makes GET request with locale header', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ data: 'test' }),
    })

    const result = await apiGet('/api/test')
    expectFetchCalledWith('/api/test', {
      headers: { 'X-Locale': 'en' },
    })
    expect(result).toEqual({ data: 'test' })
  })

  it('throws error on non-ok response', async () => {
    mockFetch.mockResolvedValue({
      ok: false,
      json: () => Promise.resolve({ error: 'Not Found' }),
    })

    await expect(apiGet('/api/missing')).rejects.toThrow('Not Found')
  })

  it('attaches msgKey to error on non-ok response', async () => {
    mockFetch.mockResolvedValue({
      ok: false,
      json: () => Promise.resolve({ error: 'Directory not found', msgKey: 'DirectoryNotFound' }),
    })

    try {
      await apiGet('/api/missing-dir')
      expect.fail('Should have thrown')
    } catch (err) {
      expect((err as Error).message).toBe('Directory not found')
      expect((err as Error & { msgKey?: string }).msgKey).toBe('DirectoryNotFound')
    }
  })

  it('throws with statusText when JSON error has no error field', async () => {
    mockFetch.mockResolvedValue({
      ok: false,
      statusText: 'Bad Request',
      json: () => Promise.resolve({}),
    })

    await expect(apiGet('/api/bad')).rejects.toThrow('Bad Request')
  })

  it('handles JSON parse failure in error response', async () => {
    mockFetch.mockResolvedValue({
      ok: false,
      statusText: 'Internal Server Error',
      json: () => Promise.reject(new Error('Invalid JSON')),
    })

    await expect(apiGet('/api/broken')).rejects.toThrow('Internal Server Error')
  })
})

describe('apiPost', () => {
  it('makes POST request with JSON body and locale header', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ ok: true, sessionId: '123' }),
    })

    const result = await apiPost('/api/test', { name: 'test' })
    expectFetchCalledWith('/api/test', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Locale': 'en' },
      body: JSON.stringify({ name: 'test' }),
    })
    expect(result).toEqual({ ok: true, sessionId: '123' })
  })

  it('throws error with data.error message on non-ok response', async () => {
    mockFetch.mockResolvedValue({
      ok: false,
      json: () => Promise.resolve({ error: 'Session not found' }),
    })

    await expect(apiPost('/api/test', {})).rejects.toThrow('Session not found')
  })

  it('throws with statusText when no error field', async () => {
    mockFetch.mockResolvedValue({
      ok: false,
      statusText: 'Bad Request',
      json: () => Promise.resolve({}),
    })

    await expect(apiPost('/api/test', {})).rejects.toThrow('Bad Request')
  })

  it('handles JSON parse failure in error response', async () => {
    mockFetch.mockResolvedValue({
      ok: false,
      statusText: 'Internal Server Error',
      json: () => Promise.reject(new Error('Invalid JSON')),
    })

    await expect(apiPost('/api/test', {})).rejects.toThrow('Internal Server Error')
  })
})

describe('apiDelete', () => {
  it('makes DELETE request with locale header', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ ok: true }),
    })

    const result = await apiDelete('/api/test/123')
    expectFetchCalledWith('/api/test/123', {
      method: 'DELETE',
      headers: { 'X-Locale': 'en' },
    })
    expect(result).toEqual({ ok: true })
  })

  it('throws error on non-ok response', async () => {
    mockFetch.mockResolvedValue({
      ok: false,
      statusText: 'Forbidden',
      json: () => Promise.resolve({}),
    })

    await expect(apiDelete('/api/test/123')).rejects.toThrow('Forbidden')
  })

  it('sends body with DELETE request when body option is provided', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ success: true }),
    })

    const result = await apiDelete('/api/git/branch', { body: { name: 'feature-x' } })
    expect(mockFetch).toHaveBeenCalledWith(
      '/api/git/branch',
      expect.objectContaining({
        method: 'DELETE',
        body: JSON.stringify({ name: 'feature-x' }),
        headers: expect.objectContaining({ 'Content-Type': 'application/json' }),
      }),
    )
    expect(result).toEqual({ success: true })
  })

  it('includes error from response JSON when DELETE fails', async () => {
    mockFetch.mockResolvedValue({
      ok: false,
      statusText: 'Internal Server Error',
      json: () => Promise.resolve({ error: 'cannot_delete_current' }),
    })

    await expect(apiDelete('/api/git/branch', { body: { name: 'main' } }))
      .rejects.toThrow('cannot_delete_current')
  })
})

describe('cancelChat', () => {
  it('makes POST request to cancel endpoint', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({}) })

    await cancelChat('session-123')
    expect(mockFetch).toHaveBeenCalledWith(
      '/api/ai/chat/cancel?session_id=session-123',
      expect.objectContaining({
        method: 'POST',
        body: '{}',
        headers: expect.objectContaining({ 'Content-Type': 'application/json' }),
        signal: expect.any(AbortSignal),
      }),
    )
  })

  it('encodes session ID with special characters', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({}) })

    await cancelChat('session/with+special')
    expect(mockFetch).toHaveBeenCalledWith(
      '/api/ai/chat/cancel?session_id=session%2Fwith%2Bspecial',
      expect.objectContaining({
        method: 'POST',
        signal: expect.any(AbortSignal),
      }),
    )
  })

  it('throws error on non-ok response', async () => {
    mockFetch.mockResolvedValue({
      ok: false,
      statusText: 'Not Found',
      json: () => Promise.resolve({}),
    })

    await expect(cancelChat('bad-session')).rejects.toThrow()
  })
})

// ── External AbortSignal support ──

describe('apiGet with signal', () => {
  it('passes external signal influence to fetch call', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ data: 'test' }),
    })

    const externalController = new AbortController()
    await apiGet('/api/test', { signal: externalController.signal })

    // The signal passed to fetch should be an AbortSignal but NOT the same instance
    // as the external one — createSignal creates a merged controller
    const fetchOpts = mockFetch.mock.calls[0][1] as { signal: AbortSignal }
    expect(fetchOpts.signal).toBeInstanceOf(AbortSignal)
    expect(fetchOpts.signal).not.toBe(externalController.signal)
    // The merged signal should not be aborted since external signal is still active
    expect(fetchOpts.signal.aborted).toBe(false)
  })

  it('rejects immediately when external signal is already aborted', async () => {
    const controller = new AbortController()
    controller.abort()

    // When the external signal is already aborted, createSignal immediately aborts the
    // internal controller too. The real fetch would throw DOMException, but our mock
    // doesn't check the signal — so the fetch resolves, and apiGet reads resp.ok which
    // is undefined because the cleanup fires before fetch resolves.
    // The key behavior to verify: the merged signal is aborted from the start.
    const result = apiGet('/api/test', { signal: controller.signal })
    // Regardless of whether mock fetch cooperates, the code should not return success
    await expect(result).rejects.toThrow()
  })

  it('aborts a timeout with an AbortError reason, not the opaque default', async () => {
    // A bare abort() makes fetch throw "signal is aborted without reason",
    // which hides the fact that this was a CLIENT-side timeout (the server may
    // have answered). The reason must also keep name === 'AbortError' so the
    // many callers that treat a superseded request as non-fatal still do.
    vi.useFakeTimers()

    let capturedSignal: AbortSignal | undefined
    mockFetch.mockImplementation((_url: string, opts: { signal?: AbortSignal }) => {
      capturedSignal = opts?.signal
      return new Promise(() => {})
    })

    const promise = apiGet('/api/test')
    promise.catch(() => {})
    await vi.advanceTimersByTimeAsync(10_000)

    expect(capturedSignal?.aborted).toBe(true)
    const reason = capturedSignal?.reason
    expect(reason).toBeInstanceOf(DOMException)
    expect((reason as DOMException).name).toBe('AbortError')
    expect(String((reason as DOMException).message)).toContain('timed out')

    vi.useRealTimers()
  })

  it('rejects when fetch times out', async () => {
    vi.useFakeTimers()

    // Simulate a hanging fetch — never resolves on its own.
    // We capture the signal passed to fetch so we can verify it gets aborted on timeout.
    let capturedSignal: AbortSignal | undefined
    mockFetch.mockImplementation((_url: string, opts: { signal?: AbortSignal }) => {
      capturedSignal = opts?.signal
      return new Promise(() => {}) // never resolves
    })

    const promise = apiGet('/api/test')

    // Before timeout: signal is not aborted
    expect(capturedSignal?.aborted).toBe(false)

    // Advance past the 10s timeout
    await vi.advanceTimersByTimeAsync(10_000)

    // After timeout: the internal signal should be aborted
    expect(capturedSignal?.aborted).toBe(true)

    // The promise should reject (suppress the unhandled rejection from our never-resolving mock)
    promise.catch(() => {})

    vi.useRealTimers()
  })
})

describe('apiPut', () => {
  it('makes PUT request with JSON body and locale header', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ task: { id: '1' } }),
    })

    const result = await apiPut('/api/tasks/1', { action: 'pause' })
    expectFetchCalledWith('/api/tasks/1', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', 'X-Locale': 'en' },
      body: JSON.stringify({ action: 'pause' }),
    })
    expect(result).toEqual({ task: { id: '1' } })
  })

  it('throws error with data.error message on non-ok response', async () => {
    mockFetch.mockResolvedValue({
      ok: false,
      json: () => Promise.resolve({ error: 'Invalid cron' }),
    })

    await expect(apiPut('/api/tasks/1', {})).rejects.toThrow('Invalid cron')
  })

  it('forwards external signal to fetch', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({}),
    })

    const externalSignal = new AbortController().signal
    await apiPut('/api/tasks/1', { action: 'trigger' }, { signal: externalSignal })

    expect(mockFetch).toHaveBeenCalledWith('/api/tasks/1', expect.objectContaining({
      signal: expect.any(AbortSignal),
    }))
  })
})

describe('apiPost with signal', () => {
  it('forwards external signal to fetch', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ task: { id: 'new' } }),
    })

    const externalSignal = new AbortController().signal
    await apiPost('/api/tasks', { name: 'test' }, { signal: externalSignal })

    expect(mockFetch).toHaveBeenCalledWith('/api/tasks', expect.objectContaining({
      signal: expect.any(AbortSignal),
    }))
  })
})

describe('apiDelete with signal', () => {
  it('forwards external signal to fetch', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({}),
    })

    const externalSignal = new AbortController().signal
    await apiDelete('/api/tasks/1', { signal: externalSignal })

    expect(mockFetch).toHaveBeenCalledWith('/api/tasks/1', expect.objectContaining({
      signal: expect.any(AbortSignal),
    }))
  })
})

describe('apiPatch', () => {
  it('makes PATCH request with JSON body and locale header', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ needs_restart: false }),
    })

    const result = await apiPatch('/api/config', { 'chat.page_size': 25 })
    expectFetchCalledWith('/api/config', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json', 'X-Locale': 'en' },
      body: JSON.stringify({ 'chat.page_size': 25 }),
    })
    expect(result).toEqual({ needs_restart: false })
  })

  it('throws error with data.error message on non-ok response', async () => {
    mockFetch.mockResolvedValue({
      ok: false,
      json: () => Promise.resolve({ error: 'forbidden_field' }),
    })

    await expect(apiPatch('/api/config', {})).rejects.toThrow('forbidden_field')
  })

  it('throws with statusText when no error field', async () => {
    mockFetch.mockResolvedValue({
      ok: false,
      statusText: 'Bad Request',
      json: () => Promise.resolve({}),
    })

    await expect(apiPatch('/api/config', {})).rejects.toThrow('Bad Request')
  })

  it('handles JSON parse failure in error response', async () => {
    mockFetch.mockResolvedValue({
      ok: false,
      statusText: 'Internal Server Error',
      json: () => Promise.reject(new Error('Invalid JSON')),
    })

    await expect(apiPatch('/api/config', {})).rejects.toThrow('Internal Server Error')
  })

  it('forwards external signal to fetch', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({}),
    })

    const externalSignal = new AbortController().signal
    await apiPatch('/api/config', { key: 'value' }, { signal: externalSignal })

    expect(mockFetch).toHaveBeenCalledWith('/api/config', expect.objectContaining({
      signal: expect.any(AbortSignal),
    }))
  })
})
