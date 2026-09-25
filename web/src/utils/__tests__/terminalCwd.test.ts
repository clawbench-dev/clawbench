import { describe, expect, it, vi, beforeEach } from 'vitest'

const mockApiGet = vi.fn()
vi.mock('@/utils/api', () => ({
  apiGet: (...args: unknown[]) => mockApiGet(...args),
}))

import { fetchTerminalCwd } from '../terminalCwd'

describe('fetchTerminalCwd', () => {
  beforeEach(() => {
    mockApiGet.mockReset()
  })

  it('returns the live cwd from the status endpoint', async () => {
    mockApiGet.mockResolvedValue({ cwd: '/tmp/scratch', hasSession: true })

    await expect(fetchTerminalCwd('sess-1')).resolves.toBe('/tmp/scratch')
    expect(mockApiGet).toHaveBeenCalledWith('/api/terminal/status?session=sess-1')
  })

  it('does not issue a request without a session id', async () => {
    // The endpoint's "all sessions" branch returns no cwd, so the round-trip
    // would be wasted.
    await expect(fetchTerminalCwd(undefined)).resolves.toBe('')
    await expect(fetchTerminalCwd('')).resolves.toBe('')
    expect(mockApiGet).not.toHaveBeenCalled()
  })

  it('returns empty string when the response carries no cwd', async () => {
    mockApiGet.mockResolvedValue({ hasSession: true })

    await expect(fetchTerminalCwd('sess-1')).resolves.toBe('')
  })

  it('returns empty string when the session is unknown', async () => {
    mockApiGet.mockResolvedValue({ hasSession: false })

    await expect(fetchTerminalCwd('gone')).resolves.toBe('')
  })

  it('swallows a request failure instead of throwing', async () => {
    // A failed lookup must not break the caller's action — the caller falls
    // back to the tab's launch directory.
    mockApiGet.mockRejectedValue(new Error('network down'))

    await expect(fetchTerminalCwd('sess-1')).resolves.toBe('')
  })

  it('url-encodes the session id', async () => {
    mockApiGet.mockResolvedValue({ cwd: '/x' })

    await fetchTerminalCwd('a b&c=d')

    expect(mockApiGet).toHaveBeenCalledWith('/api/terminal/status?session=a%20b%26c%3Dd')
  })
})
