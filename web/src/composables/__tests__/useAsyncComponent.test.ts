import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { buildAsyncComponentOptions } from '../useAsyncComponent.ts'
import AsyncComponentError from '@/components/common/AsyncComponentError.vue'

describe('buildAsyncComponentOptions', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('sets loading and error components', () => {
    const opts = buildAsyncComponentOptions({ loader: () => Promise.resolve({}) })
    expect(opts.loadingComponent).toBeTruthy()
    expect(opts.errorComponent).toBe(AsyncComponentError)
  })

  it('retries the loader up to maxRetries times, then fails', async () => {
    const loader = vi.fn().mockRejectedValue(new Error('Failed to fetch dynamically imported module'))
    const opts = buildAsyncComponentOptions({ loader, maxRetries: 3, retryDelay: 100 })

    const onError = opts.onError!
    let retries = 0
    let failed = false

    const fail = () => { failed = true }

    // Each attempt returns the next `retry` callback. Simulate the chain:
    // attempts 1,2,3 auto-retry; attempt 4 (attempts > maxRetries) fails.
    for (let i = 1; i <= 4; i++) {
      const error = new Error('Failed to fetch dynamically imported module')
      const retry = vi.fn()
      onError(error, retry, fail, i)
      // In-flight retry fires the next attempt after the delay.
      await vi.advanceTimersByTimeAsync(100)
      retries += retry.mock.calls.length
    }

    expect(retries).toBe(3)   // maxRetries auto-retries
    expect(failed).toBe(true) // 4th attempt surfaced the error
  })
})
