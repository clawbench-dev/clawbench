import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { useFileSearch } from '@/composables/useFileSearch'

// Mock appLog
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// Mock EventSource
class MockEventSource {
  static instances: MockEventSource[] = []
  url: string
  onerror: (() => void) | null = null
  private listeners: Map<string, Array<(e: { data: string }) => void>> = new Map()
  closed = false

  constructor(url: string) {
    this.url = url
    MockEventSource.instances.push(this)
  }

  addEventListener(type: string, handler: (e: { data: string }) => void) {
    if (!this.listeners.has(type)) this.listeners.set(type, [])
    this.listeners.get(type)!.push(handler)
  }

  emit(type: string, data: unknown) {
    const handlers = this.listeners.get(type) || []
    handlers.forEach(h => h({ data: JSON.stringify(data) }))
  }

  close() {
    this.closed = true
  }
}

// Provide global EventSource mock
const OriginalEventSource = globalThis.EventSource

beforeEach(() => {
  MockEventSource.instances = []
  globalThis.EventSource = MockEventSource as unknown as typeof EventSource
  vi.useFakeTimers()
})

afterEach(() => {
  globalThis.EventSource = OriginalEventSource
  vi.useRealTimers()
})

describe('useFileSearch', () => {
  it('initializes with default state', () => {
    const { state } = useFileSearch()
    expect(state.query).toBe('')
    expect(state.recursive).toBe(false)
    expect(state.scope).toBe('current')
    expect(state.results).toEqual([])
    expect(state.searching).toBe(false)
    expect(state.total).toBe(0)
    expect(state.truncated).toBe(false)
    expect(state.searchBasePath).toBe('')
  })

  it('startSearch with empty query clears results', () => {
    const { state, startSearch } = useFileSearch()
    state.results = [{ name: 'test.go', path: 'test.go', type: 'file', matchedIndices: [0] }]
    state.query = ''
    startSearch('')
    expect(state.results).toEqual([])
    expect(state.searching).toBe(false)
  })

  it('startSearch sets searching and searchBasePath, then opens SSE after debounce', () => {
    const { state, startSearch } = useFileSearch()
    state.query = 'main'
    startSearch('src')
    expect(state.searchBasePath).toBe('src')
    expect(state.searching).toBe(true)
    expect(state.results).toEqual([])

    // Before debounce, no SSE yet
    expect(MockEventSource.instances.length).toBe(0)

    // After debounce
    vi.advanceTimersByTime(300)
    expect(MockEventSource.instances.length).toBe(1)
    expect(MockEventSource.instances[0].url).toContain('/api/dir/search')
    expect(MockEventSource.instances[0].url).toContain('q=main')
    expect(MockEventSource.instances[0].url).toContain('path=src')
    expect(MockEventSource.instances[0].url).toContain('recursive=false')
  })

  it('startSearch cancels previous search before starting new one', () => {
    const { state, startSearch } = useFileSearch()
    state.query = 'first'
    startSearch('')
    vi.advanceTimersByTime(300)
    expect(MockEventSource.instances.length).toBe(1)

    state.query = 'second'
    startSearch('')
    // Previous SSE should be closed
    expect(MockEventSource.instances[0].closed).toBe(true)
    expect(MockEventSource.instances.length).toBe(1)

    vi.advanceTimersByTime(300)
    expect(MockEventSource.instances.length).toBe(2)
  })

  it('cancelSearch closes EventSource and clears debounce', () => {
    const { state, startSearch, cancelSearch } = useFileSearch()
    state.query = 'test'
    startSearch('')
    vi.advanceTimersByTime(300)

    cancelSearch()
    expect(MockEventSource.instances[0].closed).toBe(true)
    expect(state.searching).toBe(false)
  })

  it('result events accumulate to state.results', () => {
    const { state, startSearch } = useFileSearch()
    state.query = 'test'
    startSearch('')
    vi.advanceTimersByTime(300)

    const es = MockEventSource.instances[0]
    es.emit('result', { name: 'test.go', path: 'pkg/test.go', type: 'file', matchedIndices: [0, 1, 2, 3] })
    es.emit('result', { name: 'test_util.go', path: 'internal/test_util.go', type: 'file', matchedIndices: [0, 1, 2, 3] })

    expect(state.results.length).toBe(2)
    expect(state.results[0].name).toBe('test.go')
    expect(state.results[1].path).toBe('internal/test_util.go')
  })

  it('done event sets total, truncated, clears searching', () => {
    const { state, startSearch } = useFileSearch()
    state.query = 'test'
    startSearch('')
    vi.advanceTimersByTime(300)

    const es = MockEventSource.instances[0]
    es.emit('done', { total: 5, truncated: true })

    expect(state.total).toBe(5)
    expect(state.truncated).toBe(true)
    expect(state.searching).toBe(false)
  })

  it('reset clears all state', () => {
    const { state, startSearch, reset } = useFileSearch()
    state.query = 'test'
    startSearch('')
    vi.advanceTimersByTime(300)

    const es = MockEventSource.instances[0]
    es.emit('result', { name: 'test.go', path: 'test.go', type: 'file', matchedIndices: [0] })
    es.emit('done', { total: 1, truncated: false })

    reset()
    expect(state.query).toBe('')
    expect(state.results).toEqual([])
    expect(state.total).toBe(0)
    expect(state.truncated).toBe(false)
    expect(state.searching).toBe(false)
    expect(state.searchBasePath).toBe('')
  })

  it('uses recursive=false in URL when state.recursive is false', () => {
    const { state, startSearch } = useFileSearch()
    state.query = 'test'
    state.recursive = false
    startSearch('')
    vi.advanceTimersByTime(300)

    expect(MockEventSource.instances[0].url).toContain('recursive=false')
  })

  it('sends exact=false in URL by default and exact=true when enabled', () => {
    const { state, startSearch } = useFileSearch()
    state.query = 'test'
    startSearch('')
    vi.advanceTimersByTime(300)
    expect(MockEventSource.instances[0].url).toContain('exact=false')

    state.exact = true
    startSearch('')
    vi.advanceTimersByTime(300)
    expect(MockEventSource.instances[1].url).toContain('exact=true')
  })

  it('SSE error event clears searching state', () => {
    const { state, startSearch } = useFileSearch()
    state.query = 'test'
    startSearch('')
    vi.advanceTimersByTime(300)

    const es = MockEventSource.instances[0]
    es.emit('error', { message: 'I/O error' })

    expect(state.searching).toBe(false)
  })

  it('EventSource onerror clears searching state', () => {
    const { state, startSearch } = useFileSearch()
    state.query = 'test'
    startSearch('')
    vi.advanceTimersByTime(300)

    const es = MockEventSource.instances[0]
    es.onerror?.()

    expect(state.searching).toBe(false)
  })

  it('scope=current uses currentDir as search path', () => {
    const { state, startSearch } = useFileSearch()
    state.query = 'test'
    state.scope = 'current'
    startSearch('internal/handler')
    vi.advanceTimersByTime(300)

    expect(state.searchBasePath).toBe('internal/handler')
    expect(MockEventSource.instances[0].url).toContain('path=internal%2Fhandler')
  })

  it('scope=global uses empty string as search path', () => {
    const { state, startSearch } = useFileSearch()
    state.query = 'test'
    state.scope = 'global'
    startSearch('internal/handler')
    vi.advanceTimersByTime(300)

    expect(state.searchBasePath).toBe('internal/handler')
    // When scope is global, search from project root (empty path)
    expect(MockEventSource.instances[0].url).toContain('path=')
    expect(MockEventSource.instances[0].url).not.toContain('path=internal')
  })

  it('effectiveDir returns empty string when scope is global', () => {
    const { state, effectiveDir, startSearch } = useFileSearch()
    state.query = 'test'
    state.scope = 'global'
    startSearch('internal/handler')
    expect(effectiveDir.value).toBe('')
  })

  it('effectiveDir returns searchBasePath when scope is current', () => {
    const { state, effectiveDir, startSearch } = useFileSearch()
    state.query = 'test'
    state.scope = 'current'
    startSearch('internal/handler')
    expect(effectiveDir.value).toBe('internal/handler')
  })
})
