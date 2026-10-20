import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { buildAsyncComponentOptions } from '../useAsyncComponent.ts'
import AsyncComponentError from '@/components/common/AsyncComponentError.vue'
import {
  _resetChunkLoadRecovery,
  claimChunkReload,
  installChunkLoadRecovery,
  isStaleChunkError,
} from '@/utils/chunkLoadRecovery'

const CHUNK_ERROR = new Error('Failed to fetch dynamically imported module: http://127.0.0.1:20000/CodeMirrorViewer-DxHc752e.js')
const CSS_PRELOAD_ERROR = new Error('Unable to preload CSS for /UsageStatsPanel-DO6TAU0Y.css')
const NETWORK_ERROR = new Error('NetworkError when fetching resource')

function makeI18n() {
  return createI18n({
    legacy: false,
    locale: 'zh',
    messages: { zh: { common: { retry: '重试', componentLoadFailed: '加载失败' } } },
  })
}

describe('buildAsyncComponentOptions', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.useRealTimers()
    sessionStorage.clear()
  })

  it('sets a loading component and an error component that renders the retry UI', () => {
    const opts = buildAsyncComponentOptions({ loader: () => Promise.resolve({}) })
    expect(opts.loadingComponent).toBeTruthy()
    expect(opts.errorComponent).toBeTruthy()

    // Vue renders errorComponent with only an `error` prop. The component must
    // still show the shared error surface (message + retry button) — a bare
    // AsyncComponentError would be mounted without its `retry` handler.
    const wrapper = mount(opts.errorComponent as never, {
      props: { error: CHUNK_ERROR },
      global: { plugins: [makeI18n()] },
    })
    expect(wrapper.findComponent(AsyncComponentError).exists()).toBe(true)
    expect(wrapper.text()).toContain('重试')
  })

  it('reloads from the error component retry button (Vue gives it no retry handler)', async () => {
    const onRetry = vi.fn()
    const opts = buildAsyncComponentOptions({ loader: () => Promise.resolve({}), onRetry })
    const wrapper = mount(opts.errorComponent as never, {
      props: { error: CHUNK_ERROR },
      global: { plugins: [makeI18n()] },
    })
    await wrapper.get('button').trigger('click')
    expect(onRetry).toHaveBeenCalledTimes(1)
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

  it('recovers from a persistent chunk-fetch failure (stale build)', async () => {
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

  it('treats a failed CSS preload as a stale chunk (that is how the stats pane died)', async () => {
    // The reported failure was `Unable to preload CSS for /UsageStatsPanel-*.css`
    // (the CSS dep 404s first, so the JS import is never attempted). Missing
    // that message from the pattern leaves the pane spinning forever.
    const onStaleChunk = vi.fn()
    const opts = buildAsyncComponentOptions({ loader: () => Promise.reject(CSS_PRELOAD_ERROR), retryDelay: 100, onStaleChunk })

    opts.onError!(CSS_PRELOAD_ERROR, vi.fn(), vi.fn(), 1)
    await vi.advanceTimersByTimeAsync(100)
    opts.onError!(CSS_PRELOAD_ERROR, vi.fn(), vi.fn(), 2)

    expect(onStaleChunk).toHaveBeenCalledTimes(1)
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

describe('isStaleChunkError', () => {
  it('recognizes the fetch, module-script and CSS-preload variants', () => {
    expect(isStaleChunkError(CHUNK_ERROR)).toBe(true)
    expect(isStaleChunkError(CSS_PRELOAD_ERROR)).toBe(true)
    expect(isStaleChunkError(new Error('Importing a module script failed.'))).toBe(true)
  })

  it('does not classify a plain network error as stale (that must retry)', () => {
    expect(isStaleChunkError(NETWORK_ERROR)).toBe(false)
  })
})

describe('claimChunkReload', () => {
  beforeEach(() => sessionStorage.clear())
  afterEach(() => sessionStorage.clear())

  it('allows one reload per guard window', () => {
    expect(claimChunkReload()).toBe(true)
    expect(claimChunkReload()).toBe(false)
  })
})

describe('installChunkLoadRecovery', () => {
  const listeners: EventListener[] = []
  let reload: ReturnType<typeof vi.fn>

  beforeEach(() => {
    sessionStorage.clear()
    // The burst detector is module-level state: without a reset, the first
    // test's failure arms it and a later "single failure" case would reload.
    _resetChunkLoadRecovery()
    reload = vi.fn()
    // jsdom cannot navigate; the reload must never be attempted for real.
    vi.stubGlobal('location', { ...window.location, reload })
    // Capture the listener so each test dispatches against a clean slate
    // instead of the accumulated listeners of every previous install.
    const add = window.addEventListener.bind(window)
    vi.spyOn(window, 'addEventListener').mockImplementation((type, fn, opts) => {
      if (type === 'vite:preloadError') listeners.push(fn as EventListener)
      return add(type, fn, opts)
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    listeners.length = 0
  })

  function dispatchPreloadError(payload: unknown): Event {
    const event = new Event('vite:preloadError', { cancelable: true })
    ;(event as Event & { payload?: unknown }).payload = payload
    for (const fn of listeners) fn(event)
    return event
  }

  it('does not reload on a single preload failure (could be transient)', () => {
    installChunkLoadRecovery()
    dispatchPreloadError(CSS_PRELOAD_ERROR)
    expect(reload).not.toHaveBeenCalled()
  })

  it('reloads once a burst of preload failures proves the build is stale', () => {
    // A stale build fails every chunk the pane references within a few ms
    // (measured: nine 404s in ~20 ms). The first failure only arms the burst.
    installChunkLoadRecovery()
    dispatchPreloadError(CSS_PRELOAD_ERROR)
    dispatchPreloadError(CHUNK_ERROR)
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('does not preventDefault — Vite would resolve the import to undefined', () => {
    // `preventDefault` means "handled", and Vite then resolves the import with
    // no module. Vue warns "loader resolved to undefined" and renders the
    // loading component forever, which is the exact stuck-spinner symptom.
    installChunkLoadRecovery()
    dispatchPreloadError(CHUNK_ERROR)
    const event = dispatchPreloadError(CHUNK_ERROR)
    expect(event.defaultPrevented).toBe(false)
  })

  it('does not reload twice in a row (loop guard)', () => {
    installChunkLoadRecovery()
    dispatchPreloadError(CHUNK_ERROR)
    dispatchPreloadError(CHUNK_ERROR)
    dispatchPreloadError(CHUNK_ERROR)
    dispatchPreloadError(CHUNK_ERROR)
    expect(reload).toHaveBeenCalledTimes(1)
  })
})
