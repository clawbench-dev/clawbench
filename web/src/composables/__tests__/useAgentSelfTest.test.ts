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

  // ─── deep mode ─────────────────────────────────

  it('posts to the deep endpoint and exposes the reply', async () => {
    mockApiPost.mockResolvedValue({
      ok: true,
      deep: { reply: 'OK', transport: 'acp-stdio', stopReason: 'end_turn', durationMs: 900 },
      static: okResult,
    })
    const { runDeep, deepResult, result } = useAgentSelfTest()

    await runDeep('claude')

    const [url, , opts] = mockApiPost.mock.calls[0]
    expect(url).toBe('/api/agents/claude/self-test-deep')
    // A real turn can take up to ~90s server-side, so the client timeout must
    // exceed that or the UI would report a failure for a working agent.
    expect((opts as { timeoutMs: number }).timeoutMs).toBeGreaterThanOrEqual(90_000)

    expect(deepResult.value?.reply).toBe('OK')
    expect(deepResult.value?.stopReason).toBe('end_turn')
    // The static checks ride along in the response, but a SUCCESSFUL turn must
    // NOT surface them: pressing "run a real turn" should not print the 12
    // configuration rows, or the two buttons look like they do the same thing.
    expect(result.value).toBeNull()
  })

  it('surfaces the blocking checks when a real turn is refused', async () => {
    // A refusal is a static-check failure, and the rows are the only thing that
    // says WHICH check blocked the turn. Hiding them would leave a bare
    // "configuration checks failed" with nothing to act on.
    mockApiPost.mockResolvedValue({
      ok: false,
      error: 'The configuration checks failed; fix them before running a real turn.',
      static: { agentId: 'claude', ok: false, passed: 1, failed: 1, warned: 0,
        checks: [{ key: 'acp_command', status: 'fail', detail: 'Executable "nope" was not found' }] },
    })
    const { runDeep, deepResult, result, error } = useAgentSelfTest()

    await runDeep('claude')

    expect(deepResult.value).toBeNull()
    expect(error.value).toContain('configuration checks failed')
    expect(result.value?.failed).toBe(1)
    expect(result.value?.checks[0].detail).toContain('nope')
  })

  it('does not surface an all-passing static report on a failed turn', async () => {
    // A turn that started and then failed (spawn/handshake) carries the passing
    // pre-check report. Printing it would bury the real error under 12 green
    // rows, so only a REFUSAL (failed > 0) brings the rows back.
    mockApiPost.mockResolvedValue({
      ok: false,
      error: 'the agent closed the stream without a terminal event',
      static: okResult,
    })
    const { runDeep, result, error } = useAgentSelfTest()

    await runDeep('claude')

    expect(error.value).toContain('terminal event')
    expect(result.value).toBeNull()
  })

  it('reports a refusal (static checks failed) as an error with the static result', async () => {
    mockApiPost.mockResolvedValue({
      ok: false,
      error: 'The configuration checks failed; fix them before running a real turn.',
      static: { agentId: 'claude', ok: false, passed: 3, failed: 2, warned: 0, checks: [] },
    })
    const { runDeep, deepResult, result, error } = useAgentSelfTest()

    await runDeep('claude')

    expect(deepResult.value).toBeNull()
    expect(error.value).toContain('configuration checks failed')
    // The failing checks must still be visible, otherwise the user sees only a
    // vague refusal with no way to act.
    expect(result.value?.failed).toBe(2)
  })

  it('clears a previous deep result before a new shallow run', async () => {
    mockApiPost.mockResolvedValueOnce({ ok: true, deep: { reply: 'old' } })
    const { run, runDeep, deepResult } = useAgentSelfTest()
    await runDeep('claude')
    expect(deepResult.value).not.toBeNull()

    mockApiPost.mockResolvedValueOnce(okResult)
    await run('claude')
    expect(deepResult.value).toBeNull()
  })
})
