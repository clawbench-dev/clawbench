import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { useFileContentSearch } from '@/composables/useFileContentSearch'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// Mock EventSource. The composable only uses addEventListener / close / onerror,
// so the fake mirrors that surface and records every opened URL.
class MockEventSource {
  static instances: MockEventSource[] = []
  url: string
  onerror: (() => void) | null = null
  closed = false
  private listeners: Map<string, Array<(e: { data?: string }) => void>> = new Map()

  constructor(url: string) {
    this.url = url
    MockEventSource.instances.push(this)
  }

  addEventListener(type: string, handler: (e: { data?: string }) => void) {
    if (!this.listeners.has(type)) this.listeners.set(type, [])
    this.listeners.get(type)!.push(handler)
  }

  emit(type: string, data?: unknown) {
    const handlers = this.listeners.get(type) || []
    handlers.forEach(h => h(data === undefined ? {} : { data: JSON.stringify(data) }))
  }

  /** Emit a raw (unstringified) payload, for malformed-data cases. */
  emitRaw(type: string, data: string) {
    const handlers = this.listeners.get(type) || []
    handlers.forEach(h => h({ data }))
  }

  close() {
    this.closed = true
  }
}

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

/** Open the SSE for `query` and return the (single) connection. */
function startAndConnect(query: string, dir = '') {
  const api = useFileContentSearch()
  api.state.query = query
  api.startSearch(dir)
  vi.advanceTimersByTime(300)
  return api
}

