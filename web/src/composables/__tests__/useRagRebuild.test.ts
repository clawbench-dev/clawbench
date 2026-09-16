import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { pollRebuildStatus, startRebuild, rebuildStatus, _resetRebuildStatusForTesting } from '@/composables/useRagRebuild'
import { apiGet, apiPost } from '@/utils/api'

vi.mock('@/utils/api', () => ({
  apiGet: vi.fn(),
  apiPost: vi.fn(),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const running = (processed: number, total: number, pct: number) => ({
  kind: 'fts' as const, status: 'running' as const, phase: 'resegmenting' as const,
  total, processed, progress_pct: pct, elapsed_ms: 0,
})

const done = () => ({
  kind: 'fts' as const, status: 'done' as const, phase: 'indexing' as const,
  total: 100, processed: 100, progress_pct: 100, elapsed_ms: 1000,
})

describe('useRagRebuild', () => {
  beforeEach(() => {
    vi.mocked(apiGet).mockReset()
    vi.mocked(apiPost).mockReset()
    _resetRebuildStatusForTesting()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('polls until the status leaves running, then returns the terminal status', async () => {
    vi.mocked(apiGet)
      .mockResolvedValueOnce(running(30, 100, 30))
      .mockResolvedValueOnce(running(70, 100, 70))
      .mockResolvedValueOnce(done())

    const promise = pollRebuildStatus()
    await vi.advanceTimersByTimeAsync(1000)
    await vi.advanceTimersByTimeAsync(1000)
    const status = await promise

    expect(status?.status).toBe('done')
    // Must not poll again after a terminal state.
    expect(apiGet).toHaveBeenCalledTimes(3)
    expect(apiGet).toHaveBeenCalledWith('/api/rag/rebuild/status')
  })

  it('publishes progress to the shared ref while running', async () => {
    // The panel renders this ref, so it must reflect each poll, not just the
    // final state. The second call is left pending so the running snapshot is
    // still in the ref when we assert.
    vi.mocked(apiGet)
      .mockResolvedValueOnce(running(25, 100, 25))
      .mockReturnValueOnce(new Promise(() => {}))

    const promise = pollRebuildStatus()
    await vi.advanceTimersByTimeAsync(1000)

    expect(rebuildStatus.value.status).toBe('running')
    expect(rebuildStatus.value.processed).toBe(25)
    expect(rebuildStatus.value.progress_pct).toBe(25)

    // Let the test finish without awaiting the pending poll.
    void promise
  })

  it('returns immediately when the first poll is already terminal', async () => {
    // Covers the race where the rebuild finished before the first poll.
    vi.mocked(apiGet).mockResolvedValueOnce(done())

    const status = await pollRebuildStatus()

    expect(status?.status).toBe('done')
    expect(apiGet).toHaveBeenCalledTimes(1)
  })

  it('keeps polling through a transient poll failure', async () => {
    // The rebuild continues server-side, so a blip must not be reported as
    // a rebuild failure.
    vi.mocked(apiGet)
      .mockRejectedValueOnce(new Error('network blip'))
      .mockResolvedValueOnce(done())

    const promise = pollRebuildStatus()
    await vi.advanceTimersByTimeAsync(1000)
    const status = await promise

    expect(status?.status).toBe('done')
  })

  it('stops polling and returns null when cancelled', async () => {
    vi.mocked(apiGet).mockResolvedValue(running(10, 100, 10))
    let cancelled = false

    const promise = pollRebuildStatus(() => cancelled)
    cancelled = true
    await vi.advanceTimersByTimeAsync(1000)
    const status = await promise

    expect(status).toBeNull()
    expect(vi.mocked(apiGet).mock.calls.length).toBeLessThanOrEqual(1)
  })

  it('reports a blocked status distinctly from an error', async () => {
    // A vector rebuild whose embedding service is down is blocked, not failed:
    // nothing is broken, it just needs the dependency back.
    vi.mocked(apiGet).mockResolvedValueOnce({
      kind: 'vector', status: 'blocked', phase: 'embedding',
      total: 100, processed: 0, progress_pct: 0, elapsed_ms: 10,
      error: 'embedding service unavailable',
    })

    const status = await pollRebuildStatus()

    expect(status?.status).toBe('blocked')
    expect(status?.error).toBe('embedding service unavailable')
  })

  it('startRebuild sends the kind and then polls', async () => {
    vi.mocked(apiPost).mockResolvedValue({ status: 'accepted', kind: 'full' })
    vi.mocked(apiGet).mockResolvedValueOnce(done())

    await startRebuild('full')

    expect(apiPost).toHaveBeenCalledWith('/api/rag/rebuild', { kind: 'full' })
    expect(apiGet).toHaveBeenCalledTimes(1)
  })

  it('startRebuild propagates a rejected trigger so the caller can show the reason', async () => {
    const refusal = new Error('已拒绝重建：分词器不可用') as Error & { msgKey?: string }
    refusal.msgKey = 'RAGSegmenterUnavailable'
    vi.mocked(apiPost).mockRejectedValue(refusal)

    await expect(startRebuild('fts')).rejects.toThrow('分词器不可用')
    // A refused trigger must not start polling.
    expect(apiGet).not.toHaveBeenCalled()
  })
})
