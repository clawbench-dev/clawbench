import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'

const mockApiGet = vi.fn()
vi.mock('@/utils/api', () => ({
  apiGet: (...args: unknown[]) => mockApiGet(...args),
}))
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import { useGitCodeStats, resetGitStats } from '@/composables/useGitCodeStats'

const GIT_RESP = {
  isGit: true,
  totals: { added: 2000, deleted: 0, net: 2000, commitCnt: 1 },
  rows: [],
  trend: [],
}

function routeMock(usageResp: unknown) {
  mockApiGet.mockImplementation((url: string) =>
    Promise.resolve(String(url).startsWith('/api/usage/stats') ? usageResp : GIT_RESP))
}

function usageCalls(): URLSearchParams[] {
  return mockApiGet.mock.calls
    .map(c => String(c[0]))
    .filter(u => u.startsWith('/api/usage/stats'))
    .map(u => new URLSearchParams(u.split('?')[1]))
}

describe('useGitCodeStats usage totals', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetGitStats()
  })

  it('loads usage totals alongside the git stats in one refresh', async () => {
    routeMock({ totals: { total: 500_000, credit: 50, costUsd: 5 }, rows: [] })
    const stats = useGitCodeStats()
    await stats.loadGitStats()
    await flushPromises()

    expect(stats.usageTotals.value).toEqual({ total: 500_000, credit: 50, costUsd: 5 })
    expect(stats.usageLoading.value).toBe(false)
    expect(stats.usageError.value).toBeNull()
    expect(usageCalls()).toHaveLength(1)
  })

  it('treats an all-zero aggregate as "no usage" rather than zero spend', async () => {
    // The backend returns a zeroed totals object when no rows fall in the
    // window; keeping it would render "0 tokens / 1K lines", which reads as a
    // free run instead of a missing measurement.
    routeMock({ totals: { total: 0, credit: 0, costUsd: 0 }, rows: [] })
    const stats = useGitCodeStats()
    await stats.loadGitStats()
    await flushPromises()
    expect(stats.usageTotals.value).toBeNull()
  })

  it('records the error and clears stale totals when the usage query fails', async () => {
    routeMock({ totals: { total: 1, credit: 1, costUsd: 1 }, rows: [] })
    const stats = useGitCodeStats()
    await stats.loadGitStats()
    await flushPromises()
    expect(stats.usageTotals.value).not.toBeNull()

    mockApiGet.mockImplementation((url: string) => (
      String(url).startsWith('/api/usage/stats')
        ? Promise.reject({ message: 'usage down', status: 500 })
        : Promise.resolve(GIT_RESP)
    ))
    await stats.loadGitStats()
    await flushPromises()

    // A stale ratio must not survive a failed refresh.
    expect(stats.usageTotals.value).toBeNull()
    expect(stats.usageError.value?.status).toBe(500)
    // The git half still succeeded — one failure must not blank the other.
    expect(stats.raw.value?.totals?.net).toBe(2000)
  })

  it('clears the previous window\'s totals as soon as a new query starts', async () => {
    // The two halves arrive independently. Without clearing here, a fast git
    // response would pair the NEW net lines with the PREVIOUS window's spend —
    // a number that looks real but describes no window at all.
    routeMock({ totals: { total: 500_000, credit: 50, costUsd: 5 }, rows: [] })
    const stats = useGitCodeStats()
    await stats.loadGitStats()
    await flushPromises()
    expect(stats.usageTotals.value).toEqual({ total: 500_000, credit: 50, costUsd: 5 })

    let releaseUsage: ((v: unknown) => void) | null = null
    mockApiGet.mockImplementation((url: string) => (
      String(url).startsWith('/api/usage/stats')
        ? new Promise(r => { releaseUsage = r as (v: unknown) => void })
        : Promise.resolve(GIT_RESP)
    ))
    const pending = stats.loadGitStats()
    await pending
    await flushPromises()

    // Git landed; usage is still in flight. The stale totals must be gone and
    // the loading flag must be up, so the panel shows "loading" not a ratio.
    expect(stats.raw.value?.totals?.net).toBe(2000)
    expect(stats.usageTotals.value).toBeNull()
    expect(stats.usageLoading.value).toBe(true)

    releaseUsage!({ totals: { total: 800_000, credit: 80, costUsd: 8 }, rows: [] })
    await flushPromises()
    expect(stats.usageTotals.value).toEqual({ total: 800_000, credit: 80, costUsd: 8 })
    expect(stats.usageLoading.value).toBe(false)
  })

  it('resetGitStats clears the usage totals so a project switch cannot reuse them', async () => {
    routeMock({ totals: { total: 500_000, credit: 50, costUsd: 5 }, rows: [] })
    const stats = useGitCodeStats()
    await stats.loadGitStats()
    await flushPromises()
    expect(stats.usageTotals.value).not.toBeNull()

    resetGitStats()
    expect(stats.usageTotals.value).toBeNull()
    expect(stats.usageLoading.value).toBe(false)
    expect(stats.usageError.value).toBeNull()
  })
})