describe('useFileContentSearch', () => {
  it('initializes with VSCode-like defaults', () => {
    const { state } = useFileContentSearch()
    expect(state.query).toBe('')
    expect(state.recursive).toBe(false)
    expect(state.regex).toBe(false)
    expect(state.wholeWord).toBe(false)
    expect(state.caseSensitive).toBe(false)
    expect(state.include).toBe('')
    expect(state.exclude).toBe('')
    expect(state.scope).toBe('current')
    expect(state.results).toEqual([])
    expect(state.searching).toBe(false)
    expect(state.error).toBe('')
  })

  it('debounces before opening the stream', () => {
    const { state, startSearch } = useFileContentSearch()
    state.query = 'needle'
    startSearch('src')
    expect(state.searching).toBe(true)
    expect(MockEventSource.instances.length).toBe(0)

    vi.advanceTimersByTime(300)
    expect(MockEventSource.instances.length).toBe(1)
    expect(MockEventSource.instances[0].url).toContain('/api/file/content-search')
  })

  it('sends every search option as a query param', () => {
    const { state, startSearch } = useFileContentSearch()
    state.query = 'foo'
    state.regex = true
    state.wholeWord = true
    state.caseSensitive = true
    state.recursive = true
    state.include = '*.ts'
    state.exclude = 'dist/**'
    startSearch('src')
    vi.advanceTimersByTime(300)

    const url = MockEventSource.instances[0].url
    expect(url).toContain('q=foo')
    expect(url).toContain('path=src')
    expect(url).toContain('recursive=true')
    expect(url).toContain('regex=true')
    expect(url).toContain('wholeWord=true')
    expect(url).toContain('caseSensitive=true')
    expect(url).toContain(`include=${encodeURIComponent('*.ts')}`)
    expect(url).toContain(`exclude=${encodeURIComponent('dist/**')}`)
  })

  it('omits empty include/exclude params', () => {
    const api = startAndConnect('foo')
    expect(api.state.searching).toBe(true)
    const url = MockEventSource.instances[0].url
    expect(url).not.toContain('include=')
    expect(url).not.toContain('exclude=')
  })

  it('global scope forces an empty path and recursion', () => {
    const { state, startSearch, effectiveDir, effectiveRecursive } = useFileContentSearch()
    state.query = 'foo'
    state.scope = 'global'
    state.recursive = false

    expect(effectiveDir.value).toBe('')
    expect(effectiveRecursive.value).toBe(true)

    startSearch('src')
    vi.advanceTimersByTime(300)
    const url = MockEventSource.instances[0].url
    expect(url).toContain('path=&')
    expect(url).toContain('recursive=true')
  })

  it('global scope keeps the user recursive preference for when it is turned off', () => {
    const { state, effectiveRecursive } = useFileContentSearch()
    state.recursive = false
    state.scope = 'global'
    expect(effectiveRecursive.value).toBe(true)

    state.scope = 'current'
    expect(effectiveRecursive.value).toBe(false)
  })

  it('groups one result event into one file entry', () => {
    const api = startAndConnect('needle')
    MockEventSource.instances[0].emit('result', {
      name: 'a.go',
      path: 'src/a.go',
      matches: [
        { line: 3, text: 'needle', ranges: [{ start: 0, end: 6 }] },
        { line: 9, text: 'needle again', ranges: [{ start: 0, end: 6 }] },
      ],
      total: 2,
    })

    expect(api.state.results).toHaveLength(1)
    expect(api.state.results[0].path).toBe('src/a.go')
    expect(api.state.results[0].matches).toHaveLength(2)
    expect(api.state.files).toBe(1)
    expect(api.loadedMatches.value).toBe(2)
  })

  it('accumulates multiple files and tracks the loaded match count', () => {
    const api = startAndConnect('needle')
    const es = MockEventSource.instances[0]
    es.emit('result', { name: 'a.go', path: 'a.go', matches: [{ line: 1, text: 'needle', ranges: [] }], total: 1 })
    es.emit('result', {
      name: 'b.go',
      path: 'b.go',
      matches: [
        { line: 1, text: 'needle', ranges: [] },
        { line: 2, text: 'needle', ranges: [] },
      ],
      total: 2,
    })

    expect(api.state.results).toHaveLength(2)
    expect(api.loadedMatches.value).toBe(3)
  })

  it('done event applies totals and stops the spinner', () => {
    const api = startAndConnect('needle')
    const es = MockEventSource.instances[0]
    es.emit('result', { name: 'a.go', path: 'a.go', matches: [{ line: 1, text: 'needle', ranges: [] }], total: 1 })
    es.emit('done', { files: 1, matches: 7, searched: 42, truncated: true })

    expect(api.state.files).toBe(1)
    expect(api.state.matches).toBe(7)
    expect(api.state.searched).toBe(42)
    expect(api.state.truncated).toBe(true)
    expect(api.state.searching).toBe(false)
  })

  it('business error event surfaces the message and stops searching', () => {
    const api = startAndConnect('([bad')
    MockEventSource.instances[0].emit('error', { message: '正则表达式无效：missing closing )' })

    expect(api.state.error).toBe('正则表达式无效：missing closing )')
    expect(api.state.searching).toBe(false)
  })

  it('connection error stops searching without setting a business error', () => {
    const api = startAndConnect('needle')
    MockEventSource.instances[0].onerror?.()

    expect(api.state.searching).toBe(false)
    expect(api.state.error).toBe('')
  })

  it('empty query clears results without opening a stream', () => {
    const api = startAndConnect('needle')
    api.state.results = [{ name: 'a.go', path: 'a.go', matches: [], total: 0 }]

    api.state.query = ''
    api.startSearch('')

    expect(api.state.results).toEqual([])
    expect(api.state.searching).toBe(false)
    expect(api.state.error).toBe('')
    expect(MockEventSource.instances.length).toBe(1)
  })

  it('whitespace-only query is treated as empty', () => {
    const { state, startSearch } = useFileContentSearch()
    state.query = '   '
    startSearch('')
    vi.advanceTimersByTime(300)
    expect(MockEventSource.instances.length).toBe(0)
    expect(state.searching).toBe(false)
  })

  it('a new search cancels the previous stream', () => {
    const api = startAndConnect('first')
    expect(MockEventSource.instances.length).toBe(1)

    api.state.query = 'second'
    api.startSearch('')
    expect(MockEventSource.instances[0].closed).toBe(true)

    vi.advanceTimersByTime(300)
    expect(MockEventSource.instances.length).toBe(2)
  })

  it('starting a new search clears the previous error and results', () => {
    const api = startAndConnect('([bad')
    MockEventSource.instances[0].emit('error', { message: 'bad regex' })
    expect(api.state.error).toBe('bad regex')

    api.state.query = 'good'
    api.startSearch('')
    expect(api.state.error).toBe('')
    expect(api.state.results).toEqual([])
    expect(api.state.files).toBe(0)
  })

  it('cancelSearch closes the stream and stops the spinner', () => {
    const api = startAndConnect('needle')
    api.cancelSearch()
    expect(MockEventSource.instances[0].closed).toBe(true)
    expect(api.state.searching).toBe(false)
  })

  it('reset clears every field including options', () => {
    const api = startAndConnect('needle')
    api.state.include = '*.go'
    api.state.exclude = 'dist/**'
    api.state.error = 'x'

    api.reset()

    expect(api.state.query).toBe('')
    expect(api.state.results).toEqual([])
    expect(api.state.files).toBe(0)
    expect(api.state.matches).toBe(0)
    expect(api.state.truncated).toBe(false)
    expect(api.state.error).toBe('')
    expect(api.state.searchBasePath).toBe('')
    expect(MockEventSource.instances[0].closed).toBe(true)
  })

  it('ignores malformed result and done payloads', () => {
    const api = startAndConnect('needle')
    const es = MockEventSource.instances[0]
    // Raw (non-JSON) data — the listeners must not throw.
    expect(() => {
      es.emitRaw('result', 'not json')
      es.emitRaw('done', 'not json')
    }).not.toThrow()

    expect(api.state.results).toEqual([])
    // The stream ended, so the spinner must stop even though the totals were
    // unusable — otherwise the UI spins forever.
    expect(api.state.searching).toBe(false)
    expect(api.state.error).toBe('')
  })
})
