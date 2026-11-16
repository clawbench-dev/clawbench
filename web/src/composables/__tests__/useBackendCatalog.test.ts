import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { useBackendCatalog, type BackendInfo } from '@/composables/useBackendCatalog'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const BACKENDS: BackendInfo[] = [
  { id: 'claude', name: 'Claude', specialty: 'Code', default_cmd: 'claude', install_cmd: 'npm install -g @anthropic-ai/claude-code' },
  { id: 'builtin', name: 'ClawBench Agent', specialty: 'Built-in', default_cmd: '' },
]

function stubJson(payload: unknown) {
  const fetchMock = vi.fn(async () => ({ ok: true, json: async () => payload }))
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('useBackendCatalog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('loads the catalogue from /api/backends', async () => {
    const fetchMock = stubJson({ backends: BACKENDS })
    const catalog = useBackendCatalog()

    await catalog.load()

    expect(fetchMock).toHaveBeenCalledWith('/api/backends')
    expect(catalog.backends.value).toEqual(BACKENDS)
    expect(catalog.loading.value).toBe(false)
  })

  it('treats a missing backends array as an empty catalogue', async () => {
    stubJson({})
    const catalog = useBackendCatalog()

    await catalog.load()

    expect(catalog.backends.value).toEqual([])
  })

  it('keeps the previous list when the response is not ok', async () => {
    stubJson({ backends: BACKENDS })
    const catalog = useBackendCatalog()
    await catalog.load()

    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, json: async () => ({}) })))
    await catalog.load()

    expect(catalog.backends.value).toEqual(BACKENDS)
    expect(catalog.loading.value).toBe(false)
  })

  it('keeps the previous list and logs when the request throws', async () => {
    stubJson({ backends: BACKENDS })
    const catalog = useBackendCatalog()
    await catalog.load()

    const boom = new Error('network down')
    vi.stubGlobal('fetch', vi.fn(async () => { throw boom }))
    const { appLog } = await import('@/utils/appLog')
    await catalog.load()

    expect(catalog.backends.value).toEqual(BACKENDS)
    expect(catalog.loading.value).toBe(false)
    expect(appLog.w).toHaveBeenCalledWith('BackendCatalog', expect.any(String), boom)
  })

  it('exposes loading while the request is in flight', async () => {
    let resolveFetch!: (v: unknown) => void
    const pending = new Promise((res) => { resolveFetch = res })
    vi.stubGlobal('fetch', vi.fn(() => pending))
    const catalog = useBackendCatalog()

    const running = catalog.load()
    expect(catalog.loading.value).toBe(true)

    resolveFetch({ ok: true, json: async () => ({ backends: BACKENDS }) })
    await running

    expect(catalog.loading.value).toBe(false)
    expect(catalog.backends.value).toEqual(BACKENDS)
  })
})
