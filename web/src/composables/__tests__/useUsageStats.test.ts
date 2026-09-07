import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// Mock apiGet
const mockApiGet = vi.fn()
vi.mock('@/utils/api', () => ({
  apiGet: (...args: unknown[]) => mockApiGet(...args),
}))

// Mock useLocale
vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

// Mock appLog
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import { useUsageStats } from '@/composables/useUsageStats'
import { nextTick } from 'vue'

const EMPTY_RESPONSE = {
  totals: { input: 0, output: 0, total: 0, cacheHit: 0, cacheMiss: 0, credit: 0, costUsd: 0, messageCnt: 0 },
  rows: [],
}

describe('useUsageStats', () => {
  beforeEach(async () => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    mockApiGet.mockReset()
    mockApiGet.mockResolvedValue(EMPTY_RESPONSE)
    // Reset singleton state and flush any pending load.
    const stats = useUsageStats()
    await stats.resetStatsFilter()
    await Promise.resolve()
    mockApiGet.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  describe('loadStats URL building', () => {
    it('builds default 24h request with dims/metrics repeated, default sort', async () => {
      const stats = useUsageStats()
      mockApiGet.mockResolvedValue(EMPTY_RESPONSE)
      await stats.loadStats()

      expect(mockApiGet).toHaveBeenCalledTimes(1)
      const [url, opts] = mockApiGet.mock.calls[0]
      expect(url.startsWith('/api/usage/stats?')).toBe(true)
      const parsed = new URLSearchParams(url.split('?')[1])
      expect(parsed.get('start')).toBeTruthy()
      expect(parsed.get('end')).toBeTruthy()
      expect(parsed.getAll('dims')).toEqual(['model'])
      expect(parsed.getAll('metrics')).toEqual(['total'])
      expect(parsed.get('sort')).toBe('total')
      expect(parsed.get('order')).toBe('desc')
      expect(parsed.get('trend')).toBeNull()
      expect(opts.signal).toBeInstanceOf(AbortSignal)
    })

    it('adds trend=true when chart type is trend', async () => {
      const stats = useUsageStats()
      mockApiGet.mockResolvedValue({ ...EMPTY_RESPONSE, trend: [] })
      stats.setChartType('trend')
      await nextTick()
      await nextTick()

      expect(mockApiGet).toHaveBeenCalledTimes(1)
      const [url] = mockApiGet.mock.calls[0]
      const parsed = new URLSearchParams(url.split('?')[1])
      expect(parsed.get('trend')).toBe('1')
    })

    it('reflects dim/metric changes in the URL after the debounce window', async () => {
      const stats = useUsageStats()
      mockApiGet.mockResolvedValue(EMPTY_RESPONSE)
      stats.setDims(['model', 'agent'])
      stats.setMetrics(['total', 'cost'])
      await nextTick()
      // Debounced reload fires 300ms after the last change.
      vi.advanceTimersByTime(320)
      await Promise.resolve()

      expect(mockApiGet).toHaveBeenCalled()
      const [url] = mockApiGet.mock.calls[mockApiGet.mock.calls.length - 1]
      const parsed = new URLSearchParams(url.split('?')[1])
      expect(parsed.getAll('dims')).toEqual(['model', 'agent'])
      expect(parsed.getAll('metrics')).toEqual(['total', 'cost'])
    })

    it('coalesces rapid setMetrics calls into a single debounced request', async () => {
      const stats = useUsageStats()
      mockApiGet.mockResolvedValue(EMPTY_RESPONSE)
      stats.setMetrics(['total'])
      stats.setMetrics(['total', 'cost'])
      stats.setMetrics(['total', 'cost', 'input'])
      await nextTick()
      vi.advanceTimersByTime(320)
      await Promise.resolve()

      const calls = mockApiGet.mock.calls.length
      expect(calls).toBe(1)
      const parsed = new URLSearchParams(mockApiGet.mock.calls[0][0].split('?')[1])
      expect(parsed.getAll('metrics')).toEqual(['total', 'cost', 'input'])
    })
  })

  describe('derived totals', () => {
    it('shows only nonzero additive cards incl. cache hit tokens', () => {
      const stats = useUsageStats()
      stats.raw.value = {
        totals: { input: 100, output: 0, total: 100, cacheHit: 80, cacheMiss: 20, credit: 0, costUsd: 0.05, messageCnt: 1 },
        rows: [],
      }
      const visible = stats.visibleTotals.value
      const ids = visible.map(c => c.metric)
      // output, credit are 0 → hidden; cost shown. cacheHit tokens are a card;
      // cacheMiss is not a standalone card (shown via the drill-down donut).
      expect(ids).toContain('input')
      expect(ids).toContain('total')
      expect(ids).toContain('cacheHit')
      expect(ids).toContain('cost')
      expect(ids).not.toContain('output')
      expect(ids).not.toContain('credit')
      expect(ids).not.toContain('cacheMiss')
    })
  })

  describe('error propagation', () => {
    it('captures status, msgKey and message from thrown api error', async () => {
      const stats = useUsageStats()
      const err = new Error('bad request') as Error & { status?: number; msgKey?: string }
      err.status = 400
      err.msgKey = 'InvalidRequest'
      mockApiGet.mockRejectedValue(err)
      await stats.loadStats()
      expect(stats.error.value).toEqual({ status: 400, msgKey: 'InvalidRequest', message: 'bad request' })
      // raw retains the last successful response (stale data is kept on error).
      expect(stats.raw.value).toEqual(EMPTY_RESPONSE)
    })
  })

  describe('sort guard', () => {
    it('deselecting the sort column re-assigns sort to first selected metric', async () => {
      const stats = useUsageStats()
      stats.filter.value.metrics = ['total', 'cost']
      stats.filter.value.sortBy = 'cost'
      stats.setMetrics(['total']) // deselect cost
      expect(stats.filter.value.metrics).toEqual(['total'])
      expect(stats.filter.value.sortBy).toBe('total')
      vi.advanceTimersByTime(320)
      await Promise.resolve()
    })

    it('sort toggles direction when re-clicking the active sort column', async () => {
      const stats = useUsageStats()
      mockApiGet.mockResolvedValue(EMPTY_RESPONSE)
      stats.setSort('total') // default sort is already total → toggles desc→asc
      await nextTick()
      expect(stats.filter.value.sortDesc).toBe(false)
    })
  })
})
