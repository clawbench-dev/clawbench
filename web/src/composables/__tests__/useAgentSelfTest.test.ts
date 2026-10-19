import { describe, expect, it, vi, beforeEach } from 'vitest'

const mockApiPost = vi.fn()
vi.mock('@/utils/api', () => ({
  apiPost: (...args: unknown[]) => mockApiPost(...args),
}))
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import { useAgentSelfTest } from '@/composables/useAgentSelfTest'

const okResult = {
  agentId: 'claude',
  ok: true,
  passed: 2,
  failed: 0,
  warned: 0,
  checks: [{ key: 'enabled', status: 'ok' as const }],
}

describe('useAgentSelfTest', () => {
  beforeEach(() => {
    mockApiPost.mockReset()
  })

  it('posts to the agent-scoped self-test endpoint', async () => {
    mockApiPost.mockResolvedValue(okResult)
    const { run } = useAgentSelfTest()

    await run('claude')

    expect(mockApiPost).toHaveBeenCalledTimes(1)
    const [url] = mockApiPost.mock.calls[0]
    expect(url).toBe('/api/agents/claude/self-test')
  })

  it('encodes the agent id so a slash cannot escape the route', async () => {
    mockApiPost.mockResolvedValue(okResult)
    const { run } = useAgentSelfTest()

    await run('weird/id')

    const [url] = mockApiPost.mock.calls[0]
    expect(url).toBe('/api/agents/weird%2Fid/self-test')
  })

  it('exposes the result and clears the testing flag', async () => {
    mockApiPost.mockResolvedValue(okResult)
    const { run, testing, result } = useAgentSelfTest()

    const p = run('claude')
    expect(testing.value).toBe(true)
    await p

    expect(testing.value).toBe(false)
    expect(result.value?.ok).toBe(true)
    expect(result.value?.checks).toHaveLength(1)
  })

  it('surfaces the request error instead of a generic failure', async () => {
    mockApiPost.mockRejectedValue(new Error('Agent not found'))
    const { run, error, result, testing } = useAgentSelfTest()

    await run('ghost')

    expect(error.value).toBe('Agent not found')
    expect(result.value).toBeNull()
    expect(testing.value).toBe(false)
  })

  it('clears the previous result before each run so a stale pass cannot persist', async () => {
    mockApiPost.mockResolvedValueOnce(okResult)
    const { run, result } = useAgentSelfTest()
    await run('claude')
    expect(result.value).not.toBeNull()

    mockApiPost.mockRejectedValueOnce(new Error('boom'))
    await run('claude')
    expect(result.value).toBeNull()
  })

  it('clear() resets both result and error', async () => {
    mockApiPost.mockRejectedValue(new Error('boom'))
    const { run, clear, result, error } = useAgentSelfTest()
    await run('claude')
    expect(error.value).toBe('boom')

    clear()
    expect(result.value).toBeNull()
    expect(error.value).toBe('')
  })
})
