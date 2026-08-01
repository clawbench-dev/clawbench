import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// Mock appLog before importing the composable
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import { useMessageClusters, resetMessageClustersState } from '@/composables/useMessageClusters'

// ── Helpers ──

function mockFetchResponse(data: unknown, status = 200) {
  return Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(data),
  } as Response)
}

const CLUSTERS_URL = '/api/chat/message-clusters'
const COMPUTE_URL = '/api/chat/message-clusters/compute'
const STATUS_URL = '/api/chat/message-clusters/compute/status'

const sampleClusters = [
  {
    id: 1,
    representative: 'hello',
    variants: ['hi', 'hey'],
    total_count: 10,
    representative_count: 5,
  },
  {
    id: 2,
    representative: 'thanks',
    variants: ['thank you', 'thx'],
    total_count: 8,
    representative_count: 4,
  },
]

const sampleResponse = {
  clusters: sampleClusters,
  total: 18,
  mode: 'kmeans',
  progress: 'done',
  updated_at: '2026-08-01T12:00:00Z',
}

describe('useMessageClusters', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.restoreAllMocks()
    resetMessageClustersState()
    globalThis.fetch = vi.fn()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  describe('fetchClusters', () => {
    it('fetches clusters from API', async () => {
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse(sampleResponse))

      const { clusters, loaded, loading, mode, updatedAt, progress, fetchClusters } = useMessageClusters()
      await fetchClusters()

      expect(fetch).toHaveBeenCalledWith(CLUSTERS_URL)
      expect(clusters.value).toEqual(sampleClusters)
      expect(mode.value).toBe('kmeans')
      expect(updatedAt.value).toBe('2026-08-01T12:00:00Z')
      expect(progress.value.status).toBe('done')
      expect(loaded.value).toBe(true)
      expect(loading.value).toBe(false)
    })

    it('handles empty response', async () => {
      const emptyResponse = {
        clusters: [],
        total: 0,
        mode: '',
        progress: 'idle',
        updated_at: '',
      }
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse(emptyResponse))

      const { clusters, mode, progress, loaded, fetchClusters } = useMessageClusters()
      await fetchClusters()

      expect(clusters.value).toEqual([])
      expect(mode.value).toBe('')
      expect(progress.value.status).toBe('idle')
      expect(loaded.value).toBe(true)
    })

    it('handles fetch error gracefully', async () => {
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse(null, 500))

      const { clusters, loaded, fetchClusters } = useMessageClusters()
      await fetchClusters()

      expect(clusters.value).toEqual([])
      expect(loaded.value).toBe(false)
    })
  })

  describe('startCompute', () => {
    it('starts computation and returns started', async () => {
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse({}, 200))

      const { computing, progress, startCompute } = useMessageClusters()
      const result = await startCompute()

      expect(fetch).toHaveBeenCalledWith(COMPUTE_URL, { method: 'POST' })
      expect(computing.value).toBe(true)
      expect(progress.value.status).toBe('computing')
      expect(result).toBe('started')
    })

    it('handles 409 already running and returns already_running', async () => {
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse({}, 409))

      const { computing, startCompute } = useMessageClusters()
      const result = await startCompute()

      expect(computing.value).toBe(false)
      expect(fetch).toHaveBeenCalledTimes(1)
      expect(result).toBe('already_running')
    })

    it('handles POST error gracefully and returns error', async () => {
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse({}, 500))

      const { computing, startCompute } = useMessageClusters()
      const result = await startCompute()

      expect(computing.value).toBe(false)
      expect(result).toBe('error')
    })
  })

  describe('progress polling', () => {
    it('polls progress and stops on done', async () => {
      // Mock compute POST
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse({}, 200))

      const { computing, progress, clusters, loaded, startCompute, stopPolling, pollProgress } = useMessageClusters()
      await startCompute()
      pollProgress() // start polling manually (drawer would call this)

      // First poll: still computing
      const computingProgress = {
        status: 'computing',
        phase: 'clustering',
        msg_count: 50,
        cluster_count: 0,
        elapsed_ms: 1000,
        mode: 'kmeans',
      }
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse(computingProgress))

      vi.advanceTimersByTime(2000)
      await vi.waitFor(() => {
        expect(progress.value.phase).toBe('clustering')
      })

      // Second poll: done
      const doneProgress = {
        status: 'done',
        phase: 'saving',
        msg_count: 100,
        cluster_count: 5,
        elapsed_ms: 3000,
        mode: 'kmeans',
      }
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse(doneProgress))
      // After done, fetchClusters is called
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse(sampleResponse))

      vi.advanceTimersByTime(2000)
      await vi.waitFor(() => {
        expect(computing.value).toBe(false)
        expect(progress.value.status).toBe('done')
      })

      await vi.waitFor(() => {
        expect(clusters.value).toEqual(sampleClusters)
        expect(loaded.value).toBe(true)
      })

      // No more polling after done
      const callCount = vi.mocked(fetch).mock.calls.length
      vi.advanceTimersByTime(2000)
      expect(vi.mocked(fetch).mock.calls.length).toBe(callCount)
    })

    it('polls progress and stops on error', async () => {
      // Mock compute POST
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse({}, 200))

      const { computing, progress, startCompute, pollProgress } = useMessageClusters()
      await startCompute()
      pollProgress()

      // First poll returns error status
      const errorProgress = {
        status: 'error',
        phase: 'extracting',
        msg_count: 0,
        cluster_count: 0,
        elapsed_ms: 500,
        mode: 'kmeans',
        error: 'embedding failed',
      }
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse(errorProgress))

      vi.advanceTimersByTime(2000)
      await vi.waitFor(() => {
        expect(computing.value).toBe(false)
        expect(progress.value.status).toBe('error')
        expect(progress.value.error).toBe('embedding failed')
      })

      // No more polling after error
      const callCount = vi.mocked(fetch).mock.calls.length
      vi.advanceTimersByTime(2000)
      expect(vi.mocked(fetch).mock.calls.length).toBe(callCount)
    })

    it('continues polling when status is still computing', async () => {
      // Mock compute POST
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse({}, 200))

      const { startCompute, stopPolling, computing, pollProgress } = useMessageClusters()
      await startCompute()
      pollProgress()

      // Poll 1: computing
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse({
        status: 'computing', phase: 'extracting', msg_count: 10, cluster_count: 0, elapsed_ms: 500, mode: 'kmeans',
      }))
      vi.advanceTimersByTime(2000)
      await vi.waitFor(() => expect(fetch).toHaveBeenCalledWith(STATUS_URL))

      // Poll 2: still computing
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse({
        status: 'computing', phase: 'clustering', msg_count: 50, cluster_count: 0, elapsed_ms: 1500, mode: 'kmeans',
      }))
      vi.advanceTimersByTime(2000)
      await vi.waitFor(() => expect(computing.value).toBe(true))

      // Poll 3: done — stops polling
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse({
        status: 'done', phase: 'saving', msg_count: 100, cluster_count: 5, elapsed_ms: 3000, mode: 'kmeans',
      }))
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse(sampleResponse))
      vi.advanceTimersByTime(2000)
      await vi.waitFor(() => expect(computing.value).toBe(false))
    })
  })

  describe('stopPolling', () => {
    it('clears interval and stops further polls', async () => {
      // Mock compute POST
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse({}, 200))

      const { startCompute, stopPolling, pollProgress } = useMessageClusters()
      await startCompute()
      pollProgress()

      // Let first poll happen
      vi.mocked(fetch).mockResolvedValueOnce(mockFetchResponse({
        status: 'computing', phase: 'extracting', msg_count: 10, cluster_count: 0, elapsed_ms: 500, mode: 'kmeans',
      }))
      vi.advanceTimersByTime(2000)
      await vi.waitFor(() => expect(fetch).toHaveBeenCalledWith(STATUS_URL))

      stopPolling()
      const callCount = vi.mocked(fetch).mock.calls.length

      vi.advanceTimersByTime(6000)
      expect(vi.mocked(fetch).mock.calls.length).toBe(callCount)
    })
  })
})
