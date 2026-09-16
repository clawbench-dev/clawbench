import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { pollFtsRebuildStatus, startFtsRebuild } from '@/composables/useFtsRebuild'
import { apiGet, apiPost } from '@/utils/api'

vi.mock('@/utils/api', () => ({
  apiGet: vi.fn(),
  apiPost: vi.fn(),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const running = (pct: number) => ({
  status: 'running' as const, phase: 'resegmenting', total: 100, processed: pct,
  progress_pct: pct, indexed: 0, resegmented: 0, elapsed_ms: 0,
})

const done = (resegmented: number) => ({
  status: 'done' as const, phase: 'indexing', total: 100, processed: 100,
  progress_pct: 100, indexed: 100, resegmented, elapsed_ms: 1000,
})

describe('useFtsRebuild', () => {
  beforeEach(() => {
    vi.mocked(apiGet).mockReset()
    vi.mocked(apiPost).mockReset()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('polls until the status leaves running, then returns the terminal status', async () => {
    vi.mocked(apiGet)
      .mockResolvedValueOnce(running(30))
      .mockResolvedValueOnce(running(70))
      .mockResolvedValueOnce(done(5))

    const promise = pollFtsRebuildStatus()
    // Advance past each poll interval so the loop makes progress.
    await vi.advanceTimersByTimeAsync(1000)
    await vi.advanceTimersByTimeAsync(1000)
    const status = await promise

    expect(status?.status).toBe('done')
    expect(status?.resegmented).toBe(5)
    // Must not poll again after a terminal state.
    expect(apiGet).toHaveBeenCalledTimes(3)
    expect(apiGet).toHaveBeenCalledWith('/api/rag/rebuild-fts/status')
  })

  it('returns immediately when the first poll is already terminal', async () => {
    // Covers the race where the rebuild finished before the first poll.
    vi.mocked(apiGet).mockResolvedValueOnce(done(0))

    const status = await pollFtsRebuildStatus()

    expect(status?.status).toBe('done')
    expect(apiGet).toHaveBeenCalledTimes(1)
  })

  it('keeps polling through a transient poll failure', async () => {
    // The rebuild continues server-side, so a blip must not be reported as
    // a rebuild failure.
    vi.mocked(apiGet)
      .mockRejectedValueOnce(new Error('network blip'))
      .mockResolvedValueOnce(done(3))

    const promise = pollFtsRebuildStatus()
    await vi.advanceTimersByTimeAsync(1000)
    const status = await promise

    expect(status?.status).toBe('done')
    expect(status?.resegmented).toBe(3)
  })

  it('stops polling and returns null when cancelled', async () => {
    vi.mocked(apiGet).mockResolvedValue(running(10))
    let cancelled = false

    const promise = pollFtsRebuildStatus(() => cancelled)
    cancelled = true
    await vi.advanceTimersByTimeAsync(1000)
    const status = await promise

    expect(status).toBeNull()
    // The cancelled check runs before each poll, so at most the initial one ran.
    expect(vi.mocked(apiGet).mock.calls.length).toBeLessThanOrEqual(1)
  })

  it('reports an error status from the server', async () => {
    vi.mocked(apiGet).mockResolvedValueOnce({
      status: 'error', phase: '', total: 0, processed: 0, progress_pct: 0,
      indexed: 0, resegmented: 0, elapsed_ms: 10, error: 'boom',
    })

    const status = await pollFtsRebuildStatus()

    expect(status?.status).toBe('error')
    expect(status?.error).toBe('boom')
  })

  it('startFtsRebuild triggers the POST then polls', async () => {
    vi.mocked(apiPost).mockResolvedValue({ status: 'accepted' })
    vi.mocked(apiGet).mockResolvedValueOnce(done(7))

    const status = await startFtsRebuild()

    expect(apiPost).toHaveBeenCalledWith('/api/rag/rebuild-fts', {})
    expect(status?.resegmented).toBe(7)
  })

  it('propagates a rejected trigger so the caller can show the server reason', async () => {
    const refusal = new Error('已拒绝重建：分词器不可用') as Error & { msgKey?: string }
    refusal.msgKey = 'RAGSegmenterUnavailable'
    vi.mocked(apiPost).mockRejectedValue(refusal)

    await expect(startFtsRebuild()).rejects.toThrow('分词器不可用')
    // A refused trigger must not start polling.
    expect(apiGet).not.toHaveBeenCalled()
  })
})
