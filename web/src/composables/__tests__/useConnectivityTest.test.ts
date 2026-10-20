import { describe, expect, it, vi, beforeEach } from 'vitest'

// `apiPost` is mocked at the module boundary: the composable's whole job is
// deciding WHAT it sends (and with which timeout), so the request arguments are
// the behaviour under test.
const apiPost = vi.fn()
vi.mock('@/utils/api', () => ({
  apiPost: (...args: unknown[]) => apiPost(...args),
}))

import { useConnectivityTest } from '@/composables/useConnectivityTest'

describe('useConnectivityTest', () => {
  beforeEach(() => {
    apiPost.mockReset()
    apiPost.mockResolvedValue({ success: true, message: 'ok' })
  })

  it('sends one request per test with its category and values', async () => {
    const { runTests, testResults } = useConnectivityTest()
    await runTests([
      { category: 'rag', values: { a: 1 } },
      { category: 'tts', values: { b: 2 } },
    ])

    expect(apiPost).toHaveBeenCalledTimes(2)
    expect(apiPost).toHaveBeenNthCalledWith(
      1,
      '/api/config/test',
      { category: 'rag', values: { a: 1 } },
      { timeoutMs: 15_000 },
    )
    expect(apiPost).toHaveBeenNthCalledWith(
      2,
      '/api/config/test',
      { category: 'tts', values: { b: 2 } },
      { timeoutMs: 15_000 },
    )
    expect(testResults.value).toHaveLength(2)
  })

  it('honours a per-test timeoutMs instead of the 15s default', async () => {
    // MCP probes a real stdio server, which can spend tens of seconds fetching
    // its package on first use; the generic 15s expires before it answers.
    const { runTests } = useConnectivityTest()
    await runTests([{ category: 'mcp', values: {}, timeoutMs: 60_000 }])

    expect(apiPost).toHaveBeenCalledWith(
      '/api/config/test',
      { category: 'mcp', values: {} },
      { timeoutMs: 60_000 },
    )
  })

  it('falls back to 15s when timeoutMs is explicitly undefined', async () => {
    const { runTests } = useConnectivityTest()
    await runTests([{ category: 'rag', values: {}, timeoutMs: undefined }])

    expect(apiPost).toHaveBeenCalledWith(
      '/api/config/test',
      expect.anything(),
      { timeoutMs: 15_000 },
    )
  })

  it('keeps the other results when one request rejects', async () => {
    apiPost.mockImplementation((_url: string, body: { category: string }) => {
      if (body.category === 'bad') return Promise.reject(new Error('boom'))
      return Promise.resolve({ success: true, message: 'ok' })
    })

    const { runTests, testResults } = useConnectivityTest()
    await runTests([
      { category: 'bad', values: {} },
      { category: 'good', values: {} },
    ])

    expect(testResults.value).toHaveLength(2)
    expect(testResults.value[0]).toEqual({ success: false, message: 'boom' })
    expect(testResults.value[1]).toEqual({ success: true, message: 'ok' })
  })

  it('turns a non-Error rejection into a readable failure', async () => {
    apiPost.mockRejectedValue('nope')

    const { runTests, testResults } = useConnectivityTest()
    await runTests([{ category: 'rag', values: {} }])

    expect(testResults.value[0]).toEqual({ success: false, message: 'Test failed' })
  })

  it('toggles testing around the run and clears previous results up front', async () => {
    let resolveRequest: (v: unknown) => void = () => {}
    apiPost.mockImplementation(() => new Promise(res => { resolveRequest = res }))

    const { runTests, testing, testResults } = useConnectivityTest()
    testResults.value = [{ success: true, message: 'stale' }]

    const run = runTests([{ category: 'rag', values: {} }])
    expect(testing.value).toBe(true)
    expect(testResults.value).toEqual([])

    resolveRequest({ success: true, message: 'fresh' })
    await run
    expect(testing.value).toBe(false)
    expect(testResults.value).toEqual([{ success: true, message: 'fresh' }])
  })

  it('clearResults empties the list', () => {
    const { clearResults, testResults } = useConnectivityTest()
    testResults.value = [{ success: true, message: 'x' }]
    clearResults()
    expect(testResults.value).toEqual([])
  })
})
