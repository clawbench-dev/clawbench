import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { buildAsyncComponentOptions } from '../useAsyncComponent.ts'
import AsyncComponentError from '@/components/common/AsyncComponentError.vue'

const CHUNK_ERROR = new Error('Failed to fetch dynamically imported module: http://127.0.0.1:20000/CodeMirrorViewer-DxHc752e.js')
const NETWORK_ERROR = new Error('NetworkError when fetching resource')

describe('buildAsyncComponentOptions', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.useRealTimers()
    sessionStorage.clear()
  })

  it('sets loading and error components', () => {
    const opts = buildAsyncComponentOptions({ loader: () => Promise.resolve({}) })
    expect(opts.loadingComponent).toBeTruthy()
    expect(opts.errorComponent).toBe(AsyncComponentError)
  })

  it('retries the loader up to maxRetries times, then fails', async () => {
    const loader = vi.fn().mockRejectedValue(NETWORK_ERROR)
    const opts = buildAsyncComponentOptions({ loader, maxRetries: 3, retryDelay: 100 })

    const onError = opts.onError!
    let retries = 0
    let failed = false

    const fail = () => { failed = true }

    // Each attempt returns the next `retry` callback. Simulate the chain:
    // attempts 1,2,3 auto-retry; attempt 4 (attempts > maxRetries) fails.
    for (let i = 1; i <= 4; i++) {
      const retry = vi.fn()
      onError(NETWORK_ERROR, retry, fail, i)
      // In-flight retry fires the next attempt after the delay.
      await vi.advanceTimersByTimeAsync(100)
      retries += retry.mock.calls.length
    }

    expect(retries).toBe(3)   // maxRetries auto-retries
    expect(failed).toBe(true) // 4th attempt surfaced the error
  })

  it('retries normally on a first chunk-fetch failure (could be transient)', async () => {
    // Tunnel/network jitter can produce the same message as a stale chunk —
    // the first failure must still go through the normal retry path.
    const onStaleChunk = vi.fn()
    const opts = buildAsyncComponentOptions({ loader: () => Promise.reject(CHUNK_ERROR), maxRetries: 3, retryDelay: 100, onStaleChunk })

    const retry = vi.fn()
    const fail = vi.fn()
    opts.onError!(CHUNK_ERROR, retry, fail, 1)
    await vi.advanceTimersByTimeAsync(100)

    expect(retry).toHaveBeenCalledTimes(1)
    expect(onStaleChunk).not.toHaveBeenCalled()
    expect(fail).not.toHaveBeenCalled()
  })

  it('reloads the page when a chunk-fetch failure persists (stale build)', async () => {
    const onStaleChunk = vi.fn()
    const opts = buildAsyncComponentOptions({ loader: () => Promise.reject(CHUNK_ERROR), maxRetries: 3, retryDelay: 100, onStaleChunk })

    const fail = vi.fn()
    // First failure: normal retry. Second failure: the chunk is gone for
    // good (old build), so the page reloads instead of retrying forever.
    opts.onError!(CHUNK_ERROR, vi.fn(), fail, 1)
    await vi.advanceTimersByTimeAsync(100)
    opts.onError!(CHUNK_ERROR, vi.fn(), fail, 2)

    expect(onStaleChunk).toHaveBeenCalledTimes(1)
    expect(fail).not.toHaveBeenCalled()
  })

  it('does not reload more than once within the guard window', async () => {
    const onStaleChunk = vi.fn()
    const opts = buildAsyncComponentOptions({ loader: () => Promise.reject(CHUNK_ERROR), maxRetries: 3, retryDelay: 100, onStaleChunk })

    const fail = vi.fn()
    // First stale reload happens on the second consecutive failure.
    opts.onError!(CHUNK_ERROR, vi.fn(), fail, 2)
    expect(onStaleChunk).toHaveBeenCalledTimes(1)

    // A later component hitting the same stale chunk must NOT reload again
    // within the guard window — it falls back to retry/fail so a broken
    // reload cannot loop.
    opts.onError!(CHUNK_ERROR, vi.fn(), fail, 2)
    expect(onStaleChunk).toHaveBeenCalledTimes(1)
    expect(fail).not.toHaveBeenCalled()
  })
})
