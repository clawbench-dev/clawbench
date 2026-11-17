import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const mockApiPost = vi.fn()
vi.mock('@/utils/api', () => ({
  apiPost: (...args: unknown[]) => mockApiPost(...args),
}))

import { installBackend, useAgentInstall } from '@/composables/useAgentInstall'

beforeEach(() => {
  mockApiPost.mockReset()
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('installBackend', () => {
  it('posts to the backend-keyed endpoint and never sends a command', async () => {
    // The server owns the command (it looks it up in its own registry). If the
    // client ever started sending one, this endpoint would become "run this
    // string on the server" for any authenticated caller.
    mockApiPost.mockResolvedValue({ ok: true, backend: 'claude', command: 'npm i -g x' })

    await installBackend('claude')

    expect(mockApiPost).toHaveBeenCalledTimes(1)
    const [url, body] = mockApiPost.mock.calls[0]
    expect(url).toBe('/api/backends/claude/install')
    expect(body).toEqual({})
  })

  it('url-encodes the backend id', async () => {
    mockApiPost.mockResolvedValue({ ok: true })
    await installBackend('weird id/../x')
    expect(mockApiPost.mock.calls[0][0]).toBe('/api/backends/weird%20id%2F..%2Fx/install')
  })

  it('waits longer than the server budget', async () => {
    // The server allows 10 minutes. A client that gave up sooner would report a
    // failure while the install kept running — a phantom error that invites a
    // second, conflicting attempt.
    mockApiPost.mockResolvedValue({ ok: true })
    await installBackend('claude')
    const opts = mockApiPost.mock.calls[0][2] as { timeoutMs: number }
    expect(opts.timeoutMs).toBeGreaterThan(10 * 60 * 1000)
  })
})

describe('useAgentInstall', () => {
  it('surfaces the server message on failure', async () => {
    mockApiPost.mockRejectedValue(new Error('未检测到 npm，请先安装 Node.js'))
    const { error, installing, result, run } = useAgentInstall()

    const out = await run('claude')

    expect(out).toBeNull()
    expect(error.value).toBe('未检测到 npm，请先安装 Node.js')
    expect(result.value).toBeNull()
    expect(installing.value).toBe(false)
  })

  it('keeps the result on success and clears the error', async () => {
    mockApiPost.mockResolvedValue({ ok: true, backend: 'claude', refreshed: true, output: 'ok' })
    const { error, installing, result, run } = useAgentInstall()

    const out = await run('claude')

    expect(out).not.toBeNull()
    expect(result.value?.refreshed).toBe(true)
    expect(error.value).toBe('')
    expect(installing.value).toBe(false)
  })

  it('refuses to start a second install while one is running', async () => {
    let resolveFirst!: (v: unknown) => void
    mockApiPost.mockReturnValue(new Promise((res) => { resolveFirst = res }))
    const { installing, run } = useAgentInstall()

    const first = run('claude')
    expect(installing.value).toBe(true)

    const second = await run('claude')
    expect(second).toBeNull()
    // Only one request: the server refuses concurrent installs anyway, so firing
    // the second would just produce a confusing "busy" error.
    expect(mockApiPost).toHaveBeenCalledTimes(1)

    resolveFirst({ ok: true, refreshed: true })
    await first
    expect(installing.value).toBe(false)
  })

  it('does not retry a failed install on its own', async () => {
    // A retry loop here would hammer npm; the user decides whether to try again.
    mockApiPost.mockRejectedValue(new Error('boom'))
    const { run } = useAgentInstall()

    await run('claude')
    expect(mockApiPost).toHaveBeenCalledTimes(1)
  })
})
